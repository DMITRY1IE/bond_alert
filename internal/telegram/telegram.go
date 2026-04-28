package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"bond_alert_gin/internal/app"
	"bond_alert_gin/internal/moex"
	"bond_alert_gin/internal/validator"
)

func RunPolling(ctx context.Context, a *app.App) {
	commands := []tgbotapi.BotCommand{
		{Command: "add", Description: "Добавить облигацию в отслеживание"},
		{Command: "list", Description: "Список отслеживаемых облигаций"},
		{Command: "remove", Description: "Удалить облигацию из отслеживания"},
		{Command: "help", Description: "Справка по командам"},
	}
	if _, err := a.Bot.Request(tgbotapi.NewSetMyCommands(commands...)); err != nil {
		log.Printf("failed to set bot commands: %v", err)
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = a.Cfg.TelegramGetUpdatesTimeout
	ch := a.Bot.GetUpdatesChan(u)
	for {
		select {
		case <-ctx.Done():
			a.Bot.StopReceivingUpdates()
			for range ch {
			}
			return
		case upd, ok := <-ch:
			if !ok {
				return
			}
			HandleUpdate(context.Background(), a, upd)
		}
	}
}

func HandleUpdate(ctx context.Context, a *app.App, upd tgbotapi.Update) {
	if upd.Message == nil {
		return
	}
	msg := upd.Message
	if msg.From == nil {
		return
	}
	userID := int64(msg.From.ID)
	if len(a.Cfg.AllowedTelegramUserIDs) > 0 {
		allowed := false
		for _, id := range a.Cfg.AllowedTelegramUserIDs {
			if id == userID {
				allowed = true
				break
			}
		}
		if !allowed {
			_, _ = a.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "У вас нет доступа к этому боту."))
			return
		}
	}
	if !msg.IsCommand() {
		return
	}
	cmd := strings.ToLower(msg.Command())
	args := strings.TrimSpace(msg.CommandArguments())
	send := func(text string) {
		_, err := a.Bot.Send(tgbotapi.NewMessage(msg.Chat.ID, text))
		if err != nil {
			log.Printf("telegram send: %v", err)
		}
	}

	switch cmd {
	case "start":
		if msg.From != nil {
			u := msg.From.UserName
			var un *string
			if u != "" {
				un = &u
			}
			fn := msg.From.FirstName
			ln := msg.From.LastName
			var f, l *string
			if fn != "" {
				f = &fn
			}
			if ln != "" {
				l = &ln
			}
			_, err := a.Store.EnsureUser(ctx, int64(msg.From.ID), un, f, l)
			if err != nil {
				send("Ошибка БД. Попробуйте позже.")
				return
			}
		}
		send("Привет! Я присылаю новости по выбранным облигациям с оценкой тональности.\n\n" +
			"Команды:\n" +
			"/add <ISIN или тикер> — добавить в отслеживание\n" +
			"/list — список подписок\n" +
			"/remove <ISIN или тикер> — отписаться\n" +
			"/help — справка\n\n" +
			"━━━━━━━━━━━━━━━━━━━━━━━━━━\n" +
			"📌 Пример новости:\n\n" +
			"📢 НОВОСТЬ ПО SU26233RMFS5 / ОФЗ 26233\n\n" +
			"🏢 Эмитент: Минфин России\n\n" +
			"📊 Сентимент: 🟢 ПОЛОЖИТЕЛЬНЫЙ\n" +
			"💡 Почему: Новости о снижении ключевой ставки могут положительно повлиять на стоимость облигации\n\n" +
			"📰 ЦБ РФ снизил ключевую ставку до 16%\n\n" +
			"Решение принято на заседании Совета директоров...\n\n" +
			"🔗 https://cbr.ru/press/pr...\n" +
			"🕐 28.04.2026 15:30")
	case "help":
		send("/add <ISIN или тикер> — подписка (данные с MOEX).\n" +
			"/list — отслеживаемые облигации.\n" +
			"/remove <ISIN или тикер> — отписка.\n\n" +
			"Пример: /add RU000A101F94 или /add SU26233RMFS5")
	case "add":
		if args == "" {
			send("Укажите ISIN или тикер: /add RU000A101F94")
			return
		}
		ok, ident, m := validator.ValidateIdentifier(args)
		if !ok {
			send(m)
			return
		}
		if msg.From == nil {
			return
		}
		rb, err := moex.ResolveBond(ctx, a.Cfg.UserAgent, ident)
		if err != nil || rb == nil {
			send("Не удалось найти облигацию на MOEX. Проверьте ISIN или тикер SECID.")
			return
		}
		userID, err := a.Store.EnsureUser(ctx, int64(msg.From.ID), strPtr(msg.From.UserName), strPtr(msg.From.FirstName), strPtr(msg.From.LastName))
		if err != nil {
			send("Ошибка БД.")
			return
		}
		b, err := a.Store.UpsertBond(ctx, rb.ISIN, rb.Ticker, rb.Name, rb.Issuer)
		if err != nil {
			send("Ошибка БД.")
			return
		}
		if err := a.Store.SetSubscriptionActive(ctx, userID, b.ID, true); err != nil {
			send("Ошибка БД.")
			return
		}
		send(fmt.Sprintf("✅ Облигация \"%s\" добавлена в отслеживание. Буду присылать новости с анализом тональности.", rb.Name))
	case "list":
		if msg.From == nil {
			return
		}
		uid, ok, err := a.Store.GetUserByTelegram(ctx, int64(msg.From.ID))
		if err != nil || !ok {
			send("Подписок пока нет. Используйте /add.")
			return
		}
		bonds, err := a.Store.ListUserBonds(ctx, uid)
		if err != nil {
			send("Ошибка БД.")
			return
		}
		if len(bonds) == 0 {
			send("Список пуст. Добавьте облигацию: /add <ISIN или тикер>")
			return
		}
		var lines []string
		for _, b := range bonds {
			line := fmt.Sprintf("• %s (%s)", b.Name, b.ISIN)
			if b.Ticker != nil && *b.Ticker != "" {
				line += fmt.Sprintf(" — %s", *b.Ticker)
			}
			lines = append(lines, line)
		}
		send("Отслеживаемые облигации:\n" + strings.Join(lines, "\n"))
	case "remove":
		if args == "" {
			send("Укажите ISIN или тикер: /remove SU26233RMFS5")
			return
		}
		ok, ident, m := validator.ValidateIdentifier(args)
		if !ok {
			send(m)
			return
		}
		if msg.From == nil {
			return
		}
		uid, ok, err := a.Store.GetUserByTelegram(ctx, int64(msg.From.ID))
		if err != nil || !ok {
			send("Подписок нет.")
			return
		}
		b, err := a.Store.FindBondForUserRemove(ctx, uid, ident)
		if err != nil || b == nil {
			send("Такая облигация не найдена в ваших подписках.")
			return
		}
		label := fmt.Sprintf("%s (%s)", b.Name, b.ISIN)
		if err := a.Store.SetSubscriptionActive(ctx, uid, b.ID, false); err != nil {
			send("Ошибка БД.")
			return
		}
		send("Удалено из отслеживания: " + label + ".")
	default:
		send("Неизвестная команда. /help")
	}
}

func strPtr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
