package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string

	TelegramBotToken          string
	TelegramWebhookURL        string
	TelegramWebhookSecret     string
	TelegramHTTPTimeoutSec    int
	TelegramGetUpdatesTimeout int
	AllowedTelegramUserIDs    []int64

	OpenRouterAPIKey  string
	OpenRouterBaseURL string
	OpenRouterModel   string

	ParsingInterval time.Duration
	UserAgent       string
	RSSFeedURLs     []string

	ListenAddr string
	LogLevel   string
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func Load() *Config {
	rss := getenv("RSS_FEED_URLS", "https://www.cbr.ru/rss/eventrss")
	var feeds []string
	for _, u := range strings.Split(rss, ",") {
		if s := strings.TrimSpace(u); s != "" {
			feeds = append(feeds, s)
		}
	}
	var allowedUserIDs []int64
	allowedStr := getenv("TELEGRAM_ALLOWED_USER_IDS", "")
	for _, idStr := range strings.Split(allowedStr, ",") {
		if s := strings.TrimSpace(idStr); s != "" {
			if id, err := strconv.ParseInt(s, 10, 64); err == nil {
				allowedUserIDs = append(allowedUserIDs, id)
			}
		}
	}
	min := getenvInt("PARSING_INTERVAL_MINUTES", 30)
	if min < 1 {
		min = 1
	}
	return &Config{
		DatabaseURL:               getenv("DATABASE_URL", ""),
		TelegramBotToken:          getenv("TELEGRAM_BOT_TOKEN", ""),
		TelegramWebhookURL:        getenv("TELEGRAM_WEBHOOK_URL", ""),
		TelegramWebhookSecret:     getenv("TELEGRAM_WEBHOOK_SECRET", ""),
		TelegramHTTPTimeoutSec:    getenvInt("TELEGRAM_HTTP_TIMEOUT_SEC", 60),
		TelegramGetUpdatesTimeout: getenvInt("TELEGRAM_GET_UPDATES_TIMEOUT", 60),
		AllowedTelegramUserIDs:    allowedUserIDs,
		OpenRouterAPIKey:          getenv("OPENROUTER_API_KEY", ""),
		OpenRouterBaseURL:         getenv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
		OpenRouterModel:           getenv("OPENROUTER_MODEL", "meta-llama/llama-3.2-3b-instruct:free"),
		ParsingInterval:           time.Duration(min) * time.Minute,
		UserAgent:                 getenv("USER_AGENT", "BondSentimentBot/1.0 (Go)"),
		RSSFeedURLs:               feeds,
		ListenAddr:                getenv("LISTEN_ADDR", ":8000"),
		LogLevel:                  strings.ToUpper(getenv("LOG_LEVEL", "INFO")),
	}
}
