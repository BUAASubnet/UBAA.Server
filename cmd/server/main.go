package main

import (
	"log"

	"github.com/BUAASubnet/UBAA.Server/internal/app"
	"github.com/BUAASubnet/UBAA.Server/internal/auth"
	"github.com/BUAASubnet/UBAA.Server/internal/config"
	"github.com/BUAASubnet/UBAA.Server/internal/features"
	"github.com/BUAASubnet/UBAA.Server/internal/storage"
	"github.com/BUAASubnet/UBAA.Server/internal/upstream"
	"github.com/coocood/freecache"
)

func main() {
	cfg := config.Load()
	db, err := storage.OpenSQLite(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	cache := freecache.NewCache(cfg.FreeCacheSizeBytes)
	clients := upstream.NewClientFactory(db, upstream.NewURLRewriter(cfg.UseVPN), cfg.TrustAllCerts)
	authService := auth.NewService(cfg, db, cache, clients)
	server := app.New(app.Dependencies{
		Config: cfg,
		DB:     db,
		Cache:  cache,
		Auth:   authService,
		Features: features.NewService(clients, features.Options{
			BykcDebugRawAPILog:       cfg.BykcDebugRawAPILog,
			BykcDebugParsedCourseLog: cfg.BykcDebugParsedCourseLog,
		}),
	})
	log.Printf("Starting UBAA.Server on %s", cfg.ListenAddr())
	if err := server.Listen(cfg.ListenAddr()); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
