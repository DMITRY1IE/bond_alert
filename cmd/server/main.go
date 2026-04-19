package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"

	"bond_alert_gin/internal/app"
	"bond_alert_gin/internal/config"
	"bond_alert_gin/internal/db"
	"bond_alert_gin/internal/httpserver"
	"bond_alert_gin/internal/jobs"
	"bond_alert_gin/internal/telegram"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	if cfg.TelegramBotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	a, err := app.New(cfg, pool)
	if err != nil {
		log.Fatalf("app: %v", err)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.TelegramWebhookURL != "" {
		u, err := url.Parse(cfg.TelegramWebhookURL)
		if err != nil {
			log.Fatalf("TELEGRAM_WEBHOOK_URL: %v", err)
		}
		if _, err := a.Bot.Request(tgbotapi.WebhookConfig{URL: u}); err != nil {
			log.Fatalf("setWebhook: %v", err)
		}
		log.Printf("webhook set: %s", cfg.TelegramWebhookURL)
		defer func() {
			_, _ = a.Bot.Request(tgbotapi.DeleteWebhookConfig{})
		}()
	} else {
		go telegram.RunPolling(rootCtx, a)
		log.Println("telegram long polling started")
	}

	c := cron.New()
	mins := int(cfg.ParsingInterval / time.Minute)
	if mins < 1 {
		mins = 1
	}
	if _, err := c.AddFunc(fmt.Sprintf("@every %dm", mins), func() {
		jobs.RunParsingCycle(context.Background(), cfg, a.Store, a.OR, a.Bot)
	}); err != nil {
		log.Fatalf("cron: %v", err)
	}
	c.Start()
	defer c.Stop()
	log.Printf("scheduler: every %s", cfg.ParsingInterval)

	r := httpserver.NewRouter(a)
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	<-rootCtx.Done()
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	log.Println("shutdown")
}
