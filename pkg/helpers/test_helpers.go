package helpers

import (
	"context"
	"database/sql"
	posgresPkg "eventix/pkg/postgres"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type TestEnvironment struct {
	PostgresContainer *postgres.PostgresContainer
	ServiceCmd        *exec.Cmd
	DB                *sql.DB
	DSN               string
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	t.Fatal("repo root not found (.gitignore marker)")
	return ""
}

func buildBinary(t *testing.T, dir, out string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	bin := filepath.Join(t.TempDir(), out)
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	outBytes, err := cmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(outBytes))

	return bin
}

func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close(), "release test port")
	return port
}

func waitReady(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("service on %s did not become ready in %s", addr, timeout)
}

func killCmd(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func SetupTestEnvironment(t *testing.T, serviceName string) *TestEnvironment {
	t.Helper()

	rootDir := repoRoot(t)

	if err := godotenv.Load(filepath.Join(rootDir, ".env")); err != nil {
		log.Println(" .env file not found, using environment variables")
	}

	dbNameTest := os.Getenv("DB_NAME_TEST")
	dbPasswordTest := os.Getenv("DB_PASSWORD_TEST")
	dbUserTest := os.Getenv("DB_USER_TEST")

	var jwtSecretTest, jwtAccessExpTest, jwtRefreshExpTest string
	if serviceName == "auth" {
		jwtSecretTest = "test-secret"
		jwtAccessExpTest = "15m"
		jwtRefreshExpTest = "60m"
	}

	port := freePort(t)

	ctx := context.Background()

	postgresContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase(dbNameTest),
		postgres.WithUsername(dbUserTest),
		postgres.WithPassword(dbPasswordTest),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err, "failed to start postgres container")

	host, err := postgresContainer.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := postgresContainer.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", host, mappedPort.Num(), dbUserTest, dbPasswordTest, dbNameTest)

	db, err := posgresPkg.NewPostgresDB(dsn)
	require.NoError(t, err, "failed to connect to test database")

	applyMigrations(t, db)

	bin := buildBinary(t, filepath.Join(rootDir, "services", serviceName, "cmd"), serviceName+"-service")

	dbEnv := []string{
		"DB_HOST=" + host,
		fmt.Sprintf("DB_PORT=%d", mappedPort.Num()),
		"DB_USER=" + dbUserTest,
		"DB_PASSWORD=" + dbPasswordTest,
		"DB_NAME=" + dbNameTest,
	}

	env := append(append([]string{}, dbEnv...),
		"JWT_SECRET="+jwtSecretTest,
		fmt.Sprintf(strings.ToUpper(serviceName)+"_GRPC_PORT=%d", port),
		"JWT_ACCESS_TOKEN_EXPIRATION="+jwtAccessExpTest,
		"JWT_REFRESH_TOKEN_EXPIRATION="+jwtRefreshExpTest,
	)

	cmd := exec.Command(bin)
	cmd.Dir = rootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), env...)

	require.NoError(t, cmd.Start(), "failed to start "+serviceName+" service")
	waitReady(t, fmt.Sprintf("127.0.0.1:%d", port), 30*time.Second)

	return &TestEnvironment{
		PostgresContainer: postgresContainer,
		ServiceCmd:        cmd,
		DB:                db,
		DSN:               dsn,
	}
}

func (te *TestEnvironment) Cleanup(t *testing.T) {
	killCmd(te.ServiceCmd)

	if te.DB != nil {
		_ = te.DB.Close()
	}

	if te.PostgresContainer != nil {
		if err := te.PostgresContainer.Terminate(context.Background()); err != nil {
			t.Logf("warning: failed to terminate postgres container: %v", err)
		}
	}
}

func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()

	migrationsDir := filepath.Join(repoRoot(t), "migrations")

	tables := []string{"users", "events", "bookings", "refresh_tokens"}
	for _, table := range tables {
		_, err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table))
		if err != nil {
			t.Logf("Warning: failed to drop %s: %v", table, err)
		}
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no .up.sql migrations found")
	}

	for _, n := range names {
		body, err := os.ReadFile(filepath.Join(migrationsDir, n))
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("apply migration %s: %v", n, err)
		}
	}
}
