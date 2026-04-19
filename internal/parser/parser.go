package parser

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"bond_alert_gin/internal/domain"
)

const (
	smartLabNews   = "https://smart-lab.ru/blog/news/"
	finamBondsRoot = "https://bonds.finam.ru/"
)

func bondKeywords(b *domain.Bond) map[string]struct{} {
	parts := make(map[string]struct{})
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) >= 4 {
			parts[strings.ToUpper(s)] = struct{}{}
		}
	}
	if b.ISIN != "" {
		add(b.ISIN)
	}
	if b.Ticker != nil && *b.Ticker != "" {
		add(*b.Ticker)
	}
	if b.Name != "" {
		parts[strings.ToUpper(b.Name)] = struct{}{}
		for _, w := range regexp.MustCompile(`[^\p{L}\p{N}]+`).Split(b.Name, -1) {
			add(w)
		}
	}
	return parts
}

func textMatches(text string, kw map[string]struct{}) bool {
	u := strings.ToUpper(text)
	for k := range kw {
		if len(k) >= 4 && strings.Contains(u, k) {
			return true
		}
	}
	return false
}

var ruDateRe = regexp.MustCompile(`(?i)(\d{1,2})\s+([а-яё]+)\s+(\d{4})(?:,\s*(\d{1,2}):(\d{2}))?`)

var ruMonths = map[string]time.Month{
	"января": 1, "февраля": 2, "марта": 3, "апреля": 4, "мая": 5, "июня": 6,
	"июля": 7, "августа": 8, "сентября": 9, "октября": 10, "ноября": 11, "декабря": 12,
}

func parseRuDate(line string) *time.Time {
	m := ruDateRe.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	day := atoi(m[1])
	monName := strings.ToLower(m[2])
	year := atoi(m[3])
	hour := atoi(m[4])
	min := atoi(m[5])
	mon, ok := ruMonths[monName]
	if !ok {
		return nil
	}
	t := time.Date(year, mon, day, hour, min, 0, 0, time.Local)
	return &t
}

func atoi(s string) int {
	if s == "" {
		return 0
	}
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func getText(ctx context.Context, client *http.Client, ua, urlStr string) (string, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return "", err
		}
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			last = fmt.Errorf("http %d", resp.StatusCode)
			continue
		}
		return string(b), nil
	}
	return "", last
}

func ParseSmartLab(ctx context.Context, client *http.Client, ua string, b *domain.Bond, maxPages int) ([]domain.ParsedItem, error) {
	kw := bondKeywords(b)
	if len(kw) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	var out []domain.ParsedItem
	for page := 1; page <= maxPages; page++ {
		u := smartLabNews
		if page > 1 {
			u = fmt.Sprintf("%s?page=%d", smartLabNews, page)
		}
		html, err := getText(ctx, client, ua, u)
		if err != nil {
			break
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			continue
		}
		doc.Find("h2, h3").Each(func(_ int, h *goquery.Selection) {
			title := strings.TrimSpace(h.Text())
			if len(title) < 10 {
				return
			}
			a := h.Find("a[href*='/blog/news/']").First()
			if a.Length() == 0 {
				return
			}
			href, _ := a.Attr("href")
			full := absolutize("https://smart-lab.ru", href)
			if _, ok := seen[full]; ok {
				return
			}
			block := strings.TrimSpace(h.Next().Text())
			blob := title + " " + block
			if !textMatches(blob, kw) {
				return
			}
			seen[full] = struct{}{}
			var pub *time.Time
			if block != "" {
				pub = parseRuDate(block)
			}
			out = append(out, domain.ParsedItem{
				Title:       truncate(title, 500),
				URL:         full,
				Summary:     truncate(block, 1000),
				PublishedAt: pub,
				Source:      "smart-lab.ru",
			})
		})
		doc.Find("a[href*='/blog/news/']").Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			full := absolutize("https://smart-lab.ru", href)
			if _, ok := seen[full]; ok {
				return
			}
			title := strings.TrimSpace(s.Text())
			if len(title) < 12 {
				return
			}
			blob := title
			if p := s.Parent(); p != nil && p.Length() > 0 {
				blob = strings.TrimSpace(p.Text())
			}
			if !textMatches(blob, kw) {
				return
			}
			seen[full] = struct{}{}
			pub := parseRuDate(blob)
			out = append(out, domain.ParsedItem{
				Title:       truncate(title, 500),
				URL:         full,
				Summary:     truncate(blob, 1000),
				PublishedAt: pub,
				Source:      "smart-lab.ru",
			})
		})
		if len(out) >= 40 {
			break
		}
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out, nil
}

