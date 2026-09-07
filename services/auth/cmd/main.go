package main

import (
	"context"
	"eventix/pkg/postgres"
	"eventix/proto/auth/pb"
	storage "eventix/services/auth/internal/repository"
	service "eventix/services/auth/internal/service"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"gopkg.in/natefinch/lumberjack.v2"
)

type DB struct {
	dbHost string
	dbPort string
	dbUser string
	dbPass string
	dbName string
}

func main() {
	if err := godotenv.Load("./.env"); err != nil {
		log.Println(" .env file not found, using environment variables")
	}

	logPath := os.Getenv("LOG_FILE_PATH_AUTH")
	if logPath == "" {
		logPath = "logs/auth-service.log"
	}

	logWriter := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    100,
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}
	defer logWriter.Close()

	dbParam := DB{}
	dbParam.dbHost = os.Getenv("DB_HOST")
	dbParam.dbPort = os.Getenv("DB_PORT")
	dbParam.dbUser = os.Getenv("DB_USER")
	dbParam.dbPass = os.Getenv("DB_PASSWORD")
	dbParam.dbName = os.Getenv("DB_NAME")

	jwtSecret := os.Getenv("JWT_SECRET")
	grpcPort := os.Getenv("AUTH_GRPC_PORT")

	accessTokenExp := os.Getenv("JWT_ACCESS_TOKEN_EXPIRATION")
	refreshTokenExp := os.Getenv("JWT_REFRESH_TOKEN_EXPIRATION")

	logger := slog.New(slog.NewJSONHandler(logWriter, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	logger.Info("Starting Auth Service initialization...")

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbParam.dbHost, dbParam.dbPort, dbParam.dbUser, dbParam.dbPass, dbParam.dbName)

	db, err := postgres.NewPostgresDB(dsn)
	if err != nil {
		logger.Error("Failed to connect to database", "error", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		logger.Error("Failed to ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("Successfully connected to PostgreSQL")

	accessExp, err := time.ParseDuration(accessTokenExp)
	if err != nil {
		logger.Error("Invalid ACCESS_TOKEN_EXPIRATION", "error", err)
		os.Exit(1)
	}

	refreshExp, err := time.ParseDuration(refreshTokenExp)
	if err != nil {
		logger.Error("Invalid REFRESH_TOKEN_EXPIRATION", "error", err)
		os.Exit(1)
	}

	repo := storage.NewPostgresStorage(db)
	authManager := service.NewAuthManager(repo, logger, jwtSecret, accessExp, refreshExp)

	grpcServer := grpc.NewServer()
	pb.RegisterAuthServiceServer(grpcServer, authManager)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		logger.Error("Failed to listen on port", "port", grpcPort, "error", err)
		os.Exit(1)
	}

	logger.Info("Auth Service is running and ready to accept connections", "port", grpcPort)
	log.Println("Auth Service is started")
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		logger.Info("Shutting down gracefully...")
		healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		grpcServer.GracefulStop()
		logger.Info("Auth Service stopped")
	}()

	if err := grpcServer.Serve(lis); err != nil {
		logger.Error("Failed to serve gRPC server", "error", err)
	}
}
