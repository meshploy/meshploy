package main

import (
	"log"

	"github.com/joho/godotenv"
	"github.com/meshploy/apps/api/internal/config"
	"github.com/meshploy/apps/api/internal/server"
	"github.com/meshploy/packages/db"
)

func main() {
	_ = godotenv.Load("../../.env")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	if err := db.Migrate(database); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	srv := server.New(cfg, database)
	log.Printf("API listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
