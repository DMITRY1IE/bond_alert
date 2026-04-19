package domain

import "time"

type Bond struct {
	ID     int32
	ISIN   string
	Ticker *string
	Name   string
	Issuer *string
}

type ResolvedBond struct {
	ISIN   string
	Ticker string
	Name   string
	Issuer *string
}

type ParsedItem struct {
	Title       string
	URL         string
	Summary     string
	PublishedAt *time.Time
	Source      string
}
