package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"bond_alert_gin/internal/domain"
	"bond_alert_gin/internal/validator"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) EnsureUser(ctx context.Context, telegramID int64, username, first, last *string) (int32, error) {
	const q = `
INSERT INTO users (telegram_id, username, first_name, last_name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (telegram_id) DO UPDATE SET
  username = COALESCE(EXCLUDED.username, users.username),
  first_name = COALESCE(EXCLUDED.first_name, users.first_name),
  last_name = COALESCE(EXCLUDED.last_name, users.last_name),
  last_active = CURRENT_TIMESTAMP
RETURNING id`
	var id int32
	err := s.pool.QueryRow(ctx, q, telegramID, username, first, last).Scan(&id)
	return id, err
}

func (s *Store) GetBondByISIN(ctx context.Context, isin string) (*domain.Bond, error) {
	const q = `SELECT id, isin, ticker, name, issuer FROM bonds WHERE isin = $1`
	var b domain.Bond
	err := s.pool.QueryRow(ctx, q, isin).Scan(&b.ID, &b.ISIN, &b.Ticker, &b.Name, &b.Issuer)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) UpsertBond(ctx context.Context, isin, ticker, name string, issuer *string) (*domain.Bond, error) {
	const q = `
INSERT INTO bonds (isin, ticker, name, issuer) VALUES ($1, $2, $3, $4)
ON CONFLICT (isin) DO UPDATE SET
  ticker = COALESCE(EXCLUDED.ticker, bonds.ticker),
  name = EXCLUDED.name,
  issuer = COALESCE(EXCLUDED.issuer, bonds.issuer),
  updated_at = CURRENT_TIMESTAMP
RETURNING id, isin, ticker, name, issuer`
	var b domain.Bond
	err := s.pool.QueryRow(ctx, q, isin, nullIfEmpty(ticker), name, issuer).Scan(&b.ID, &b.ISIN, &b.Ticker, &b.Name, &b.Issuer)
	return &b, err
}

func nullIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func (s *Store) SetSubscriptionActive(ctx context.Context, userID, bondID int32, active bool) error {
	const q = `
INSERT INTO subscriptions (user_id, bond_id, is_active) VALUES ($1, $2, $3)
ON CONFLICT (user_id, bond_id) DO UPDATE SET is_active = EXCLUDED.is_active`
	_, err := s.pool.Exec(ctx, q, userID, bondID, active)
	return err
}

