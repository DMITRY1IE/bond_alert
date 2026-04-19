package notifier

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"bond_alert_gin/internal/store"
)

func formatNews(n *store.NewsRow) string {
	sent := "NEUTRAL"
	if n.Sentiment != nil {
		sent = strings.ToUpper(*n.Sentiment)
	}
	labels := map[string]struct {
		emoji string
		ru    string
	}{
		"POSITIVE": {"🟢", "ПОЛОЖИТЕЛЬНЫЙ"},
		"NEUTRAL":  {"🟡", "НЕЙТРАЛЬНЫЙ"},
		"NEGATIVE": {"🔴", "ОТРИЦАТЕЛЬНЫЙ"},
	}
	lb, ok := labels[sent]
	if !ok {
		lb = labels["NEUTRAL"]
	}
	display := n.Bond.ISIN
	if n.Bond.Ticker != nil && *n.Bond.Ticker != "" {
		display = *n.Bond.Ticker
	}
	titleLine := n.Bond.Name
	if strings.TrimSpace(titleLine) == "" {
		titleLine = display
	}
	issuer := "—"
	if n.Bond.Issuer != nil && strings.TrimSpace(*n.Bond.Issuer) != "" {
		issuer = *n.Bond.Issuer
	}
	body := ""
	if n.Content != nil {
		body = *n.Content
	}
	if len(body) > 300 {
		body = strings.TrimSpace(body[:300]) + "..."
	}
	reason := "—"
	if n.SentimentReason != nil && strings.TrimSpace(*n.SentimentReason) != "" {
		reason = *n.SentimentReason
	}
	pubT := n.PublishedAt
	if pubT == nil {
		pubT = &n.CreatedAt
	}
	pubS := pubT.Format("02.01.2006 15:04")

	return fmt.Sprintf(
		"📢 НОВОСТЬ ПО %s / %s\n\n"+
			"🏢 Эмитент: %s\n\n"+
			"📊 Сентимент: %s %s\n"+
			"💡 Почему: %s\n\n"+
			"📰 %s\n\n"+
			"%s\n\n"+
			"🔗 %s\n"+
			"🕐 %s",
		display, titleLine, issuer, lb.emoji, lb.ru, reason, n.Title, body, n.URL, pubS,
	)
}

func Deliver(ctx context.Context, bot *tgbotapi.BotAPI, st *store.Store, newsID int32) error {
	row, err := st.GetNewsWithBond(ctx, newsID)
	if err != nil || row == nil {
		return err
	}
	subs, err := st.SubscriberTelegramIDs(ctx, row.BondID)
	if err != nil {
		return err
	}
	text := formatNews(row)
	if len(subs) == 0 {
		return st.MarkNewsSent(ctx, newsID)
	}
	for _, s := range subs {
		ok, err := st.HasDelivery(ctx, newsID, s.UserID)
		if err != nil || ok {
			continue
		}
		msg := tgbotapi.NewMessage(s.TelegramID, text)
		if _, err := bot.Send(msg); err != nil {
			continue
		}
		_ = st.InsertDelivery(ctx, newsID, s.UserID)
	}
	return st.MarkNewsSent(ctx, newsID)
}
