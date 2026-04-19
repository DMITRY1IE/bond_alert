package moex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bond_alert_gin/internal/domain"
	"bond_alert_gin/internal/validator"
)

const (
	securitiesSearch = "https://iss.moex.com/iss/securities.json"
	bondMarket       = "https://iss.moex.com/iss/engines/stock/markets/bonds/securities.json"
)

type issTable struct {
	Columns []string          `json:"columns"`
	Data    [][]json.RawMessage `json:"data"`
}

type issResponse struct {
	Securities issTable `json:"securities"`
}

func rowMaps(t issTable) []map[string]any {
	out := make([]map[string]any, 0, len(t.Data))
	for _, row := range t.Data {
		m := make(map[string]any)
		for i, col := range t.Columns {
			if i >= len(row) {
				break
			}
			var v any
			_ = json.Unmarshal(row[i], &v)
			lk := strings.ToLower(col)
			m[lk] = v
		}
		out = append(out, m)
	}
	return out
}

func str(m map[string]any, key string) string {
	key = strings.ToLower(key)
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	default:
		return fmt.Sprint(t)
	}
}

func getJSON(ctx context.Context, client *http.Client, rawURL string, params url.Values) ([]map[string]any, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(b))
	}
	var body issResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return rowMaps(body.Securities), nil
}

func bondRows(rows []map[string]any) []map[string]any {
	types := map[string]struct{}{
		"corporate_bond": {}, "ofz_bond": {}, "subfederal_bond": {},
		"exchange_bond": {}, "municipal_bond": {},
	}
	var br []map[string]any
	for _, r := range rows {
		t := str(r, "type")
		if _, ok := types[t]; ok {
			br = append(br, r)
		}
	}
	if len(br) == 0 {
		for _, r := range rows {
			if strings.HasSuffix(str(r, "group"), "bonds") {
				br = append(br, r)
			}
		}
	}
	if len(br) == 0 {
		return rows
	}
	return br
}

func moexBondDetail(ctx context.Context, client *http.Client, secid string) (map[string]any, error) {
	rows, err := getJSON(ctx, client, bondMarket, url.Values{"securities": {secid}, "iss.meta": {"off"}})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	for _, r := range rows {
		if str(r, "BOARDID") == "TQOB" {
			return r, nil
		}
	}
	return rows[0], nil
}

func ResolveBond(ctx context.Context, userAgent, raw string) (*domain.ResolvedBond, error) {
	ident := validator.NormalizeBondIdentifier(raw)
	client := &http.Client{Timeout: 30 * time.Second}
	if userAgent != "" {
		client.Transport = &headerRoundTripper{ua: userAgent, next: http.DefaultTransport}
	}

	rows, err := getJSON(ctx, client, securitiesSearch, url.Values{"q": {ident}, "iss.meta": {"off"}})
	if err != nil {
		return nil, err
	}
	br := bondRows(rows)
	var picked map[string]any
	if validator.LooksLikeISIN(ident) {
		for _, r := range br {
			if strings.ToUpper(str(r, "isin")) == ident {
				picked = r
				break
			}
		}
		if picked == nil {
			for _, r := range rows {
				if strings.ToUpper(str(r, "isin")) == ident {
					picked = r
					break
				}
			}
		}
	} else {
		ui := strings.ToUpper(ident)
		for _, r := range br {
			if strings.ToUpper(str(r, "secid")) == ui || strings.ToUpper(str(r, "shortname")) == ui {
				picked = r
				break
			}
		}
		if picked == nil && len(rows) > 0 {
			picked = rows[0]
		}
	}
	if picked == nil {
		return nil, nil
	}
	secid := str(picked, "secid")
	isin := strings.ToUpper(str(picked, "isin"))
	name := str(picked, "shortname")
	if name == "" {
		name = str(picked, "name")
	}
	if name == "" {
		name = ident
	}
	issuer := str(picked, "emitent_title")
	var iss *string
	if strings.TrimSpace(issuer) != "" {
		issuer = truncate(issuer, 255)
		iss = &issuer
	}
	if secid != "" {
		if d, err := moexBondDetail(ctx, client, secid); err == nil && d != nil {
			if v := strings.ToUpper(str(d, "ISIN")); v != "" {
				isin = v
			}
			if v := str(d, "SHORTNAME"); v != "" {
				name = v
			} else if v := str(d, "SECNAME"); v != "" {
				name = v
			}
		}
	}
	if isin == "" && validator.LooksLikeISIN(ident) {
		isin = ident
	}
	if isin == "" {
		return nil, nil
	}
	name = truncate(name, 255)
	return &domain.ResolvedBond{ISIN: isin, Ticker: secid, Name: name, Issuer: iss}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type headerRoundTripper struct {
	ua   string
	next http.RoundTripper
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("User-Agent", h.ua)
	return h.next.RoundTrip(r)
}
