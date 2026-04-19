CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    telegram_id BIGINT UNIQUE NOT NULL,
    username VARCHAR(100),
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_active TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bonds (
    id SERIAL PRIMARY KEY,
    isin VARCHAR(12) UNIQUE NOT NULL,
    ticker VARCHAR(20),
    name VARCHAR(255) NOT NULL,
    issuer VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS subscriptions (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bond_id INTEGER NOT NULL REFERENCES bonds(id) ON DELETE CASCADE,
    subscribed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    is_active BOOLEAN DEFAULT TRUE,
    PRIMARY KEY (user_id, bond_id)
);

CREATE TABLE IF NOT EXISTS news (
    id SERIAL PRIMARY KEY,
    bond_id INTEGER NOT NULL REFERENCES bonds(id) ON DELETE CASCADE,
    title VARCHAR(500) NOT NULL,
    content TEXT,
    url VARCHAR(2000) UNIQUE NOT NULL,
    source VARCHAR(100),
    published_at TIMESTAMP,
    sentiment VARCHAR(10),
    sentiment_reason VARCHAR(255),
    analyzed_at TIMESTAMP,
    sent_to_users BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ck_news_sentiment CHECK (
        sentiment IS NULL OR sentiment IN ('POSITIVE', 'NEUTRAL', 'NEGATIVE')
    )
);

CREATE TABLE IF NOT EXISTS news_delivery (
    id SERIAL PRIMARY KEY,
    news_id INTEGER NOT NULL REFERENCES news(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sent_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_news_bond_id ON news(bond_id);
CREATE INDEX IF NOT EXISTS idx_news_sentiment ON news(sentiment);
CREATE INDEX IF NOT EXISTS idx_news_url ON news(url);
CREATE INDEX IF NOT EXISTS idx_news_published_at ON news(published_at);
CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id);
