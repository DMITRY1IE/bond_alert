package jobs

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"bond_alert_gin/internal/config"
	"bond_alert_gin/internal/notifier"
	"bond_alert_gin/internal/openrouter"
	"bond_alert_gin/internal/parser"
	"bond_alert_gin/internal/store"
)

func RunParsingCycle(ctx context.Context, cfg *config.Config, st *store.Store, or *openrouter.Client, bot *tgbotapi.BotAPI) {
	ids, err := st.ActiveSubscriptionBondIDs(ctx)
	if err != nil {
		log.Printf("jobs: active bonds: %v", err)
		return
	}
	if len(ids) == 0 {
		log.Printf("jobs: no active subscriptions")
		return
	}
	client := &http.Client{Timeout: 45 * time.Second}
	for _, bid := range ids {
		processBond(ctx, cfg, st, or, bot, client, bid)
	}
}

func processBond(ctx context.Context, cfg *config.Config, st *store.Store, or *openrouter.Client, bot *tgbotapi.BotAPI, client *http.Client, bondID int32) {
	b, err := st.GetBondByID(ctx, bondID)
	if err != nil || b == nil {
		return
	}
	items, err := parser.Collect(ctx, client, cfg.UserAgent, b)
	if err != nil {
		log.Printf("jobs: collect bond=%d: %v", bondID, err)
		return
	}
	var newIDs []int32
	for _, it := range items {
		exists, err := st.NewsExistsByURL(ctx, it.URL)
		if err != nil || exists {
			continue
		}
		body := it.Summary
		if len(body) < 80 {
			body = parser.FetchArticleBody(ctx, client, cfg.UserAgent, it.URL, 1000)
		}
		llmIn := it.Title + "\n\n" + body
		sent, reason := or.AnalyzeSentimentOrNeutral(ctx, llmIn)
		pub := it.PublishedAt
		analyzed := time.Now().UTC()
		id, err := st.InsertNews(ctx, b.ID, it.Title, truncate(body, 1000), it.URL, it.Source, pub, sent, reason, analyzed)
		if err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "23505" {
				continue
			}
			log.Printf("jobs: insert news: %v", err)
			continue
		}
		newIDs = append(newIDs, id)
	}
	if bot == nil || len(newIDs) == 0 {
		return
	}
	for _, nid := range newIDs {
		if err := notifier.Deliver(ctx, bot, st, nid); err != nil {
			log.Printf("jobs: deliver news=%d: %v", nid, err)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
