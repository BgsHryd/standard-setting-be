package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/httpapi"
	"standard-setting/backend/internal/service"
	"standard-setting/backend/internal/session"
	"standard-setting/backend/internal/store"
)

func main() {
	security, err := loadStartupSecurity()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mongoStore, err := store.NewMongo(ctx, env("MONGO_URI", "mongodb://localhost:27017"), env("MONGO_DATABASE", "standard-setting"))
	if err != nil {
		log.Fatalf("mongodb tidak tersedia: %v", err)
	}
	defer mongoStore.Close(context.Background())

	redisDatabase, _ := strconv.Atoi(env("REDIS_DATABASE", "0"))
	redisStore := session.NewRedis(env("REDIS_ADDRESS", "localhost:6379"), os.Getenv("REDIS_PASSWORD"), redisDatabase)
	if err := redisStore.Ping(ctx); err != nil {
		log.Fatalf("redis tidak tersedia: %v", err)
	}
	defer redisStore.Close()

	initialPassword := security.initialPassword
	initialPasswordHash, err := auth.HashPassword(initialPassword)
	if err != nil {
		log.Fatalf("gagal membuat password awal: %v", err)
	}
	if err := mongoStore.BootstrapAdmin(ctx, initialPasswordHash); err != nil {
		log.Fatalf("gagal menyiapkan data awal: %v", err)
	}

	if security.seedDemo {
		if err := mongoStore.SeedDemoItems(ctx); err != nil {
			log.Fatalf("seed demo: %v", err)
		}
	}
	authService := auth.NewService(
		mongoStore,
		redisStore,
		security.jwtSecret,
		15*time.Minute,
		7*24*time.Hour,
	)
	authService.SetLoginLimiter(redisStore)
	businessService := service.New(mongoStore, redisStore, initialPassword)
	app := httpapi.New(httpapi.Config{Auth: authService, Service: businessService, AllowedOrigins: env("CORS_ORIGINS", "http://localhost:5176"), SecureCookies: strings.EqualFold(env("APP_ENV", "development"), "production"), TrustedProxies: strings.FieldsFunc(os.Getenv("TRUSTED_PROXIES"), func(r rune) bool { return r == ',' })})

	serverErrors := make(chan error, 1)
	go func() { serverErrors <- app.Listen(env("HTTP_ADDRESS", ":8080")) }()
	log.Printf("Standard Setting API aktif di %s", env("HTTP_ADDRESS", ":8080"))

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-serverErrors:
		log.Printf("server berhenti: %v", err)
	case <-shutdown:
		shutdownContext, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := app.ShutdownWithContext(shutdownContext); err != nil {
			log.Printf("shutdown tidak bersih: %v", err)
		}
	}
}

type startupSecurity struct {
	jwtSecret, initialPassword string
	seedDemo                   bool
}

func loadStartupSecurity() (startupSecurity, error) {
	cfg := startupSecurity{jwtSecret: env("JWT_SECRET", "standard-setting-local-secret-change-this"), initialPassword: env("INITIAL_PASSWORD", "12345*")}
	value := env("SEED_DEMO_ITEMS", "false")
	var err error
	cfg.seedDemo, err = strconv.ParseBool(value)
	if err != nil {
		return cfg, fmt.Errorf("SEED_DEMO_ITEMS harus true atau false")
	}
	if strings.EqualFold(env("APP_ENV", "development"), "production") {
		if len(cfg.jwtSecret) < 32 || cfg.jwtSecret == "standard-setting-local-secret-change-this" || strings.HasPrefix(cfg.jwtSecret, "replace-") {
			return cfg, fmt.Errorf("JWT_SECRET produksi wajib rahasia acak minimal 32 karakter")
		}
		if len(cfg.initialPassword) < 6 || cfg.initialPassword == "12345*" || strings.HasPrefix(cfg.initialPassword, "replace-") {
			return cfg, fmt.Errorf("INITIAL_PASSWORD produksi wajib password privat minimal 6 karakter")
		}
	}
	return cfg, nil
}
func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
