package parser

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"bond_alert_gin/internal/domain"
)

func TestTextMatches(t *testing.T) {
	bond := &domain.Bond{
		ISIN:   "RU000A10EST2",
		Name:   "ЭталонФин5",
		Issuer: strPtr("Акционерное общество \"Эталон-Финанс\""),
	}
	kw := bondKeywords(bond)

	tests := []struct {
		text     string
		expected bool
	}{
		{"Эталон-Финанс выпустил облигации", true},
		{"Банки помогают заемщикам, облегчая финансовые трудности", false},
		{"ЭТАЛОНФИН5 - новая облигация", true},
		{"финансовые новости", false},
		{"АО Эталон-Финанс", true},
		{"RU000A10EST2", true},
	}

	for _, tt := range tests {
		result := textMatches(tt.text, kw)
		if result != tt.expected {
			t.Errorf("textMatches(%q) = %v, want %v", tt.text, result, tt.expected)
		}
	}
}

func TestTextMatchesMultipleWords(t *testing.T) {
	bond := &domain.Bond{
		ISIN:   "RU000A10AHE5",
		Name:   "НовТех1Р2",
		Issuer: strPtr("Общество с ограниченной ответственностью \"Новые технологии\""),
	}
	kw := bondKeywords(bond)

	tests := []struct {
		text     string
		expected bool
	}{
		{"НовТех1Р2 получил кредит", true},
		{"новых технологий в России", false},
		{"НовТех1Р2 - новая облигация", true},
		{"RU000A10AHE5", true},
		{"Крупнейшие ритейлеры запустили 418 новых СТМ", false},
		{"В России растет средний срок автокредитов", false},
		{"технологии технологии развиваются", true},
	}

	for _, tt := range tests {
		result := textMatches(tt.text, kw)
		if result != tt.expected {
			t.Errorf("textMatches(%q) = %v, want %v", tt.text, result, tt.expected)
		}
	}
}

func TestParseFinamRSS(t *testing.T) {
	client := &http.Client{Timeout: 30 * time.Second}
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

	tests := []struct {
		name     string
		bond     *domain.Bond
		expected int
	}{
		{
			name: "no_match_etalon",
			bond: &domain.Bond{
				ISIN:   "RU000A10EST2",
				Name:   "ЭталонФин5",
				Issuer: strPtr("Акционерное общество \"Эталон-Финанс\""),
			},
			expected: 0,
		},
		{
			name: "no_match_segeza",
			bond: &domain.Bond{
				ISIN:   "RU000A10DY50",
				Name:   "Сегежа3P8R",
				Issuer: strPtr("Публичное акционерное общество Группа компаний \"Сегежа\""),
			},
			expected: 0,
		},
		{
			name: "no_match_digi_group",
			bond: &domain.Bond{
				ISIN:   "RU000A10B1Q6",
				Name:   "Джи-гр 2P6",
				Issuer: strPtr("Акционерное общество \"Джи-групп\""),
			},
			expected: 0,
		},
		{
			name: "no_match_dars",
			bond: &domain.Bond{
				ISIN:   "RU000A10B8X7",
				Name:   "ДАРСДев1Р3",
				Issuer: strPtr("Общество с ограниченной ответственностью \"ДАРС-Девелопмент\""),
			},
			expected: 0,
		},
		{
			name: "no_match_sell_service",
			bond: &domain.Bond{
				ISIN:   "RU000A107GT6",
				Name:   "СЕЛЛСерв1",
				Issuer: strPtr("Общество с ограниченной ответственностью \"СЕЛЛ-Сервис\""),
			},
			expected: 0,
		},
		{
			name: "no_match_pkb",
			bond: &domain.Bond{
				ISIN:   "RU000A10BGU1",
				Name:   "ПКБ 1Р-07",
				Issuer: strPtr("Непубличное акционерное общество Профессиональная коллекторская организация \"Первое клиентское бюро\""),
			},
			expected: 0,
		},
		{
			name: "vtb_match",
			bond: &domain.Bond{
				ISIN:   "RU000A0JWUE9",
				Name:   "ВТБ Банк",
				Issuer: strPtr("ВТБ"),
			},
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := ParseFinamRSS(context.Background(), client, ua, tt.bond)
			if err != nil {
				t.Fatalf("ParseFinamRSS error: %v", err)
			}

			if tt.expected >= 0 && len(items) != tt.expected {
				kw := bondKeywords(tt.bond)
				t.Errorf("Expected %d items, got %d", tt.expected, len(items))
				for _, item := range items {
					blob := item.Title + " " + item.Summary
					t.Logf("  - %s (matched: %v)", item.Title, extractMatchedWords(blob, kw))
				}
			}
			if tt.expected < 0 && len(items) == 0 {
				t.Errorf("Expected some items, got 0")
			}
		})
	}
}

func extractMatchedWords(text string, kw bondMatch) []string {
	var found []string
	for _, word := range regexp.MustCompile(`[^\p{L}\p{N}]+`).Split(strings.ToUpper(text), -1) {
		if len(word) >= 3 {
			if _, ok := kw.exact[word]; ok {
				found = append(found, word+"(exact)")
			}
			if _, ok := kw.keywords[word]; ok {
				found = append(found, word)
			}
		}
	}
	return found
}

func TestSmartLabSellService(t *testing.T) {
	bond := &domain.Bond{
		ISIN:   "RU000A107GT6",
		Name:   "СЕЛЛСерв1",
		Issuer: strPtr("Общество с ограниченной ответственностью \"СЕЛЛ-Сервис\""),
	}

	kw := bondKeywords(bond)

	news := "Государство в 2025 г. выделило VK более 43,5 млрд руб"
	if textMatches(news, kw) {
		t.Errorf("News should NOT match for SELL-Service: %q", news)
	}
}

func TestSmartLabPKB(t *testing.T) {
	bond := &domain.Bond{
		ISIN:   "RU000A10BGU1",
		Name:   "ПКБ 1Р-07",
		Issuer: strPtr("Непубличное акционерное общество Профессиональная коллекторская организация \"Первое клиентское бюро\""),
	}

	kw := bondKeywords(bond)

	news := "ВВП России в I кв. 2026 г. снизился на 0,3%"
	if textMatches(news, kw) {
		t.Errorf("News should NOT match for PKB: %q", news)
	}
}

func TestTGK14(t *testing.T) {
	bond := &domain.Bond{
		ISIN:   "RU000A10AS02",
		Name:   "ТГК-14 1Р5",
		Issuer: strPtr("Публичное акционерное общество \"Территориальная генерирующая компания № 14\""),
	}

	kw := bondKeywords(bond)

	news := "ТГК-1 не будет публиковать отчетность по РСБУ за 1кв 2026г"
	if textMatches(news, kw) {
		t.Errorf("News should NOT match for TGK-14 (it's about TGK-1): %q", news)
	}

	news2 := "ТГК-14 выпустила новые облигации"
	if !textMatches(news2, kw) {
		t.Errorf("News SHOULD match for TGK-14: %q", news2)
	}
}

func strPtr(s string) *string {
	return &s
}
