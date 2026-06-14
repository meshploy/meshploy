package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/config"
	"github.com/meshploy/apps/api/internal/handler"
	"github.com/meshploy/apps/api/internal/middleware"
	"github.com/meshploy/apps/api/internal/service"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

func New(cfg *config.Config, db *gorm.DB) *http.Server {
	r := chi.NewRouter()

	allowedOrigins := []string{cfg.FrontendURL}
	// Always allow local dev origins so `go run` works without extra config.
	if cfg.FrontendURL != "http://localhost:5173" {
		allowedOrigins = append(allowedOrigins, "http://localhost:5173", "http://localhost:4173")
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	// Build services first so the org-member middleware can reference them.
	svc := service.New(db, cfg)

	r.Use(middleware.Auth(cfg.JWTSecret))
	r.Use(middleware.RequireAuth)
	r.Use(middleware.PathRateLimiter(map[string]*middleware.IPRateLimiter{
		// 5 attempts per minute per IP — brute-force protection
		"POST /api/v1/auth/login": middleware.NewIPRateLimiter(rate.Every(12), 5),
		// 3 registrations per hour per IP — spam protection
		"POST /api/v1/auth/register": middleware.NewIPRateLimiter(rate.Limit(3.0/3600.0), 3),
	}))
	r.Use(middleware.OrgMember(func(ctx context.Context, orgID, userID uuid.UUID) error {
		_, err := svc.Orgs.MemberRole(ctx, orgID, userID)
		return err
	}))

	apiCfg := huma.DefaultConfig("Meshploy API", "1.0.0")
	apiCfg.Info.Description = "Meshploy internal developer platform API"
	apiCfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		},
	}

	api := humachi.New(r, apiCfg)

	h := handler.New(cfg, svc)
	h.Register(api)
	h.RegisterRaw(r)

	return &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.APIPort),
		Handler: r,
	}
}
