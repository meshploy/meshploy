package server

import (
	"log"
	"time"

	"github.com/joho/godotenv"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"gorm.io/gorm"
)

// Main is the shared entrypoint for both the CE and EE binaries. The only
// difference between them is which packages have been blank-imported by the
// caller, registering their extensions before this runs.
func Main() {
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

	srv := New(cfg, database)
	log.Printf("API listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
