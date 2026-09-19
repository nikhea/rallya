package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/nikhea/rallya/cmd/config"
	auth "github.com/nikhea/rallya/internal/auth"
	"github.com/nikhea/rallya/internal/auth/handler"
	"github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/notification/jobs"

	_ "github.com/nikhea/rallya/docs"
)

// @title			Rallya API
// @version		1.0
// @description	MVP multi-tenant event platform. Auth owns user identity; org membership and permissions live in organization/iam.
// @host			localhost:8080
// @BasePath		/api/v1
// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name							Authorization
// @description					"Bearer <accessToken> from POST /auth/login (or /auth/refresh)."
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	config.LoadEnv()
	config.InitLogger()
	config.ConnectDatabase()
	config.ConnectRedis()
	defer func() {
		_ = config.CloseRedis()
		config.CloseDatabase()
	}()

	// Schema is versioned SQL in migrations/, applied as a release step
	// (make migrate-up). The API never migrates on boot, in dev or prod.

	authRepo := repository.NewAuthRepository(config.DB)
	authSvc := service.NewAuthService(authRepo)
	authHandler := handler.NewHandler(authSvc)

	// River queue: workers run in-process (MVP). Email jobs enqueue
	// transactionally from auth flows on the shared *sql.DB handle.
	workers := river.NewWorkers()
	jobs.AddAll(workers)
	driver := riverdatabasesql.New(config.SQLDB)
	if config.ListenerPool != nil {
		driver = riverdatabasesql.NewWithPgxListener(config.SQLDB, config.ListenerPool)
	}
	riverClient, err := river.NewClient(driver, &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
			jobs.EmailQueue:    {MaxWorkers: 5},
		},
		Workers: workers,
	})
	if err != nil {
		slog.Error("River client failed", "error", err)
		os.Exit(1)
	}
	authSvc.SetEnqueuer(jobs.NewRiverEnqueuer(riverClient, jobs.EmailQueue))
	if err := riverClient.Start(ctx); err != nil {
		slog.Error("River start failed", "error", err)
		os.Exit(1)
	}

	router := gin.Default()

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"app":    "Rallya API",
		})
	})

	api := router.Group("/api/v1")
	{
		api.GET("/hello", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"message": "Welcome to Rallya",
			})
		})
		auth.RegisterRoutes(api.Group("/auth"), authHandler, authRepo)
	}

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		log.Printf("Rallya API running on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := riverClient.Stop(shutdownCtx); err != nil {
		slog.Warn("River stop failed", "error", err)
	}
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("HTTP shutdown failed", "error", err)
	}
	slog.Info("Rallya API stopped")
}
