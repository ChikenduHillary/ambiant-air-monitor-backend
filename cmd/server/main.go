package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/cors"

	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/db"
	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/handlers"
	authmw "github.com/chikenduhillary/ambiant-air-monitor-api/internal/middleware"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	dbPath       := envOr("DATABASE_URL", "")
	addr         := envOr("ADDR", ":8080")
	frontendURL  := envOr("FRONTEND_URL", "http://localhost:3000")
	jwtSecret    := []byte(envOr("JWT_SECRET", "dev-secret-change-in-production"))
	googleCfg    := handlers.Config{
		JWTSecret:          jwtSecret,
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  envOr("GOOGLE_REDIRECT_URL", "http://localhost:8080/api/v1/auth/google/callback"),
		FrontendURL:        frontendURL,
	}

	if dbPath == "" {
		slog.Error("DATABASE_URL is required — set it in .env or as an environment variable")
		os.Exit(1)
	}

	database, err := db.Open(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	h := handlers.New(database, googleCfg)
	protect    := authmw.Authenticate(jwtSecret)
	adminOnly  := authmw.AdminOnly(database)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", h.Health)

		// Field devices (e.g. the AAQPHM firmware) push readings here using
		// their own per-device key instead of a user JWT.
		r.Group(func(r chi.Router) {
			r.Use(authmw.DeviceAuth(database))
			r.Post("/devices/readings", h.IngestReadings)
			r.Get("/devices/readings/current", h.GetDeviceCurrentReading)
			r.Get("/devices/readings/hourly", h.GetDeviceHourlyReadings)
			r.Get("/devices/readings/daily", h.GetDeviceDailyReadings)
			r.Get("/devices/readings/today-peak", h.GetDeviceTodayPeak)
		})

		// Device management (create/list/revoke) — a user JWT, not a device key.
		r.Group(func(r chi.Router) {
			r.Use(protect)
			r.Route("/devices", func(r chi.Router) {
				r.Get("/", h.ListDevices)
				r.Post("/", h.CreateDevice)
				r.Delete("/{id}", h.DeleteDevice)
			})
		})

		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", h.Register)
			r.Post("/login", h.Login)
			r.With(protect).Get("/me", h.Me)
			r.Get("/google", h.GoogleLogin)
			r.Get("/google/callback", h.GoogleCallback)
		})

		// Admin-only routes (requires JWT + admin role)
		r.Group(func(r chi.Router) {
			r.Use(protect)
			r.Use(adminOnly)

			r.Route("/admin", func(r chi.Router) {
				r.Get("/stats", h.AdminStats)

				r.Route("/users", func(r chi.Router) {
					r.Get("/", h.AdminListUsers)
					r.Get("/{id}", h.AdminGetUser)
					r.Put("/{id}", h.AdminUpdateUser)
					r.Delete("/{id}", h.AdminDeleteUser)
				})

				r.Route("/alerts", func(r chi.Router) {
					r.Get("/", h.AdminListAlerts)
					r.Post("/", h.AdminBroadcastAlert)
					r.Put("/read-all", h.AdminMarkAllAlertsRead)
					r.Delete("/{id}", h.AdminDeleteAlert)
				})

				r.Route("/sensors", func(r chi.Router) {
					r.Get("/", h.AdminListReadings)
					r.Get("/export", h.AdminExportReadings)
					r.Delete("/{id}", h.AdminDeleteReading)
				})

				r.Get("/symptoms", h.AdminListSymptoms)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(protect)

			r.Route("/sensors", func(r chi.Router) {
				r.Get("/current", h.GetCurrentReading)
				r.Get("/hourly", h.GetHourlyReadings)
				r.Get("/daily", h.GetDailyReadings)
				r.Get("/today-peak", h.GetTodayPeak)
			})

			r.Route("/alerts", func(r chi.Router) {
				r.Get("/", h.ListAlerts)
				r.Put("/{id}/read", h.MarkAlertRead)
			})

			r.Route("/symptoms", func(r chi.Router) {
				r.Get("/", h.ListSymptoms)
				r.Post("/", h.CreateSymptom)
				r.Get("/{id}", h.GetSymptom)
			})
		})
	})

	allowedOrigins := []string{"http://localhost:3000", "http://localhost:3001"}
	if extra := os.Getenv("CORS_ORIGINS"); extra != "" {
		allowedOrigins = append(allowedOrigins, extra)
	}

	srv := &http.Server{
		Addr: addr,
		Handler: cors.New(cors.Options{
			AllowedOrigins:   allowedOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Device-Key"},
			AllowCredentials: true,
		}).Handler(r),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("graceful shutdown initiated")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	slog.Info("server stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
