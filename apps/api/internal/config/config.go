package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL   string
	APIPort       int
	HeadscaleURL  string
	HeadscaleKey  string
	JWTSecret     string
	EncryptionKey string

	APIBaseURL  string // API_BASE_URL    e.g. https://api.meshploy.io  (defaults to http://localhost:4000)
	FrontendURL string // FRONTEND_URL    e.g. http://localhost:5173   (defaults to http://localhost:5173)

	// Kubernetes — optional; build+deploy features require K3s connectivity.
	KubeconfigPath string // KUBECONFIG  path to kubeconfig file; empty = in-cluster config
	BuilderImage   string // BUILDER_IMAGE  override the builder container image
}

func Load() (*Config, error) {
	var missing []string

	require := func(key string) string {
		v := os.Getenv(key)
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}

	port, _ := strconv.Atoi(os.Getenv("API_PORT"))
	if port == 0 {
		port = 4000
	}

	apiBase := os.Getenv("API_BASE_URL")
	if apiBase == "" {
		apiBase = "http://localhost:4000"
	}
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}

	cfg := &Config{
		DatabaseURL:   require("DATABASE_URL"),
		APIPort:       port,
		HeadscaleURL:  os.Getenv("HEADSCALE_URL"),
		HeadscaleKey:  os.Getenv("HEADSCALE_API_KEY"),
		JWTSecret:     require("JWT_SECRET"),
		EncryptionKey: require("ENCRYPTION_KEY"),

		APIBaseURL:  apiBase,
		FrontendURL: frontendURL,

		KubeconfigPath: os.Getenv("KUBECONFIG"),
		BuilderImage:   os.Getenv("BUILDER_IMAGE"),
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}
