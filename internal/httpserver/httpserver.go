package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"bond_alert_gin/internal/app"
	"bond_alert_gin/internal/telegram"
)

func NewRouter(a *app.App) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"service": "bond_alert_gin", "docs": "/health"})
	})

	if a.Cfg.TelegramWebhookURL != "" {
		r.POST("/telegram/webhook", func(c *gin.Context) {
			if a.Cfg.TelegramWebhookSecret != "" {
				if c.GetHeader("X-Telegram-Bot-Api-Secret-Token") != a.Cfg.TelegramWebhookSecret {
					c.Status(http.StatusForbidden)
					return
				}
			}
			var upd tgbotapi.Update
			if err := c.ShouldBindJSON(&upd); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
				return
			}
			go telegram.HandleUpdate(context.Background(), a, upd)
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	}
	return r
}
