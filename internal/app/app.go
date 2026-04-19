package app

import (
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bond_alert_gin/internal/config"
	"bond_alert_gin/internal/openrouter"
	"bond_alert_gin/internal/store"
)

type App struct {
	Cfg   *config.Config
	Pool  *pgxpool.Pool
	Store *store.Store
	OR    *openrouter.Client
	Bot   *tgbotapi.BotAPI
}

func New(cfg *config.Config, pool *pgxpool.Pool) (*App, error) {
	httpClient := &http.Client{
		Timeout: time.Duration(cfg.TelegramHTTPTimeoutSec) * time.Second,
	}
	bot, err := tgbotapi.NewBotAPIWithClient(cfg.TelegramBotToken, tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		return nil, err
	}
	bot.Debug = false
	or := openrouter.New(cfg.OpenRouterAPIKey, cfg.OpenRouterBaseURL, cfg.OpenRouterModel)
	return &App{
		Cfg:   cfg,
		Pool:  pool,
		Store: store.New(pool),
		OR:    or,
		Bot:   bot,
	}, nil
}
