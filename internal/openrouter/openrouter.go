package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Client struct {
	APIKey  string
	BaseURL string
	Model   string
	HTTP    *http.Client
}

func New(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	return &Client{
		APIKey:  apiKey,
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		HTTP:    &http.Client{Timeout: 90 * time.Second},
	}
}

func prompt(news string) string {
	body := news
	if len(body) > 2000 {
		body = body[:2000]
	}
	return fmt.Sprintf(`Ты — старший аналитик по долговому рынку в инвестиционном банке. Твоя задача — оценить влияние новости на стоимость и риск облигаций данного эмитента.

Новость: %s

Проанализируй новость как профессиональный аналитик и ответь ТОЛЬКО в формате JSON:
{
  "sentiment": "POSITIVE|NEGATIVE|NEUTRAL",
  "reason": "чёткое объяснение в 2-3 предложения почему такой сентимент, с указанием ключевых факторов"
}

Критерии оценки:
- POSITIVE: новости, которые могут повысить стоимость облигаций (рост прибыли, улучшение кредитного качества, снижение рисков, позитивные корпоративные события)
- NEGATIVE: новости, которые могут снизить стоимость облигаций (убытки, ухудшение финансового состояния, дефолты, реструктуризация, судебные иски, regulatory риски)
- NEUTRAL: рутинные корпоративные новости, отчетность без существенных изменений, технические события

В объяснении укажи конкретные цифры или факты из новости, влияющие на оценку.`, body)
}

var jsonObj = regexp.MustCompile(`\{[\s\S]*\}`)

func extractJSON(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	m := jsonObj.FindString(text)
	if m == "" {
		return nil, fmt.Errorf("no json in model output")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(m), &out); err != nil {
		return nil, err
	}
	return out, nil
}

type chatReq struct {
	Model       string              `json:"model"`
	Messages    []map[string]string `json:"messages"`
	Temperature float64             `json:"temperature"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) AnalyzeSentiment(ctx context.Context, newsText string) (sentiment string, reason string, err error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return "NEUTRAL", "OPENROUTER_API_KEY не задан.", fmt.Errorf("missing OPENROUTER_API_KEY")
	}
	u := c.BaseURL + "/chat/completions"
	body, _ := json.Marshal(chatReq{
		Model: c.Model,
		Messages: []map[string]string{
			{"role": "user", "content": prompt(newsText)},
		},
		Temperature: 0.3,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "NEUTRAL", "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/bond_alert_gin")
	req.Header.Set("X-Title", "bond_alert_gin")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "NEUTRAL", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "NEUTRAL", "", fmt.Errorf("openrouter %d: %s", resp.StatusCode, string(raw))
	}
	var cr chatResp
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "NEUTRAL", "", err
	}
	if len(cr.Choices) == 0 {
		return "NEUTRAL", "", fmt.Errorf("no choices")
	}
	data, err := extractJSON(cr.Choices[0].Message.Content)
	if err != nil {
		return "NEUTRAL", "", err
	}
	s := strings.ToUpper(fmt.Sprint(data["sentiment"]))
	if s != "POSITIVE" && s != "NEUTRAL" && s != "NEGATIVE" {
		s = "NEUTRAL"
	}
	rs := fmt.Sprint(data["reason"])
	if len(rs) > 500 {
		rs = rs[:497] + "..."
	}
	return s, rs, nil
}

func (c *Client) AnalyzeSentimentOrNeutral(ctx context.Context, newsText string) (sentiment, reason string) {
	s, r, err := c.AnalyzeSentiment(ctx, newsText)
	if err != nil {
		return "NEUTRAL", "Техническая ошибка анализа. Новость требует ручной оценки."
	}
	return s, r
}