func (s *Store) ListUserBonds(ctx context.Context, userID int32) ([]domain.Bond, error) {
	const q = `
SELECT b.id, b.isin, b.ticker, b.name, b.issuer
FROM bonds b
JOIN subscriptions s ON s.bond_id = b.id
WHERE s.user_id = $1 AND s.is_active = TRUE
ORDER BY b.name`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Bond
	for rows.Next() {
		var b domain.Bond
		if err := rows.Scan(&b.ID, &b.ISIN, &b.Ticker, &b.Name, &b.Issuer); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) FindBondForUserRemove(ctx context.Context, userID int32, ident string) (*domain.Bond, error) {
	ident = strings.TrimSpace(strings.ToUpper(ident))
	var q string
	var args []any
	if validator.LooksLikeISIN(ident) {
		q = `SELECT b.id, b.isin, b.ticker, b.name, b.issuer FROM bonds b
JOIN subscriptions s ON s.bond_id = b.id AND s.user_id = $1 AND s.is_active = TRUE
WHERE b.isin = $2 LIMIT 1`
		args = []any{userID, ident}
	} else {
		q = `SELECT b.id, b.isin, b.ticker, b.name, b.issuer FROM bonds b
JOIN subscriptions s ON s.bond_id = b.id AND s.user_id = $1 AND s.is_active = TRUE
WHERE UPPER(b.ticker) = $2 LIMIT 1`
		args = []any{userID, ident}
	}
	var b domain.Bond
	err := s.pool.QueryRow(ctx, q, args...).Scan(&b.ID, &b.ISIN, &b.Ticker, &b.Name, &b.Issuer)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) GetUserByTelegram(ctx context.Context, telegramID int64) (int32, bool, error) {
	const q = `SELECT id FROM users WHERE telegram_id = $1`
	var id int32
	err := s.pool.QueryRow(ctx, q, telegramID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return id, true, err
}

func (s *Store) ActiveSubscriptionBondIDs(ctx context.Context) ([]int32, error) {
	const q = `SELECT DISTINCT bond_id FROM subscriptions WHERE is_active = TRUE`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) GetBondByID(ctx context.Context, id int32) (*domain.Bond, error) {
	const q = `SELECT id, isin, ticker, name, issuer FROM bonds WHERE id = $1`
	var b domain.Bond
	err := s.pool.QueryRow(ctx, q, id).Scan(&b.ID, &b.ISIN, &b.Ticker, &b.Name, &b.Issuer)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &b, err
}

func (s *Store) NewsExistsByURL(ctx context.Context, url string) (bool, error) {
	const q = `SELECT 1 FROM news WHERE url = $1 LIMIT 1`
	var one int
	err := s.pool.QueryRow(ctx, q, url).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) InsertNews(ctx context.Context, bondID int32, title, content, url, source string, publishedAt *time.Time, sentiment, reason string, analyzedAt time.Time) (int32, error) {
	const q = `
INSERT INTO news (bond_id, title, content, url, source, published_at, sentiment, sentiment_reason, analyzed_at, sent_to_users)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, FALSE)
RETURNING id`
	var id int32
	err := s.pool.QueryRow(ctx, q, bondID, title, content, url, source, publishedAt, sentiment, reason, analyzedAt).Scan(&id)
	return id, err
}

type NewsRow struct {
	ID              int32
	BondID          int32
	Title           string
	Content         *string
	URL             string
	Sentiment       *string
	SentimentReason *string
	PublishedAt     *time.Time
	CreatedAt       time.Time
	Bond            domain.Bond
}

func (s *Store) GetNewsWithBond(ctx context.Context, newsID int32) (*NewsRow, error) {
	const q = `
SELECT n.id, n.bond_id, n.title, n.content, n.url, n.sentiment, n.sentiment_reason, n.published_at, n.created_at,
       b.id, b.isin, b.ticker, b.name, b.issuer
FROM news n JOIN bonds b ON b.id = n.bond_id WHERE n.id = $1`
	var n NewsRow
	var cont pgtype.Text
	var sent, sreason pgtype.Text
	var iss pgtype.Text
	err := s.pool.QueryRow(ctx, q, newsID).Scan(
		&n.ID, &n.BondID, &n.Title, &cont, &n.URL, &sent, &sreason, &n.PublishedAt, &n.CreatedAt,
		&n.Bond.ID, &n.Bond.ISIN, &n.Bond.Ticker, &n.Bond.Name, &iss,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cont.Valid {
		v := cont.String
		n.Content = &v
	}
	if sent.Valid {
		v := sent.String
		n.Sentiment = &v
	}
	if sreason.Valid {
		v := sreason.String
		n.SentimentReason = &v
	}
	if iss.Valid {
		v := iss.String
		n.Bond.Issuer = &v
	}
	return &n, nil
}

func (s *Store) SubscriberTelegramIDs(ctx context.Context, bondID int32) ([]struct {
	TelegramID int64
	UserID     int32
}, error) {
	const q = `
SELECT u.telegram_id, u.id FROM users u
JOIN subscriptions s ON s.user_id = u.id
WHERE s.bond_id = $1 AND s.is_active = TRUE`
	rows, err := s.pool.Query(ctx, q, bondID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		TelegramID int64
		UserID     int32
	}
	for rows.Next() {
		var r struct {
			TelegramID int64
			UserID     int32
		}
		if err := rows.Scan(&r.TelegramID, &r.UserID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) HasDelivery(ctx context.Context, newsID, userID int32) (bool, error) {
	const q = `SELECT 1 FROM news_delivery WHERE news_id = $1 AND user_id = $2 LIMIT 1`
	var one int
	err := s.pool.QueryRow(ctx, q, newsID, userID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) InsertDelivery(ctx context.Context, newsID, userID int32) error {
	const q = `INSERT INTO news_delivery (news_id, user_id) VALUES ($1, $2)`
	_, err := s.pool.Exec(ctx, q, newsID, userID)
	return err
}

func (s *Store) MarkNewsSent(ctx context.Context, newsID int32) error {
	const q = `UPDATE news SET sent_to_users = TRUE WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, newsID)
	return err
}

func (s *Store) IsSubscriptionActive(ctx context.Context, userID, bondID int32) (bool, error) {
	const q = `SELECT is_active FROM subscriptions WHERE user_id = $1 AND bond_id = $2`
	var active *bool
	err := s.pool.QueryRow(ctx, q, userID, bondID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return active != nil && *active, err
}
