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
	if len(body) > 1500 {
		body = body[:1500]
	}
	return fmt.Sprintf(`Ты финансовый аналитик. Определи сентимент новости об облигациях.

Новость: %s

Ответь ТОЛЬКО в формате JSON, без лишнего текста:
{"sentiment": "POSITIVE|NEGATIVE|NEUTRAL", "reason": "пояснение до 15 слов"}`, body)
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
	if len(rs) > 255 {
		rs = rs[:255]
	}
	return s, rs, nil
}

// AnalyzeSentimentOrNeutral never fails from caller perspective for pipeline.
func (c *Client) AnalyzeSentimentOrNeutral(ctx context.Context, newsText string) (sentiment, reason string) {
	s, r, err := c.AnalyzeSentiment(ctx, newsText)
	if err != nil {
		return "NEUTRAL", "Не удалось проанализировать; показан нейтральный сентимент."
	}
	return s, r
}
