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

	cfg := &Config{
		DatabaseURL:   require("DATABASE_URL"),
		APIPort:       port,
		HeadscaleURL:  os.Getenv("HEADSCALE_URL"),
		HeadscaleKey:  os.Getenv("HEADSCALE_API_KEY"),
		JWTSecret:     require("JWT_SECRET"),
		EncryptionKey: require("ENCRYPTION_KEY"),
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}
