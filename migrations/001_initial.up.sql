-- 001_initial.up.sql
-- Core schema for multi-tenant POS system

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Shops (tenants)
CREATE TABLE shops (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name          TEXT NOT NULL,
    owner_email   TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    tax_rate      NUMERIC(5,2) DEFAULT 0,
    currency      TEXT DEFAULT 'KES',
    phone         TEXT,
    address       TEXT,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

-- Roles within a shop
CREATE TABLE shop_users (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    shop_id       UUID NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    email         TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('owner', 'manager', 'cashier')),
    name          TEXT NOT NULL,
    phone         TEXT,
    active        BOOLEAN DEFAULT true,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_shop_users_shop ON shop_users(shop_id);
CREATE INDEX idx_shop_users_email ON shop_users(email);

-- Products
CREATE TABLE products (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    shop_id    UUID NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    sku        TEXT,
    category   TEXT,
    cost_price BIGINT NOT NULL DEFAULT 0,
    sell_price BIGINT NOT NULL DEFAULT 0,
    stock_qty  BIGINT NOT NULL DEFAULT 0,
    min_stock  BIGINT NOT NULL DEFAULT 5,
    active     BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_products_shop ON products(shop_id);
CREATE INDEX idx_products_sku ON products(shop_id, sku);

-- Workers (cashiers on shift, separate from shop_users for quick PIN access)
CREATE TABLE workers (
    id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    shop_id UUID NOT NULL REFERENCES shops(id) ON DELETE CASCADE,
    name    TEXT NOT NULL,
    pin     TEXT,
    active  BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_workers_shop ON workers(shop_id);

-- Sessions (active worker shifts)
CREATE TABLE sessions (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    shop_id       UUID NOT NULL REFERENCES shops(id),
    worker_id     UUID NOT NULL REFERENCES workers(id),
    started_at    TIMESTAMPTZ DEFAULT NOW(),
    ended_at      TIMESTAMPTZ,
    opening_float BIGINT DEFAULT 0,
    closing_cash  BIGINT,
    closing_mpesa BIGINT,
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed')),
    notes         TEXT
);
CREATE INDEX idx_sessions_shop ON sessions(shop_id, status);
CREATE INDEX idx_sessions_worker ON sessions(worker_id, status);

-- THE CORE: Immutable Event Log (hash-chained, per-shop)
CREATE TABLE events (
    id            BIGSERIAL PRIMARY KEY,
    shop_id       UUID NOT NULL REFERENCES shops(id),
    event_seq     BIGINT NOT NULL,
    event_type    TEXT NOT NULL CHECK (event_type IN ('stock_in', 'sale', 'reconciliation', 'session_open', 'session_close', 'price_change', 'void')),
    event_data    JSONB NOT NULL,
    previous_hash TEXT NOT NULL DEFAULT '',
    event_hash    TEXT NOT NULL,
    created_by    UUID REFERENCES shop_users(id),
    created_at    TIMESTAMPTZ DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_events_shop_seq ON events(shop_id, event_seq);
CREATE INDEX idx_events_shop_type ON events(shop_id, event_type);
CREATE INDEX idx_events_created_by ON events(created_by);

-- M-Pesa transactions
CREATE TABLE mpesa_transactions (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    shop_id         UUID NOT NULL REFERENCES shops(id),
    mpesa_receipt   TEXT UNIQUE NOT NULL,
    phone_number    TEXT NOT NULL,
    amount          BIGINT NOT NULL,
    event_id        BIGINT REFERENCES events(id),
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'failed')),
    created_at      TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_mpesa_shop ON mpesa_transactions(shop_id);

-- Audit trail (who did what, separate from immutable event ledger)
CREATE TABLE audit_log (
    id         BIGSERIAL PRIMARY KEY,
    shop_id    UUID NOT NULL REFERENCES shops(id),
    user_id    UUID REFERENCES shop_users(id),
    action     TEXT NOT NULL,
    details    JSONB,
    ip_address TEXT,
    user_agent TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_audit_shop ON audit_log(shop_id);
CREATE INDEX idx_audit_user ON audit_log(user_id);

-- System health metrics (for admin CLI diagnostics)
CREATE TABLE system_metrics (
    id          BIGSERIAL PRIMARY KEY,
    shop_id     UUID REFERENCES shops(id),
    metric_name TEXT NOT NULL,
    metric_value TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_metrics_name ON system_metrics(metric_name, created_at);
