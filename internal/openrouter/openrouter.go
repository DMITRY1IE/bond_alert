package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

func prompt(news, bondName, issuer string) string {
	body := news
	if len(body) > 2000 {
		body = body[:2000]
	}
	issuerLine := ""
	if issuer != "" {
		issuerLine = fmt.Sprintf("\nЭмитент: %s", issuer)
	}
	bondLine := ""
	if bondName != "" {
		bondLine = fmt.Sprintf("\nОблигация: %s", bondName)
	}
	return fmt.Sprintf(`Ты — старший аналитик по долговому рынке в инвестиционном банке. Твоя задача — определить, относится ли новость к данному эмитенту, и если да — оценить влияние на стоимость и риск его облигаций.%s%s

Новость: %s

Проанализируй новость и ответь ТОЛЬКО в формате JSON:
{
  "sentiment": "POSITIVE|NEGATIVE|NEUTRAL|NOT_RELATED",
  "reason": "чёткое объяснение в 2-3 предложения"
}

Критерии оценки:
- NOT_RELATED: новость НЕ имеет прямого отношения к данному эмитенту или его облигациям (упоминание схожих слов, сектора в целом, других компаний)
- POSITIVE: новости, которые могут повысить стоимость облигаций (рост прибыли, улучшение кредитного качества, снижение рисков, позитивные корпоративные события)
- NEGATIVE: новости, которые могут снизить стоимость облигаций (убытки, ухудшение финансового состояния, дефолты, реструктуризация, судебные иски, regulatory риски)
- NEUTRAL: рутинные корпоративные новости, отчетность без существенных изменений, технические события

ВАЖНО: если новость не относится конкретно к этому эмитенту — отвечай NOT_RELATED. Общиерыночные или макроэкономические новости, упоминание отрасли без привязки к эмитенту — это NOT_RELATED.

В объяснении укажи конкретные цифры или факты из новости, влияющие на оценку.`, issuerLine, bondLine, body)
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

func (c *Client) AnalyzeSentiment(ctx context.Context, newsText, bondName, issuer string) (sentiment string, reason string, err error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return "NEUTRAL", "OPENROUTER_API_KEY не задан.", fmt.Errorf("missing OPENROUTER_API_KEY")
	}
	u := c.BaseURL + "/chat/completions"
	body, _ := json.Marshal(chatReq{
		Model: c.Model,
		Messages: []map[string]string{
			{"role": "user", "content": prompt(newsText, bondName, issuer)},
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
	validSents := map[string]bool{"POSITIVE": true, "NEGATIVE": true, "NEUTRAL": true, "NOT_RELATED": true}
	if !validSents[s] {
		s = "NEUTRAL"
	}
	rs := fmt.Sprint(data["reason"])
	if len(rs) > 500 {
		rs = rs[:497] + "..."
	}
	return s, rs, nil
}

func (c *Client) AnalyzeSentimentOrNeutral(ctx context.Context, newsText, bondName, issuer string) (sentiment, reason string) {
	s, r, err := c.AnalyzeSentiment(ctx, newsText, bondName, issuer)
	if err != nil {
		log.Printf("AnalyzeSentiment error: %v", err)
		return fallbackSentiment(newsText)
	}
	return s, r
}

func fallbackSentiment(text string) (sentiment, reason string) {
	lower := strings.ToLower(text)
	
	negativeWords := []string{"убыт", "дефолт", "банкрот", "снижени", "падени", "реструктуризаци", "суд", "иск", "нарушен", "просроч", "невыполн", "отказ", "увольнен", "сокращ", "кризис", "риск", "угроз", "ухудш"}
	positiveWords := []string{"рост", "увеличен", "прибыл", "доход", "размещен", "успешн", "повыш", "рекорд", "превыш", "позитив", "развит", "расшир", "покупк", "погашен", "выплат"}
	
	for _, w := range negativeWords {
		if strings.Contains(lower, w) {
			return "NEGATIVE", "Обнаружены негативные факторы в тексте новости."
		}
	}
	for _, w := range positiveWords {
		if strings.Contains(lower, w) {
			return "POSITIVE", "Обнаружены позитивные факторы в тексте новости."
		}
	}
	return "NEUTRAL", "Существенных факторов не выявлено. Требуется детальный анализ."
}
