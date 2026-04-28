package parser

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/charmap"

	"bond_alert_gin/internal/domain"
)

const (
	smartLabNews = "https://smart-lab.ru/blog/news/"
	finamNewsRSS = "https://bonds.finam.ru/news/today/rss.asp"
)

type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

var requestLimiter = time.NewTicker(100 * time.Millisecond)

func waitForRateLimit() {
	<-requestLimiter.C
}

var stopWords = map[string]struct{}{
	"АО": {}, "ПАО": {}, "ОАО": {}, "ООО": {}, "ЗАО": {},
	"БАНК": {}, "КОМПАНИЯ": {}, "ХОЛДИНГ": {}, "ГРУППА": {},
	"ОБЛИГАЦИИ": {}, "ОБЛИГАЦИЯ": {}, "БО": {}, "КЛ": {},
	"СЕРИИ": {}, "СЕРИЯ": {},
	"ОБЩЕСТВО": {}, "АКЦИОНЕРНОЕ": {}, "ОТКРЫТОЕ": {}, "ЗАКРЫТОЕ": {},
	"ЛТД": {}, "ИНК": {}, "ГМБХ": {},
	"ОБ": {}, "ИО": {}, "МР": {}, "ПР": {},
}

func bondKeywords(b *domain.Bond) map[string]struct{} {
	parts := make(map[string]struct{})
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) >= 3 {
			if _, stop := stopWords[strings.ToUpper(s)]; !stop {
				parts[strings.ToUpper(s)] = struct{}{}
			}
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
	if b.Issuer != nil && *b.Issuer != "" {
		parts[strings.ToUpper(*b.Issuer)] = struct{}{}
		for _, w := range regexp.MustCompile(`[^\p{L}\p{N}]+`).Split(*b.Issuer, -1) {
			add(w)
		}
	}
	return parts
}

func textMatches(text string, kw map[string]struct{}) bool {
	for _, word := range regexp.MustCompile(`[^\p{L}\p{N}]+`).Split(text, -1) {
		if len(word) >= 3 {
			if _, ok := kw[strings.ToUpper(word)]; ok {
				return true
			}
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
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
		waitForRateLimit()
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

func getTextWindows1251(ctx context.Context, client *http.Client, ua, urlStr string) (string, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
		waitForRateLimit()
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
		decoded, err := charmap.Windows1251.NewDecoder().Bytes(b)
		if err != nil {
			return string(b), nil
		}
		return string(decoded), nil
	}
	return "", last
}

func ParseSmartLab(ctx context.Context, client *http.Client, ua string, b *domain.Bond, maxPages int) ([]domain.ParsedItem, error) {
	kw := bondKeywords(b)
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
			if len(kw) > 0 && !textMatches(blob, kw) {
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
			if len(kw) > 0 && !textMatches(blob, kw) {
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

func FetchArticleBody(ctx context.Context, client *http.Client, ua, pageURL string, maxLen int) string {
	html, err := getText(ctx, client, ua, pageURL)
	if err != nil || html == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	doc.Find("script, style, nav, header, footer, form, .login, .auth, .authorization, .sidebar, .comments").Remove()
	doc.Find("[class*='login'], [class*='auth'], [class*='register'], [id*='login'], [id*='auth']").Remove()

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
			if ok, _ := regexp.MatchString(`(?i)content|article|post|news-item|entry`, cls); ok {
				text = strings.TrimSpace(s.Text())
			}
		})
	}
	if text == "" {
		text = strings.TrimSpace(doc.Find("body").Text())
	}

	text = filterGarbage(text)
	if len(text) > maxLen {
		text = text[:maxLen]
	}
	return text
}

func filterGarbage(text string) string {
	garbagePatterns := []string{
		"Авторизация", "Зарегистрироваться", "Логин", "Пароль",
		"Напомнить пароль", "Войти", "Запомнить меня",
		"Регистрация", "Восстановление пароля",
	}
	lines := strings.Split(text, "\n")
	var filtered []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		isGarbage := false
		upper := strings.ToUpper(line)
		for _, g := range garbagePatterns {
			if strings.Contains(upper, strings.ToUpper(g)) && len(line) < 50 {
				isGarbage = true
				break
			}
		}
		if !isGarbage {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

func Collect(ctx context.Context, client *http.Client, ua string, b *domain.Bond) ([]domain.ParsedItem, error) {
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
	fn, _ := ParseFinamRSS(ctx, client, ua, b)
	add(fn)
	out := make([]domain.ParsedItem, 0, len(byURL))
	for _, v := range byURL {
		out = append(out, v)
	}
	return out, nil
}

func ParseFinamRSS(ctx context.Context, client *http.Client, ua string, b *domain.Bond) ([]domain.ParsedItem, error) {
	kw := bondKeywords(b)
	raw, err := getTextWindows1251(ctx, client, ua, finamNewsRSS)
	if err != nil || raw == "" {
		return nil, err
	}
	raw = regexp.MustCompile(`encoding="[^"]+"`).ReplaceAllString(raw, `encoding="UTF-8"`)
	var feed rssFeed
	if err := xml.Unmarshal([]byte(raw), &feed); err != nil {
		return nil, err
	}
	var out []domain.ParsedItem
	seen := map[string]struct{}{}
	for _, item := range feed.Channel.Items {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.Link)
		if title == "" || link == "" {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		desc := stripHTML(item.Description)
		blob := title + " " + desc
		matched := textMatches(blob, kw)
		if len(kw) > 0 && !matched {
			continue
		}
		seen[link] = struct{}{}
		var pub *time.Time
		if t := parseRSSDate(item.PubDate); t != nil {
			pub = t
		}
		out = append(out, domain.ParsedItem{
			Title:       truncate(title, 500),
			URL:         link,
			Summary:     truncate(desc, 1000),
			Source:      "bonds.finam.ru",
			PublishedAt: pub,
		})
		if len(out) >= 50 {
			break
		}
	}
	return out, nil
}

func stripHTML(s string) string {
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
	s = htmlEntityRe.ReplaceAllString(s, " ")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

var htmlEntityRe = regexp.MustCompile(`&[a-zA-Z]+;`)

func parseRSSDate(s string) *time.Time {
	layouts := []string{
		time.RFC1123,
		time.RFC1123Z,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 02 Jan 2006 15:04:05 -0700",
	}
	for _, lay := range layouts {
		if t, err := time.Parse(lay, s); err == nil {
			return &t
		}
	}
	return nil
}
