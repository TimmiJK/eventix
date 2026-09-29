package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	posgresPkg "eventix/pkg/postgres"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type TestEnvironment struct {
	PostgresContainer *postgres.PostgresContainer
	AuthServiceCmd    *exec.Cmd
	CatalogServiceCmd *exec.Cmd
	GatewayCmd        *exec.Cmd
	DB                *sql.DB
	DSN               string
	GatewayURL        string
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
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}

func waitReady(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
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

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func setupTestEnvironment(t *testing.T) *TestEnvironment {
	t.Helper()

	rootDir := repoRoot(t)

	if err := godotenv.Load(filepath.Join(rootDir, ".env")); err != nil {
		log.Println(" .env file not found, using environment variables")
	}

	dbNameTest := os.Getenv("DB_NAME_TEST")
	dbPasswordTest := os.Getenv("DB_PASSWORD_TEST")
	dbUserTest := os.Getenv("DB_USER_TEST")

	jwtSecretTest := "test-secret"
	jwtAccessExpTest := "15m"
	jwtRefreshExpTest := "60m"

	authPort := freePort(t)
	catalogPort := freePort(t)
	gatewayPort := freePort(t)

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

	authBin := buildBinary(t, filepath.Join(rootDir, "services", "auth", "cmd"), "auth-service")
	catalogBin := buildBinary(t, filepath.Join(rootDir, "services", "catalog", "cmd"), "catalog-service")
	gatewayBin := buildBinary(t, filepath.Join(rootDir, "gateway", "cmd"), "gateway")

	dbEnv := []string{
		"DB_HOST=" + host,
		fmt.Sprintf("DB_PORT=%d", mappedPort.Num()),
		"DB_USER=" + dbUserTest,
		"DB_PASSWORD=" + dbPasswordTest,
		"DB_NAME=" + dbNameTest,
	}

	authEnv := append(append([]string{}, dbEnv...),
		"JWT_SECRET="+jwtSecretTest,
		fmt.Sprintf("AUTH_GRPC_PORT=%d", authPort),
		"JWT_ACCESS_TOKEN_EXPIRATION="+jwtAccessExpTest,
		"JWT_REFRESH_TOKEN_EXPIRATION="+jwtRefreshExpTest,
	)

	authCmd := exec.Command(authBin)
	authCmd.Dir = rootDir
	authCmd.Stdout = os.Stdout
	authCmd.Stderr = os.Stderr
	authCmd.Env = append(os.Environ(), authEnv...)

	require.NoError(t, authCmd.Start(), "failed to start auth service")
	waitReady(t, fmt.Sprintf("127.0.0.1:%d", authPort), 30*time.Second)

	catalogEnv := append(append([]string{}, dbEnv...),
		fmt.Sprintf("CATALOG_GRPC_PORT=%d", catalogPort),
	)

	catalogCmd := exec.Command(catalogBin)
	catalogCmd.Dir = rootDir
	catalogCmd.Stdout = os.Stdout
	catalogCmd.Stderr = os.Stderr
	catalogCmd.Env = append(os.Environ(), catalogEnv...)

	require.NoError(t, catalogCmd.Start(), "failed to start catalog service")
	waitReady(t, fmt.Sprintf("127.0.0.1:%d", catalogPort), 30*time.Second)

	gatewayCmd := exec.Command(gatewayBin)
	gatewayCmd.Dir = rootDir
	gatewayCmd.Stdout = os.Stdout
	gatewayCmd.Stderr = os.Stderr
	gatewayCmd.Env = append(os.Environ(),
		fmt.Sprintf("GATEWAY_PORT=%d", gatewayPort),
		fmt.Sprintf("AUTH_SERVICE_ADDR=127.0.0.1:%d", authPort), // было: authServicePort, где никто не слушал
		fmt.Sprintf("CATALOG_SERVICE_ADDR=127.0.0.1:%d", catalogPort),
	)

	require.NoError(t, gatewayCmd.Start(), "failed to start gateway")
	waitReady(t, fmt.Sprintf("127.0.0.1:%d", gatewayPort), 30*time.Second)

	return &TestEnvironment{
		PostgresContainer: postgresContainer,
		AuthServiceCmd:    authCmd,
		CatalogServiceCmd: catalogCmd,
		GatewayCmd:        gatewayCmd,
		DB:                db,
		DSN:               dsn,
		GatewayURL:        fmt.Sprintf("http://127.0.0.1:%d", gatewayPort),
	}
}

func (te *TestEnvironment) Cleanup(t *testing.T) {
	killCmd(te.GatewayCmd)
	killCmd(te.CatalogServiceCmd)
	killCmd(te.AuthServiceCmd)

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

	tables := []string{"users", "refresh_tokens", "events", "bookings"}
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

type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type GraphQLResponse struct {
	Data   interface{} `json:"data"`
	Errors []struct {
		Message    string                 `json:"message"`
		Extensions map[string]interface{} `json:"extensions"`
	} `json:"errors,omitempty"`
}

func executeGraphQL(t *testing.T, gatewayURL, query string, variables map[string]interface{}) GraphQLResponse {
	t.Helper()

	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	jsonBody, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp, err := http.Post(gatewayURL+"/query", "application/json", bytes.NewBuffer(jsonBody))
	require.NoError(t, err)
	defer resp.Body.Close()

	var gqlResp GraphQLResponse
	err = json.NewDecoder(resp.Body).Decode(&gqlResp)
	require.NoError(t, err)

	return gqlResp
}

func registerTest(t *testing.T, gatewayURL, email, password, name string) GraphQLResponse {
	t.Helper()
	registerQuery := `
		mutation Register($input: RegisterInput!) {
			register(input: $input) {
				userId
				accessToken
				refreshToken
				expiresIn
			}
		}
	`

	registerVars := map[string]interface{}{
		"input": map[string]interface{}{
			"email":    email,
			"password": password,
			"name":     name,
		},
	}
	resp := executeGraphQL(t, gatewayURL, registerQuery, registerVars)
	return resp
}

func TestE2E_RegisterAndLoginAndLogout_Success(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.Cleanup(t)

	email := "e2e-test@example.com"
	password := "uniquepassword"
	name := "New user"

	resp := registerTest(t, env.GatewayURL, email, password, name)
	require.Empty(t, resp.Errors, "register failed: %+v", resp.Errors)
	require.NotNil(t, resp.Data, "register returned nil data")

	loginQuery := `
		mutation Login($input: LoginInput!) {
			login(input: $input) {
				userId
				accessToken
				refreshToken
			}
		}
	`

	loginVars := map[string]interface{}{
		"input": map[string]interface{}{
			"email":    email,
			"password": password,
		},
	}

	resp = executeGraphQL(t, env.GatewayURL, loginQuery, loginVars)
	require.Empty(t, resp.Errors, "login failed: %+v", resp.Errors)
	require.NotNil(t, resp.Data, "login returned nil data")

	data, ok := resp.Data.(map[string]interface{})
	require.True(t, ok, "login data has unexpected shape: %v", resp.Data)

	loginData, ok := data["login"].(map[string]interface{})
	require.True(t, ok, "no 'login' field in data: %v", data)

	refreshToken, ok := loginData["refreshToken"].(string)
	require.True(t, ok, "refreshToken missing or not a string: %v", loginData)
	require.NotEmpty(t, refreshToken)

	logoutQuery := `
		mutation Logout($refreshToken: String!) {
			logout(refreshToken: $refreshToken)
		}
	`

	logoutVars := map[string]interface{}{
		"refreshToken": refreshToken,
	}

	resp = executeGraphQL(t, env.GatewayURL, logoutQuery, logoutVars)
	require.Empty(t, resp.Errors, "logout failed: %+v", resp.Errors)
	require.NotNil(t, resp.Data, "logout returned nil data")
}

func TestE2E_RegisterAndLoginAndLogout_Error(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.Cleanup(t)

	t.Run("RegisterError_EmptyEmail", func(t *testing.T) {
		email := ""
		password := "uniquepassword"
		name := "New user"

		resp := registerTest(t, env.GatewayURL, email, password, name)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "invalid email format", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "BAD_REQUEST", err)
	})

	t.Run("RegisterError_EmptyPassword", func(t *testing.T) {
		email := "test@test.io"
		password := ""
		name := "New user"

		resp := registerTest(t, env.GatewayURL, email, password, name)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "password must be at least 10 characters", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "BAD_REQUEST", err)
	})

	t.Run("RegisterError_EmptyName", func(t *testing.T) {
		email := "test@test.io"
		password := "password1234567"
		name := ""

		resp := registerTest(t, env.GatewayURL, email, password, name)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "name is required", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "BAD_REQUEST", err)
	})

	t.Run("RegisterError_NotUniqueEmail", func(t *testing.T) {
		email := "test@test.io"
		password := "password1234567"
		name := "User"

		resp := registerTest(t, env.GatewayURL, email, password, name)
		require.Empty(t, resp.Errors)
		require.NotNil(t, resp.Data)

		resp = registerTest(t, env.GatewayURL, email, password, name)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "unable to create account", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "BAD_REQUEST", err)
	})

	t.Run("LoginError_WrongPassword", func(t *testing.T) {
		email := "test@testexample.io"
		password := "password1234567"
		name := "CommonUser"

		resp := registerTest(t, env.GatewayURL, email, password, name)
		require.Empty(t, resp.Errors)
		require.NotNil(t, resp.Data)

		loginQuery := `
		mutation Login($input: LoginInput!) {
			login(input: $input) {
				userId
				accessToken
				refreshToken
			}
		}
	`

		loginVars := map[string]interface{}{
			"input": map[string]interface{}{
				"email":    "test@test.io",
				"password": "weryhardpassword",
			},
		}

		resp = executeGraphQL(t, env.GatewayURL, loginQuery, loginVars)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "invalid credentials", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "BAD_REQUEST", err)
	})

	t.Run("LoginError_EmptyEmail", func(t *testing.T) {
		email := "test2@test.io"
		password := "password1234567"
		name := "CommonUser"

		resp := registerTest(t, env.GatewayURL, email, password, name)
		require.Empty(t, resp.Errors)
		require.NotNil(t, resp.Data)

		loginQuery := `
		mutation Login($input: LoginInput!) {
			login(input: $input) {
				userId
				accessToken
				refreshToken
			}
		}
	`

		loginVars := map[string]interface{}{
			"input": map[string]interface{}{
				"email":    "",
				"password": "weryhardpassword",
			},
		}

		resp = executeGraphQL(t, env.GatewayURL, loginQuery, loginVars)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "invalid credentials", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "BAD_REQUEST", err)
	})

	t.Run("LogoutError_InvalidRefreshToken", func(t *testing.T) {
		logoutQuery := `
		mutation Logout($refreshToken: String!) {
			logout(refreshToken: $refreshToken)
		}
	`

		logoutVars := map[string]interface{}{
			"refreshToken": "refreshToken-123-uytt-1",
		}

		resp := executeGraphQL(t, env.GatewayURL, logoutQuery, logoutVars)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "invalid refresh token", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "UNAUTHORIZED", err)
	})
}

