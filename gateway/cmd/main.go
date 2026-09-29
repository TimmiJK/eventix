package main

import (
	"eventix/gateway/graph"
	grpcclients "eventix/gateway/internal/grpc_clients"
	"eventix/gateway/internal/middleware"
	"eventix/pkg/helpers"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/joho/godotenv"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	rootDir := helpers.RepoRoot("env")
	if err := godotenv.Load(rootDir + "/.env"); err != nil {
		log.Println(" .env file not found, using environment variables")
	}

	logPath := os.Getenv("LOG_FILE_PATH_GATEWAY")
	if logPath == "" {
		gatewayDir := helpers.RepoRoot("log")
		logPath = gatewayDir + "/logs/gateway.log"
	}

	logWriter := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    100,
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}
	defer logWriter.Close()

	logger := slog.New(slog.NewJSONHandler(logWriter, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	logger.Info("Starting Gateway initialization...")

	authPort := os.Getenv("AUTH_SERVICE_ADDR")
	catalogPort := os.Getenv("CATALOG_SERVICE_ADDR")

	clients, err := grpcclients.NewClients(authPort, catalogPort)
	if err != nil {
		logger.Error("Failed to create gRPC clients", "error", err)
		os.Exit(1)
	}

	resolver := graph.NewResolver(clients)

	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))

	http.Handle("/", playground.Handler("GraphQL Playground", "/query"))
	http.Handle("/query", middleware.AuthMiddleware(logger)(srv))

	port := os.Getenv("GATEWAY_PORT")
	if port == "" {
		port = "8080"
	}

	logger.Info("Gateway is running", "port", port)
	log.Println("Gateway is started")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
