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
	event "github.com/nikhea/rallya/internal/event"
	"github.com/nikhea/rallya/internal/event/cover"
	eventhandler "github.com/nikhea/rallya/internal/event/handler"
	eventrepository "github.com/nikhea/rallya/internal/event/repository"
	eventservice "github.com/nikhea/rallya/internal/event/service"
	"github.com/nikhea/rallya/internal/iam"
	"github.com/nikhea/rallya/internal/notification/jobs"
	organization "github.com/nikhea/rallya/internal/organization"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"

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

		// IAM (Casbin): enforcer over the shared handle; policies seeded
		// per-org by the organization domain, superadmins from env.
		enforcer, err := iam.NewEnforcer(config.DB)
		if err != nil {
			slog.Error("Casbin enforcer failed", "error", err)
			os.Exit(1)
		}
		if err := iam.SeedSuperAdmins(enforcer, authSvc, config.SuperAdminEmails()); err != nil {
			slog.Error("Superadmin seed failed", "error", err)
			os.Exit(1)
		}

		// Organization domain: consumes auth via UserReader; mail via River.
		orgRepo := orgrepository.NewOrgRepository(config.DB)
		orgSvc := orgservice.NewOrgService(orgRepo, authSvc)
		orgSvc.SetEnqueuer(jobs.NewRiverEnqueuer(riverClient, jobs.EmailQueue))
		orgSvc.SetGroupSyncer(iam.NewMembershipSyncer(enforcer))
		orgSvc.SetPolicySeeder(iam.NewOrgPolicySeeder(enforcer))
		orgHandler := orghandler.NewHandler(orgSvc)
		organization.RegisterRoutes(api.Group("/orgs"), orgHandler, orgRepo, authRepo, enforcer)

		// Events domain: org-scoped CRUD + publish + covers.
		eventRepo := eventrepository.NewEventRepository(config.DB)
		eventSvc := eventservice.NewEventService(eventRepo, orgSvc)
		eventSvc.SetEnqueuer(jobs.NewRiverEnqueuer(riverClient, jobs.EmailQueue))
		if cld, err := cover.NewCloudinary(); err != nil {
			slog.Warn("Cloudinary unconfigured, covers stay on local disk", "error", err)
			eventSvc.SetCoverStorage(cover.NewLocal("./uploads"))
		} else {
			slog.Info("Cover storage: Cloudinary")
			eventSvc.SetCoverStorage(cld)
		}
		eventHandler := eventhandler.NewHandler(eventSvc)
		event.RegisterRoutes(api, eventHandler, authRepo, orgRepo, enforcer)
		// Org delete cleans event assets via the event seam (rows cascade).
		orgSvc.SetAssetCleaner(eventSvc)

		// Cover images + uploads served read-only (local disk for MVP).
		if err := os.MkdirAll("./uploads", 0o755); err != nil {
			slog.Error("Uploads dir failed", "error", err)
			os.Exit(1)
		}
		router.Static("/uploads", "./uploads")

		// GET /auth/me embeds org context (nil-safe when unwired).
		authHandler.SetMembershipLister(orgSvc)
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
