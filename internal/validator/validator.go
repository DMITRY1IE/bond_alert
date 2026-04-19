package validator

import (
	"regexp"
	"strings"
)

var isinRe = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{9}[0-9]$`)
var tickerRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{2,19}$`)

func NormalizeBondIdentifier(raw string) string {
	return strings.ReplaceAll(strings.TrimSpace(strings.ToUpper(raw)), " ", "")
}

func LooksLikeISIN(s string) bool {
	return isinRe.MatchString(s)
}

func ValidateIdentifier(raw string) (ok bool, ident string, msg string) {
	s := NormalizeBondIdentifier(raw)
	if len(s) < 4 {
		return false, "", "Слишком короткий идентификатор."
	}
	if LooksLikeISIN(s) {
		return true, s, ""
	}
	if tickerRe.MatchString(s) {
		return true, s, ""
	}
	return false, "", "Укажите ISIN (12 символов) или тикер/SECID с биржи (латиница, цифры)."
}