func absolutize(base, href string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(href, "/")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func ParseFinam(ctx context.Context, client *http.Client, ua string, b *domain.Bond) ([]domain.ParsedItem, error) {
	kw := bondKeywords(b)
	if len(kw) == 0 {
		return nil, nil
	}
	t := b.ISIN
	if b.Ticker != nil && *b.Ticker != "" {
		t = *b.Ticker
	}
	u := finamBondsRoot + "?query=" + url.QueryEscape(t)
	html, err := getText(ctx, client, ua, u)
	if err != nil {
		return nil, nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, nil
	}
	var out []domain.ParsedItem
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if len(out) >= 20 {
			return
		}
		href, _ := s.Attr("href")
		title := strings.TrimSpace(s.Text())
		if len(title) < 8 {
			return
		}
		full := absolutize("https://bonds.finam.ru", href)
		if strings.HasPrefix(href, "http") {
			full = href
		}
		if !textMatches(title, kw) {
			return
		}
		out = append(out, domain.ParsedItem{
			Title:   truncate(title, 500),
			URL:     truncate(full, 2000),
			Summary: "",
			Source:  "bonds.finam.ru",
		})
	})
	return out, nil
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

type rssRoot struct {
	Channel rssChannel `xml:"channel"`
}

func ParseRSS(ctx context.Context, client *http.Client, ua, feedURL string, b *domain.Bond) ([]domain.ParsedItem, error) {
	body, err := getText(ctx, client, ua, feedURL)
	if err != nil {
		return nil, err
	}
	var root rssRoot
	if err := xml.Unmarshal([]byte(body), &root); err != nil {
		return nil, err
	}
	kw := bondKeywords(b)
	var out []domain.ParsedItem
	for _, it := range root.Channel.Items {
		title := strings.TrimSpace(it.Title)
		link := strings.TrimSpace(it.Link)
		if title == "" || link == "" {
			continue
		}
		blob := title + " " + strings.TrimSpace(it.Description)
		if len(kw) > 0 && !textMatches(blob, kw) {
			continue
		}
		var pub *time.Time
		if it.PubDate != "" {
			for _, layout := range []string{
				time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822,
				"Mon, 02 Jan 2006 15:04:05 -0700",
			} {
				if t, err := time.Parse(layout, strings.TrimSpace(it.PubDate)); err == nil {
					tt := t.In(time.Local)
					pub = &tt
					break
				}
			}
		}
		out = append(out, domain.ParsedItem{
			Title:       truncate(title, 500),
			URL:         truncate(link, 2000),
			Summary:     truncate(strings.TrimSpace(it.Description), 1000),
			PublishedAt: pub,
			Source:      "rss",
		})
	}
	return out, nil
}

func FetchArticleBody(ctx context.Context, client *http.Client, ua, pageURL string, maxLen int) string {
	html, err := getText(ctx, client, ua, pageURL)
	if err != nil || html == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	doc.Find("script, style, nav, header, footer").Remove()
	text := strings.TrimSpace(doc.Find("article").First().Text())
	if text == "" {
		text = strings.TrimSpace(doc.Find("main").First().Text())
	}
	if text == "" {
		doc.Find("div").Each(func(_ int, s *goquery.Selection) {
			if text != "" {
				return
			}
			cls, _ := s.Attr("class")
			if ok, _ := regexp.MatchString(`(?i)content|article|post`, cls); ok {
				text = strings.TrimSpace(s.Text())
			}
		})
	}
	if text == "" {
		text = strings.TrimSpace(doc.Find("body").Text())
	}
	if len(text) > maxLen {
		text = text[:maxLen]
	}
	return text
}

func Collect(ctx context.Context, client *http.Client, ua string, b *domain.Bond, rssURLs []string) ([]domain.ParsedItem, error) {
	byURL := map[string]domain.ParsedItem{}
	add := func(items []domain.ParsedItem) {
		for _, it := range items {
			if it.URL == "" {
				continue
			}
			if _, ok := byURL[it.URL]; !ok {
				byURL[it.URL] = it
			}
		}
	}
	sl, _ := ParseSmartLab(ctx, client, ua, b, 2)
	add(sl)
	fm, _ := ParseFinam(ctx, client, ua, b)
	add(fm)
	for _, u := range rssURLs {
		rs, _ := ParseRSS(ctx, client, ua, u, b)
		add(rs)
	}
	out := make([]domain.ParsedItem, 0, len(byURL))
	for _, v := range byURL {
		out = append(out, v)
	}
	return out, nil
}
