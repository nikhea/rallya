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

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/nikhea/rallya/cmd/config"
	admin "github.com/nikhea/rallya/internal/admin"
	adminhandler "github.com/nikhea/rallya/internal/admin/handler"
	adminservice "github.com/nikhea/rallya/internal/admin/service"
	attendee "github.com/nikhea/rallya/internal/attendee"
	attendeehandler "github.com/nikhea/rallya/internal/attendee/handler"
	attendeerepository "github.com/nikhea/rallya/internal/attendee/repository"
	attendeeservice "github.com/nikhea/rallya/internal/attendee/service"
	audit "github.com/nikhea/rallya/internal/audit"
	audithandler "github.com/nikhea/rallya/internal/audit/handler"
	auditrepository "github.com/nikhea/rallya/internal/audit/repository"
	auditservice "github.com/nikhea/rallya/internal/audit/service"
	auth "github.com/nikhea/rallya/internal/auth"
	"github.com/nikhea/rallya/internal/auth/handler"
	"github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/cache"
	checkin "github.com/nikhea/rallya/internal/checkin"
	checkinhandler "github.com/nikhea/rallya/internal/checkin/handler"
	checkinrepository "github.com/nikhea/rallya/internal/checkin/repository"
	checkinservice "github.com/nikhea/rallya/internal/checkin/service"
	event "github.com/nikhea/rallya/internal/event"
	"github.com/nikhea/rallya/internal/event/cover"
	eventhandler "github.com/nikhea/rallya/internal/event/handler"
	eventrepository "github.com/nikhea/rallya/internal/event/repository"
	eventservice "github.com/nikhea/rallya/internal/event/service"
	"github.com/nikhea/rallya/internal/iam"
	kit "github.com/nikhea/rallya/internal/kit"
	kithandler "github.com/nikhea/rallya/internal/kit/handler"
	kitrepository "github.com/nikhea/rallya/internal/kit/repository"
	kitservice "github.com/nikhea/rallya/internal/kit/service"
	"github.com/nikhea/rallya/internal/notification/jobs"
	order "github.com/nikhea/rallya/internal/order"
	orderhandler "github.com/nikhea/rallya/internal/order/handler"
	orderjobs "github.com/nikhea/rallya/internal/order/jobs"
	orderrepository "github.com/nikhea/rallya/internal/order/repository"
	orderservice "github.com/nikhea/rallya/internal/order/service"
	organization "github.com/nikhea/rallya/internal/organization"
	orghandler "github.com/nikhea/rallya/internal/organization/handler"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
	payment "github.com/nikhea/rallya/internal/payment"
	paymenthandler "github.com/nikhea/rallya/internal/payment/handler"
	paymentservice "github.com/nikhea/rallya/internal/payment/service"
	"github.com/nikhea/rallya/internal/ratelimit"
	subscription "github.com/nikhea/rallya/internal/subscription"
	subhandler "github.com/nikhea/rallya/internal/subscription/handler"
	subrepository "github.com/nikhea/rallya/internal/subscription/repository"
	subservice "github.com/nikhea/rallya/internal/subscription/service"
	ticketing "github.com/nikhea/rallya/internal/ticketing"
	tickethandler "github.com/nikhea/rallya/internal/ticketing/handler"
	ticketrepository "github.com/nikhea/rallya/internal/ticketing/repository"
	ticketservice "github.com/nikhea/rallya/internal/ticketing/service"

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

	// Trust explicit proxies so rate limiting keys on real client IPs.
	// Unset = RemoteAddr only (secure default).
	if proxies := config.TrustedProxies(); len(proxies) > 0 {
		if err := router.SetTrustedProxies(proxies); err != nil {
			slog.Error("bad TRUSTED_PROXIES, using RemoteAddr", "error", err)
		}
	}

	// CORS: origins strictly from CORS_ALLOWED_ORIGINS (no default —
	// empty fails closed). Wildcard "*" serves without credentials;
	// explicit origins echo back with credentials.
	origins := config.AllowedOrigins()
	if len(origins) == 0 {
		// No middleware at all: browsers block cross-origin by default.
		slog.Warn("CORS_ALLOWED_ORIGINS unset: cross-origin requests denied")
	} else {
		corsCfg := cors.DefaultConfig()
		corsCfg.AllowMethods = []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"}
		corsCfg.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"}
		corsCfg.MaxAge = 12 * time.Hour
		if len(origins) == 1 && origins[0] == "*" {
			corsCfg.AllowAllOrigins = true
		} else {
			corsCfg.AllowOrigins = origins
			corsCfg.AllowCredentials = true
		}
		router.Use(cors.New(corsCfg))
	}

	// Rate limiting: Redis-shared when connected, in-process otherwise.
	// Strict tier guards brute-forceable auth endpoints; default tier is
	// generous (abuse-shaped traffic gets lower tiers + login telemetry).
	limiterStore := ratelimit.Store(ratelimit.NewMemoryStore())
	if config.RDB != nil {
		limiterStore = ratelimit.NewRedisStore(config.RDB)
		slog.Info("Rate limit store: Redis")
	} else {
		slog.Info("Rate limit store: memory (single instance)")
	}

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"app":    "Rallya API",
		})
	})

	api := router.Group("/api/v1")
	api.Use(ratelimit.Limit(limiterStore, "default", config.APIPerMin()))
	{
		api.GET("/hello", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"message": "Welcome to Rallya",
			})
		})
		authGroup := api.Group("/auth")
		authGroup.Use(ratelimit.Limit(limiterStore, "auth", config.AuthPerMin()))
		auth.RegisterRoutes(authGroup, authHandler, authRepo)

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

		// Audit domain: append-only trail; every mutating domain emits
		// through the Emitter seam (wired below per service). Routes
		// register last (they need every repo + the enforcer).
		auditRepo := auditrepository.NewAuditRepository(config.DB)
		auditSvc := auditservice.NewAuditService(auditRepo)
		auditHandler := audithandler.NewHandler(auditSvc)

		// Organization domain: consumes auth via UserReader; mail via River.
		orgRepo := orgrepository.NewOrgRepository(config.DB)
		orgSvc := orgservice.NewOrgService(orgRepo, authSvc)
		orgSvc.SetEnqueuer(jobs.NewRiverEnqueuer(riverClient, jobs.EmailQueue))
		orgSvc.SetGroupSyncer(iam.NewMembershipSyncer(enforcer))
		orgSvc.SetPolicySeeder(iam.NewOrgPolicySeeder(enforcer))
		orgSvc.SetAuditEmitter(auditSvc)
		orgSvc.SetApiKeyStore(authRepo)
		orgHandler := orghandler.NewHandler(orgSvc)
		organization.RegisterRoutes(api.Group("/orgs"), orgHandler, orgRepo, authRepo, enforcer)

		// Events domain: org-scoped CRUD + publish + covers.
		eventRepo := eventrepository.NewEventRepository(config.DB)
		eventSvc := eventservice.NewEventService(eventRepo, orgSvc)
		eventSvc.SetEnqueuer(jobs.NewRiverEnqueuer(riverClient, jobs.EmailQueue))
		eventSvc.SetAuditEmitter(auditSvc)
		// Public detail cache (nil-client disables: reads fall to Postgres).
		eventSvc.SetDetailCache(cache.New(config.RDB))
		if cld, err := cover.NewCloudinary(); err != nil {
			slog.Warn("Cloudinary unconfigured, covers stay on local disk", "error", err)
			eventSvc.SetCoverStorage(cover.NewLocal("./uploads"))
		} else {
			slog.Info("Cover storage: Cloudinary")
			eventSvc.SetCoverStorage(cld)
		}
		eventHandler := eventhandler.NewHandler(eventSvc)
		event.RegisterRoutes(api, eventHandler, authRepo, orgRepo, enforcer)

		// Ticketing domain: types + pricing + rules over the event adapter.
		ticketRepo := ticketrepository.NewTicketRepository(config.DB)
		ticketSvc := ticketservice.NewTicketService(ticketRepo, ticketservice.NewEventAdapter(eventSvc))
		ticketSvc.SetAuditEmitter(auditSvc)
		ticketHandler := tickethandler.NewHandler(ticketSvc)
		ticketing.RegisterRoutes(api, ticketHandler, authRepo, orgRepo, enforcer)
		// Org delete cleans event assets via the event seam (rows cascade).
		orgSvc.SetAssetCleaner(eventSvc)

		// Orders domain: claims against ticket inventory + hold sweeper.
		orderRepo := orderrepository.NewOrderRepository(config.DB)
		orderSvc := orderservice.NewOrderService(orderRepo, ticketSvc, eventSvc, orgSvc, authSvc)
		orderSvc.SetEnqueuer(jobs.NewRiverEnqueuer(riverClient, jobs.EmailQueue))
		orderSvc.SetAuditEmitter(auditSvc)
		// QR secret resolved once here: fail-closed at boot, never per-request
		// (a missing secret must not os.Exit inside a handler).
		orderSvc.SetQRSecret(config.QRSigningSecret())
		orderHandler := orderhandler.NewHandler(orderSvc)
		order.RegisterRoutes(api, orderHandler, authRepo)
		river.AddWorker(workers, &orderjobs.SweepExpiredOrdersWorker{Svc: orderSvc})
		riverClient.PeriodicJobs().Add(river.NewPeriodicJob(
			river.PeriodicInterval(5*time.Minute),
			func() (river.JobArgs, *river.InsertOpts) {
				return orderjobs.SweepExpiredOrdersArgs{}, nil
			}, nil))

		// Attendees domain: door records minted from confirmations.
		attendeeRepo := attendeerepository.NewAttendeeRepository(config.DB)
		attendeeSvc := attendeeservice.NewAttendeeService(attendeeRepo, authSvc, eventSvc, orgSvc)
		attendeeHandler := attendeehandler.NewHandler(attendeeSvc)
		attendee.RegisterRoutes(api, attendeeHandler, authRepo, orgRepo, enforcer)
		orderSvc.SetAttendeeMinter(attendeeSvc)

		// Check-in domain: door scans over the attendee seam (row flip
		// stays in attendees; logging + QR verify here).
		checkinRepo := checkinrepository.NewCheckinRepository(config.DB)
		checkinSvc := checkinservice.NewCheckinService(config.DB, checkinRepo, attendeeSvc, eventSvc)
		checkinSvc.SetQRSecret(config.QRSigningSecret())
		checkinSvc.SetAuditEmitter(auditSvc)
		checkinHandler := checkinhandler.NewHandler(checkinSvc)
		checkin.RegisterRoutes(api, checkinHandler, authRepo, orgRepo, enforcer)

		// Kit domain: named kit types per event + PENDING -> COLLECTED
		// (-> VOIDED) handouts over the attendee seam. Eligibility reads
		// attendee status (CHECKED_IN gate); check-in reverts consult the
		// collection guard so COLLECTED rows never strand on REGISTERED.
		kitRepo := kitrepository.NewKitRepository(config.DB)
		kitSvc := kitservice.NewKitService(config.DB, kitRepo, attendeeSvc, eventSvc)
		kitSvc.SetAuditEmitter(auditSvc)
		checkinSvc.SetCollectionGuard(kitSvc)
		kitHandler := kithandler.NewHandler(kitSvc)
		kit.RegisterRoutes(api, kitHandler, authRepo, orgRepo, enforcer)

		// Subscription domain: org tiers (FREE/PRO/SCALE) over Stripe
		// Billing. Absent rows resolve Free; attendee checkout stays
		// one-off and never reads this domain. Enforcing services consume
		// entitlements through setters (nil-safe: restrictive Free).
		subRepo := subrepository.NewSubscriptionRepository(config.DB)
		subSvc := subservice.NewSubscriptionService(subRepo, orgSvc)
		subSvc.SetAuditEmitter(auditSvc)
		var billing subservice.BillingProvider
		if stripeBilling, err := subservice.NewStripeBilling(subRepo); err != nil {
			slog.Warn("Stripe Billing unconfigured, subscriptions read-only", "error", err)
		} else {
			billing = stripeBilling
		}
		subSvc.SetBillingProvider(billing)
		subHandler := subhandler.NewHandler(subSvc, config.AppURL())
		subscription.RegisterRoutes(api, subHandler, authRepo, orgRepo)
		orgSvc.SetEntitlementProvider(subSvc)
		eventSvc.SetEntitlementProvider(subSvc)
		orderSvc.SetEntitlementProvider(subSvc)
		kitSvc.SetEntitlementProvider(subSvc)
		orderSvc.SetAttendeeCounter(attendeeSvc)

		// Payments domain: Stripe Checkout + webhooks. Degrades to 503s
		// when unconfigured; boot never fails for missing keys.
		var checkout paymentservice.CheckoutProvider
		if stripeProvider, err := paymentservice.NewStripeCheckout(); err != nil {
			slog.Warn("Stripe unconfigured, checkout disabled", "error", err)
		} else {
			checkout = stripeProvider
		}
		paymentSvc := paymentservice.NewPaymentService(orderSvc, checkout)
		paymentSvc.SetBillingHandler(subSvc)
		paymentHandler := paymenthandler.NewHandler(paymentSvc, config.AppURL())
		payment.RegisterRoutes(api, paymentHandler, authRepo)

		// Audit reads register last (need orgRepo + enforcer).
		audit.RegisterRoutes(api, auditHandler, authRepo, orgRepo, enforcer)

		// Admin reads: platform surface over tenant repos (superadmin only;
		// every read audited). Repair triggers follow as their own slice.
		adminSvc := adminservice.NewAdminService(orgRepo, authRepo, orderRepo, enforcer)
		adminSvc.SetAuditEmitter(auditSvc)
		admin.RegisterRoutes(api, adminhandler.NewHandler(adminSvc), authRepo, enforcer)

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
