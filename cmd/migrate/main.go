package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/migration"
	"standard-setting/backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migration:", err)
		os.Exit(1)
	}
}

func run() error {
	command := flag.String("command", "status", "status | up | seed (seed adds bootstrap admin and 36 sample items)")
	uri := flag.String("uri", env("MONGO_URI", "mongodb://localhost:27017"), "MongoDB URI")
	database := flag.String("database", env("MONGO_DATABASE", "standard-setting"), "target MongoDB database")
	timeout := flag.Duration("timeout", 30*time.Second, "maximum execution time")
	flag.Parse()
	if flag.NArg() != 0 || (*command != "status" && *command != "up" && *command != "seed") {
		return fmt.Errorf("gunakan -command status, up, atau seed; tidak ada perintah down/drop")
	}
	if *database == "" || *timeout <= 0 {
		return fmt.Errorf("database wajib diisi dan timeout harus positif")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *command == "seed" {
		mongoStore, err := store.NewMongo(ctx, *uri, *database)
		if err != nil {
			return err
		}
		defer mongoStore.Close(context.Background())
		hash, err := auth.HashPassword(env("INITIAL_PASSWORD", "12345*"))
		if err != nil {
			return err
		}
		if err := mongoStore.Seed(ctx, hash); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"database": *database, "seeded": true, "note": "existing accounts and items are preserved"})
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(*uri))
	if err != nil {
		return err
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		return err
	}
	db := client.Database(*database)
	var entries []migration.Entry
	if *command == "up" {
		entries, err = migration.Up(ctx, db)
	} else {
		entries, err = migration.Status(ctx, db)
	}
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"database": *database, "migrations": entries})
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