func createTestEvent(t *testing.T, gatewayURL, title, description, venue, eventDate string, totalSeats int, price float64) GraphQLResponse {
	t.Helper()
	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!) {
			createEvent(input: $input) {
				id
    			title
    			description
    			venue
    			eventDate
    			totalSeats
    			availableSeats
    			price
			}
		}
	`
	createEventVars := map[string]interface{}{
		"input": map[string]interface{}{
			"title":       title,
			"description": description,
			"venue":       venue,
			"eventDate":   eventDate,
			"totalSeats":  totalSeats,
			"price":       price,
		},
	}
	resp := executeGraphQL(t, gatewayURL, createEventQuery, createEventVars)
	return resp
}

func TestE2E_CreateAndUpdateAndDeleteEvent_Success(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.Cleanup(t)

	title := "Test Event"
	description := "Description"
	venue := "New-York"
	eventDate := "2026-12-25T20:00:00Z"
	totalSeats := 500
	price := 15.99

	resp := createTestEvent(t, env.GatewayURL, title, description, venue, eventDate, totalSeats, price)
	require.Empty(t, resp.Errors, "createEvent failed: %+v", resp.Errors)
	require.NotNil(t, resp.Data, "createEvent returned nil data")

	data, ok := resp.Data.(map[string]interface{})
	require.True(t, ok, "createEvent data has unexpected shape: %v", resp.Data)

	createEventData, ok := data["createEvent"].(map[string]interface{})
	require.True(t, ok, "no 'createEvent' field in data: %v", data)

	createID, ok := createEventData["id"].(string)
	require.True(t, ok, "id missing or not a string: %v", createEventData)
	assert.NotEmpty(t, createID)
	createTitle, ok := createEventData["title"].(string)
	require.True(t, ok, "title missing or not a string: %v", createEventData)
	createDescription, ok := createEventData["description"].(string)
	require.True(t, ok, "description missing or not a string: %v", createEventData)
	createVenue, ok := createEventData["venue"].(string)
	require.True(t, ok, "venue missing or not a string: %v", createEventData)
	createEventDate, ok := createEventData["eventDate"].(string)
	require.True(t, ok, "eventDate missing or not a string: %v", createEventData)
	createTotalSeats, ok := createEventData["totalSeats"].(float64)
	require.True(t, ok, "totalSeats missing or not a float64: %v", createEventData)
	createAvailableSeats, ok := createEventData["availableSeats"].(float64)
	require.True(t, ok, "availableSeats missing or not a float64: %v", createEventData)
	createPrice, ok := createEventData["price"].(float64)
	require.True(t, ok, "price missing or not a float64: %v", createEventData)

	assert.Equal(t, title, createTitle)
	assert.Equal(t, description, createDescription)
	assert.Equal(t, venue, createVenue)
	assert.Equal(t, eventDate, createEventDate)
	assert.Equal(t, totalSeats, int(createTotalSeats))
	assert.Equal(t, totalSeats, int(createAvailableSeats))
	assert.Equal(t, price, createPrice)

	updateEventQuery := `
		mutation UpdateEvent($id: ID!, $input: UpdateEventInput!) {
			updateEvent(id: $id, input: $input) {
				id
    			title
    			description
    			venue
    			eventDate
    			totalSeats
    			availableSeats
    			price
			}
		}
	`

	updateEventVars := map[string]interface{}{
		"id": createID,
		"input": map[string]interface{}{
			"title":      "New incredible show.....",
			"totalSeats": 1000,
			"price":      120,
			"updateMask": []string{"title", "totalSeats", "price"},
		},
	}

	resp = executeGraphQL(t, env.GatewayURL, updateEventQuery, updateEventVars)
	require.Empty(t, resp.Errors, "updateEvent failed: %+v", resp.Errors)
	require.NotNil(t, resp.Data, "updateEvent returned nil data")

	data, ok = resp.Data.(map[string]interface{})
	require.True(t, ok, "updateEvent data has unexpected shape: %v", resp.Data)

	updateEventData, ok := data["updateEvent"].(map[string]interface{})
	require.True(t, ok, "no 'updateEvent' field in data: %v", data)

	updateID, ok := updateEventData["id"].(string)
	require.True(t, ok, "id missing or not a string: %v", updateEventData)
	assert.NotEmpty(t, updateID)
	updateTitle, ok := updateEventData["title"].(string)
	require.True(t, ok, "title missing or not a string: %v", updateEventData)
	updateDescription, ok := updateEventData["description"].(string)
	require.True(t, ok, "description missing or not a string: %v", updateEventData)
	updateVenue, ok := updateEventData["venue"].(string)
	require.True(t, ok, "venue missing or not a string: %v", updateEventData)
	updateEventDate, ok := updateEventData["eventDate"].(string)
	require.True(t, ok, "eventDate missing or not a string: %v", updateEventData)
	updateTotalSeats, ok := updateEventData["totalSeats"].(float64)
	require.True(t, ok, "totalSeats missing or not a float64: %v", updateEventData)
	updateAvailableSeats, ok := updateEventData["availableSeats"].(float64)
	require.True(t, ok, "availableSeats missing or not a float64: %v", updateEventData)
	updatePrice, ok := updateEventData["price"].(float64)
	require.True(t, ok, "price missing or not a float64: %v", updateEventData)

	assert.Equal(t, "New incredible show.....", updateTitle)
	assert.Equal(t, description, updateDescription)
	assert.Equal(t, venue, updateVenue)
	assert.Equal(t, eventDate, updateEventDate)
	assert.Equal(t, 1000, int(updateTotalSeats))
	assert.Equal(t, 500, int(updateAvailableSeats))
	assert.Equal(t, float64(120), updatePrice)

	deleteEventQuery := `
		mutation DeleteEvent($id: ID!) {
			deleteEvent(id: $id)
		}
	`

	deleteEventVars := map[string]interface{}{
		"id": createID,
	}

	resp = executeGraphQL(t, env.GatewayURL, deleteEventQuery, deleteEventVars)
	require.Empty(t, resp.Errors, "deleteEvent failed: %+v", resp.Errors)
	require.NotNil(t, resp.Data, "deleteEvent returned nil data")

	data, ok = resp.Data.(map[string]interface{})
	require.True(t, ok, "deleteEvent data has unexpected shape: %v", resp.Data)

	deleteEventData, ok := data["deleteEvent"].(bool)
	require.True(t, ok, "no 'deleteEvent' field in data: %v", data)
	require.True(t, deleteEventData, "deleteEventData must be true: %v", data)

	getEventQuery := `
		query GetEvent($id: ID!) {
			getEvent(id: $id) {
				id
    			title
    			description
    			venue
    			eventDate
    			totalSeats
    			availableSeats
    			price
			}
		}
	`

	getEventVars := map[string]interface{}{
		"id": createID,
	}

	resp = executeGraphQL(t, env.GatewayURL, getEventQuery, getEventVars)
	require.NotEmpty(t, resp.Errors, "getEvent must failed")
	require.Nil(t, resp.Data, "getEvent must returned nil data")

	assert.Equal(t, "event not found", resp.Errors[0].Message)
	err, ok := resp.Errors[0].Extensions["code"].(string)
	require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
	assert.Equal(t, "NOT_FOUND", err)
}

func TestE2E_CreateAndUpdateAndDeleteEvent_Error(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.Cleanup(t)

	t.Run("CreateEventError_EmptyTitle", func(t *testing.T) {
		title := ""
		description := "Description"
		venue := "New-York"
		eventDate := "2026-12-25T20:00:00Z"
		totalSeats := 500
		price := 15.99

		resp := createTestEvent(t, env.GatewayURL, title, description, venue, eventDate, totalSeats, price)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "title is required", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "BAD_REQUEST", err)
	})

	t.Run("CreateEventError_NotUniqueTitle", func(t *testing.T) {
		title := "title1"
		description := "Description"
		venue := "New-York"
		eventDate := "2026-12-25T20:00:00Z"
		totalSeats := 500
		price := 15.99

		resp := createTestEvent(t, env.GatewayURL, title, description, venue, eventDate, totalSeats, price)
		require.Empty(t, resp.Errors)
		require.NotNil(t, resp.Data)

		resp = createTestEvent(t, env.GatewayURL, title, description, venue, eventDate, totalSeats, price)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "event already created", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "CONFLICT", err)
	})

	t.Run("UpdateEventError_InvalidFieldNameInFieldMask", func(t *testing.T) {
		title := "New Title"
		description := "Description"
		venue := "New-York"
		eventDate := "2026-12-25T20:00:00Z"
		totalSeats := 500
		price := 15.99

		resp := createTestEvent(t, env.GatewayURL, title, description, venue, eventDate, totalSeats, price)
		require.Empty(t, resp.Errors)
		require.NotNil(t, resp.Data)

		data, ok := resp.Data.(map[string]interface{})
		require.True(t, ok, "createEvent data has unexpected shape: %v", resp.Data)

		createEventData, ok := data["createEvent"].(map[string]interface{})
		require.True(t, ok, "no 'createEvent' field in data: %v", data)

		createID, ok := createEventData["id"].(string)
		require.True(t, ok, "id missing or not a string: %v", createEventData)
		assert.NotEmpty(t, createID)

		updateEventQuery := `
		mutation UpdateEvent($id: ID!, $input: UpdateEventInput!) {
			updateEvent(id: $id, input: $input) {
				id
    			title
    			description
    			venue
    			eventDate
    			totalSeats
    			availableSeats
    			price
			}
		}
	`

		updateEventVars := map[string]interface{}{
			"id": createID,
			"input": map[string]interface{}{
				"title":      "New incredible show.....",
				"totalSeats": 1000,
				"price":      120,
				"updateMask": []string{"title", "totalseats", "price"},
			},
		}

		resp = executeGraphQL(t, env.GatewayURL, updateEventQuery, updateEventVars)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "totalseats is not a valid EventField", resp.Errors[0].Message)
	})

	t.Run("UpdateEventError_InvalidEventID", func(t *testing.T) {
		updateEventQuery := `
		mutation UpdateEvent($id: ID!, $input: UpdateEventInput!) {
			updateEvent(id: $id, input: $input) {
				id
    			title
    			description
    			venue
    			eventDate
    			totalSeats
    			availableSeats
    			price
			}
		}
	`

		updateEventVars := map[string]interface{}{
			"id": "id-1234-uufd-5",
			"input": map[string]interface{}{
				"title":      "New incredible show.....",
				"totalSeats": 1000,
				"price":      120,
				"updateMask": []string{"title", "totalSeats", "price"},
			},
		}

		resp := executeGraphQL(t, env.GatewayURL, updateEventQuery, updateEventVars)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "invalid event id format", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "BAD_REQUEST", err)
	})

	t.Run("DeleteEventError_EventNotFound", func(t *testing.T) {
		deleteEventQuery := `
		mutation DeleteEvent($id: ID!) {
			deleteEvent(id: $id)
		}
	`

		deleteEventVars := map[string]interface{}{
			"id": uuid.New().String(),
		}

		resp := executeGraphQL(t, env.GatewayURL, deleteEventQuery, deleteEventVars)
		require.NotEmpty(t, resp.Errors)
		require.Nil(t, resp.Data)

		assert.Equal(t, "event not found", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "NOT_FOUND", err)
	})
}

func TestE2E_GetEventAndListEvents_Success(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.Cleanup(t)

	createdEvents := make(map[string]struct {
		title       string
		description string
		venue       string
		eventDate   string
		totalSeats  int
		price       float64
	})

	var eventID string
	numberOfEvents := 10
	citys := []string{"London", "Berlin", "Gamburg", "Paris", "Thimphu", "Hon kong", "Tokyo", "Osaka", "Madrid", "Los-Angeles"}
	getEventNumber := 5
	title := "New Title"
	description := "Description"
	eventDate := "2026-12-25T20:00:00Z"
	baseTotalSeats := 500
	basePrice := 15.99

	for i := 0; i < numberOfEvents; i++ {
		resp := createTestEvent(t, env.GatewayURL, title+strconv.Itoa(i), description, citys[i], eventDate, baseTotalSeats+i*250, basePrice+float64(i*3))
		require.Empty(t, resp.Errors)
		require.NotNil(t, resp.Data)

		data, ok := resp.Data.(map[string]interface{})
		require.True(t, ok, "createEvent data has unexpected shape: %v", resp.Data)

		createEventData, ok := data["createEvent"].(map[string]interface{})
		require.True(t, ok, "no 'createEvent' field in data: %v", data)

		id, ok := createEventData["id"].(string)
		require.True(t, ok, "id missing or not a string: %v", createEventData)

		createdEvents[id] = struct {
			title       string
			description string
			venue       string
			eventDate   string
			totalSeats  int
			price       float64
		}{
			title:       title + strconv.Itoa(i),
			description: description,
			venue:       citys[i],
			eventDate:   eventDate,
			totalSeats:  baseTotalSeats + i*250,
			price:       basePrice + float64(i*3),
		}

		if i == getEventNumber {
			eventID = id
		}
	}

	getEventQuery := `
		query GetEvent($id: ID!) {
			getEvent(id: $id) {
				id
    			title
    			description
    			venue
    			eventDate
    			totalSeats
    			availableSeats
    			price
			}
		}
	`

	getEventVars := map[string]interface{}{
		"id": eventID,
	}

	resp := executeGraphQL(t, env.GatewayURL, getEventQuery, getEventVars)
	require.Empty(t, resp.Errors, "getEvent failed: %v", resp.Errors)
	require.NotNil(t, resp.Data, "getEvent returned nil data: %v", resp.Data)

	data, ok := resp.Data.(map[string]interface{})
	require.True(t, ok, "getEvent data has unexpected shape: %v", resp.Data)

	getEventData, ok := data["getEvent"].(map[string]interface{})
	require.True(t, ok, "no 'getEvent' field in data: %v", data)

	getID, ok := getEventData["id"].(string)
	require.True(t, ok, "id missing or not a string: %v", getEventData)
	getTitle, ok := getEventData["title"].(string)
	require.True(t, ok, "title missing or not a string: %v", getEventData)
	getDescription, ok := getEventData["description"].(string)
	require.True(t, ok, "description missing or not a string: %v", getEventData)
	getVenue, ok := getEventData["venue"].(string)
	require.True(t, ok, "venue missing or not a string: %v", getEventData)
	getEventDate, ok := getEventData["eventDate"].(string)
	require.True(t, ok, "eventDate missing or not a string: %v", getEventData)
	getTotalSeats, ok := getEventData["totalSeats"].(float64)
	require.True(t, ok, "totalSeats missing or not a float64: %v", getEventData)
	getAvailableSeats, ok := getEventData["availableSeats"].(float64)
	require.True(t, ok, "availableSeats missing or not a float64: %v", getEventData)
	getPrice, ok := getEventData["price"].(float64)
	require.True(t, ok, "price missing or not a float64: %v", getEventData)

	assert.Equal(t, eventID, getID)
	assert.Equal(t, title+strconv.Itoa(getEventNumber), getTitle)
	assert.Equal(t, description, getDescription)
	assert.Equal(t, citys[getEventNumber], getVenue)
	assert.Equal(t, eventDate, getEventDate)
	assert.Equal(t, baseTotalSeats+getEventNumber*250, int(getTotalSeats))
	assert.Equal(t, baseTotalSeats+getEventNumber*250, int(getAvailableSeats))
	expectedPrice := basePrice + float64(getEventNumber*3)
	assert.InDelta(t, expectedPrice, getPrice, 0.01)

	listEventsQuery := `
		query ListEvents($input: ListEventsInput!) {
			listEvents(input: $input) {
				events{
					id
    				title
    				description
    				venue
    				eventDate
    				totalSeats
    				availableSeats
    				price
				}
    			totalCount
    			page
    			pageSize
			}
		}
	`

	page := 1
	pageSize := 20
	listEventsVars := map[string]interface{}{
		"input": map[string]interface{}{
			"page":        page,
			"pageSize":    pageSize,
			"searchQuery": "",
		},
	}

	resp = executeGraphQL(t, env.GatewayURL, listEventsQuery, listEventsVars)
	require.Empty(t, resp.Errors, "listEvents failed: %v", resp.Errors)
	require.NotNil(t, resp.Data, "listEvents returned nil data: %v", resp.Data)

	data, ok = resp.Data.(map[string]interface{})
	require.True(t, ok, "listEvents data has unexpected shape: %v", resp.Data)

	listEventsData, ok := data["listEvents"].(map[string]interface{})
	require.True(t, ok, "no 'listEvents' field in data: %v", data)

	listEventTotalCount, ok := listEventsData["totalCount"].(float64)
	require.True(t, ok, "no 'totalCount' field in listEventsData: %v", listEventsData)
	assert.Equal(t, numberOfEvents, int(listEventTotalCount))

	listEventPage, ok := listEventsData["page"].(float64)
	require.True(t, ok, "no 'page' field in listEventsData: %v", listEventsData)
	assert.Equal(t, page, int(listEventPage))

	listEventPageSize, ok := listEventsData["pageSize"].(float64)
	require.True(t, ok, "no 'pageSize' field in listEventsData: %v", listEventsData)
	assert.Equal(t, pageSize, int(listEventPageSize))

	eventsRaw, ok := listEventsData["events"].([]interface{})
	require.True(t, ok, "no 'events' field in listEventsData: %v", listEventsData)

	eventsByID := make(map[string]map[string]interface{})
	for _, eventRaw := range eventsRaw {
		event := eventRaw.(map[string]interface{})
		id := event["id"].(string)
		eventsByID[id] = event
	}

	assert.Equal(t, numberOfEvents, len(eventsByID))
	for id, expected := range createdEvents {
		event, exists := eventsByID[id]
		require.True(t, exists, "event with id %s not found in list", id)

		assert.Equal(t, expected.title, event["title"])
		assert.Equal(t, expected.description, event["description"])
		assert.Equal(t, expected.venue, event["venue"])
		assert.Equal(t, expected.eventDate, event["eventDate"])
		assert.Equal(t, float64(expected.totalSeats), event["totalSeats"])
		assert.InDelta(t, expected.price, event["price"], 0.01)
	}
}

func TestE2E_GetEventAndListEvents_Error(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.Cleanup(t)

	t.Run("GetEventError_InvalidID", func(t *testing.T) {
		getEventQuery := `
		query GetEvent($id: ID!) {
			getEvent(id: $id) {
				id
    			title
    			description
    			venue
    			eventDate
    			totalSeats
    			availableSeats
    			price
			}
		}
	`

		getEventVars := map[string]interface{}{
			"id": uuid.New().String(),
		}

		resp := executeGraphQL(t, env.GatewayURL, getEventQuery, getEventVars)
		require.NotEmpty(t, resp.Errors, "getEvent must failed: %v", resp.Errors)
		require.Nil(t, resp.Data, "getEvent must returned nil data: %v", resp.Data)

		assert.Equal(t, "event not found", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "NOT_FOUND", err)
	})

	t.Run("ListEventsError_PageSizeFormat", func(t *testing.T) {
		listEventsQuery := `
		query ListEvents($input: ListEventsInput!) {
			listEvents(input: $input) {
				events{
					id
    				title
    				description
    				venue
    				eventDate
    				totalSeats
    				availableSeats
    				price
				}
    			totalCount
    			page
    			pageSize
			}
		}
	`

		page := 1
		pageSize := 120
		listEventsVars := map[string]interface{}{
			"input": map[string]interface{}{
				"page":        page,
				"pageSize":    pageSize,
				"searchQuery": "",
			},
		}

		resp := executeGraphQL(t, env.GatewayURL, listEventsQuery, listEventsVars)
		require.NotEmpty(t, resp.Errors, "listEvents must failed: %v", resp.Errors)
		require.Nil(t, resp.Data, "listEvents must returned nil data: %v", resp.Data)

		assert.Equal(t, "page_size cannot be greater than 100", resp.Errors[0].Message)
		err, ok := resp.Errors[0].Extensions["code"].(string)
		require.True(t, ok, "no 'code' field in Extensions: %v", resp.Errors[0].Extensions)
		assert.Equal(t, "BAD_REQUEST", err)
	})
}

func TestE2E_ListEvents_Pagination(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.Cleanup(t)

	numberOfEvents := 5
	eventIDs := make([]string, 0, numberOfEvents)
	for i := 0; i < numberOfEvents; i++ {
		resp := createTestEvent(t, env.GatewayURL, "Pagination Event "+strconv.Itoa(i), "Description", "City"+strconv.Itoa(i), "2026-12-25T20:00:00Z", 500+i*100, 15.99+float64(i))
		data, ok := resp.Data.(map[string]interface{})
		require.True(t, ok)
		createEventData, ok := data["createEvent"].(map[string]interface{})
		require.True(t, ok)
		id, ok := createEventData["id"].(string)
		require.True(t, ok)
		require.NotEmpty(t, id)

		eventIDs = append(eventIDs, id)
	}

	listEventsQuery := `
		query ListEvents($input: ListEventsInput!) {
			listEvents(input: $input) {
				events {
					id
					title
				}
				totalCount
				page
				pageSize
			}
		}
	`

	t.Run("Page1", func(t *testing.T) {
		resp := executeGraphQL(t, env.GatewayURL, listEventsQuery, map[string]interface{}{
			"input": map[string]interface{}{
				"page":        1,
				"pageSize":    2,
				"searchQuery": "",
			},
		})
		require.Empty(t, resp.Errors)
		require.NotNil(t, resp.Data)

		data := resp.Data.(map[string]interface{})
		listData := data["listEvents"].(map[string]interface{})
		assert.Equal(t, float64(numberOfEvents), listData["totalCount"])
		assert.Equal(t, float64(1), listData["page"])
		assert.Equal(t, float64(2), listData["pageSize"])

		events := listData["events"].([]interface{})
		assert.Len(t, events, 2)
	})

	t.Run("Page2", func(t *testing.T) {
		resp := executeGraphQL(t, env.GatewayURL, listEventsQuery, map[string]interface{}{
			"input": map[string]interface{}{
				"page":        2,
				"pageSize":    2,
				"searchQuery": "",
			},
		})
		require.Empty(t, resp.Errors)
		require.NotNil(t, resp.Data)

		data := resp.Data.(map[string]interface{})
		listData := data["listEvents"].(map[string]interface{})
		assert.Equal(t, float64(numberOfEvents), listData["totalCount"])
		assert.Equal(t, float64(2), listData["page"])

		events := listData["events"].([]interface{})
		assert.Len(t, events, 2)
	})

	t.Run("Page3_Last", func(t *testing.T) {
		resp := executeGraphQL(t, env.GatewayURL, listEventsQuery, map[string]interface{}{
			"input": map[string]interface{}{
				"page":        3,
				"pageSize":    2,
				"searchQuery": "",
			},
		})
		require.Empty(t, resp.Errors)
		require.NotNil(t, resp.Data)

		data := resp.Data.(map[string]interface{})
		listData := data["listEvents"].(map[string]interface{})

		events := listData["events"].([]interface{})
		assert.Len(t, events, 1)
	})
}
