package main

import (
	"log"
	"time"

	"github.com/joho/godotenv"
	"github.com/meshploy/apps/api/internal/config"
	"github.com/meshploy/apps/api/internal/server"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

func main() {
	_ = godotenv.Load("../../.env")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db.SetEncryptionKey(cfg.EncryptionKey)

	var database *gorm.DB
	deadline := time.Now().Add(60 * time.Second)
	for {
		database, err = db.Open(cfg.DatabaseURL)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			log.Fatalf("database: failed to connect after 60s: %v", err)
		}
		log.Printf("database: not ready, retrying in 2s: %v", err)
		time.Sleep(2 * time.Second)
	}

	if err := db.Migrate(database); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	srv := server.New(cfg, database)
	log.Printf("API listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
