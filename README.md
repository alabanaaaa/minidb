# Mini-Database: Retail POS Financial Integrity Platform

A multi-tenant point-of-sale and financial integrity platform built in Go with event sourcing, hash-chained audit trails, anomaly detection, and remote administration. Designed for small-to-medium retail shops in East Africa with M-Pesa integration, worker accountability, and owner oversight from anywhere.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                          ACCESS LAYER                                 │
│                                                                       │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────────────────────┐   │
│  │  Web Browser │  │  Admin CLI   │  │  Mobile / POS Terminal    │   │
│  │  (HTMX UI)   │  │  (Remote)    │  │  (Future App)             │   │
│  └──────┬───────┘  └──────┬───────┘  └──────────┬────────────────┘   │
│         │                 │                      │                    │
└─────────┼─────────────────┼──────────────────────┼────────────────────┘
          │                 │                      │
          │  HTTPS          │  SSH / Direct        │  HTTPS
          ▼                 ▼                      ▼
┌──────────────────────────────────────────────────────────────────────┐
│                        APPLICATION LAYER                              │
│                                                                       │
│  ┌────────────────────────────────────────────────────────────────┐   │
│  │  Caddy / Traefik (Reverse Proxy — Auto TLS)                    │   │
│  └────────────────────────┬───────────────────────────────────────┘   │
│                           │                                           │
│  ┌────────────────────────▼───────────────────────────────────────┐   │
│  │  Go HTTP Server (:8080) — chi router                           │   │
│  │                                                                 │   │
│  │  ┌───────────┐ ┌──────────┐ ┌────────────┐ ┌───────────────┐  │   │
│  │  │  Auth     │ │  API     │ │  Event     │ │  Ghost Mode   │  │   │
│  │  │  (JWT)    │ │  Routes  │ │  Engine    │ │  (Anomaly     │  │   │
│  │  │           │ │  (25)    │ │  (Hash-    │ │   Detection)  │  │   │
│  │  │           │ │          │ │   chained) │ │               │  │   │
│  │  └───────────┘ └──────────┘ └────────────┘ └───────────────┘  │   │
│  │                                                                 │   │
│  │  ┌───────────┐ ┌──────────┐ ┌────────────┐ ┌───────────────┐  │   │
│  │  │Middleware │ │ Reports  │ │ M-Pesa     │ │ Structured    │  │   │
│  │  │(Auth,     │ │ & Export │ │ (Daraja)   │ │ Logging       │  │   │
│  │  │ CORS,     │ │          │ │ Webhooks   │ │ (slog)        │  │   │
│  │  │ Recovery) │ │          │ │            │ │               │  │   │
│  │  └───────────┘ └──────────┘ └────────────┘ └───────────────┘  │   │
│  └────────────────────────────────────────────────────────────────┘   │
│                           │                                           │
└───────────────────────────┼───────────────────────────────────────────┘
                            │
┌───────────────────────────▼───────────────────────────────────────────┐
│                        DATA LAYER                                      │
│                                                                       │
│  ┌────────────────────────────────────────────────────────────────┐   │
│  │  PostgreSQL 16 (Multi-tenant, Row-Level Security)              │   │
│  │                                                                 │   │
│  │  ┌─────────┐ ┌─────────┐ ┌──────────┐ ┌──────────┐           │   │
│  │  │ shops   │ │ events  │ │ products │ │ sessions │           │   │
│  │  │         │ │(hash-   │ │          │ │          │           │   │
│  │  │         │ │ chained)│ │          │ │          │           │   │
│  │  └─────────┘ └─────────┘ └──────────┘ └──────────┘           │   │
│  │  ┌─────────┐ ┌─────────┐ ┌──────────┐ ┌──────────┐           │   │
│  │  │workers  │ │ mpesa   │ │ audit_   │ │ shop_    │           │   │
│  │  │         │ │ trans.  │ │ log      │ │ users    │           │   │
│  │  └─────────┘ └─────────┘ └──────────┘ └──────────┘           │   │
│  └────────────────────────────────────────────────────────────────┘   │
│                                                                       │
│  ┌────────────────────────────────────────────────────────────────┐   │
│  │  Custom KV Store (Legacy — pos.db, append-only with CRC32)     │   │
│  │  ┌──────────────────────────────────────────────────────────┐  │   │
│  │  │  [keySize|valSize|tomb|key|value|CRC32] → file.Sync()    │  │   │
│  │  │  In-memory index: map[string]int64 (key → file offset)   │  │   │
│  │  │  Crash recovery: Replay() walks file, rebuilds index     │  │   │
│  │  └──────────────────────────────────────────────────────────┘  │   │
│  └────────────────────────────────────────────────────────────────┘   │
└───────────────────────────────────────────────────────────────────────┘
```

---

## How It Works: Top to Bottom

### The Complete Sale Flow

```
Terminal / Browser: POST /api/sales
  {
    "product_id": "prod-abc-123",
    "quantity": 2,
    "price": 100,
    "worker_id": "worker-001",
    "payment": 1
  }

  │
  ▼
┌─────────────────────────────────────────────────────────────────┐
│  1. HTTP SERVER (cmd/server/main.go)                             │
│     • Graceful shutdown handler (SIGINT/SIGTERM)                │
│     • Structured JSON logging (slog)                            │
│     • chi router with middleware stack                          │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│  2. MIDDLEWARE CHAIN (internal/api/middleware.go)                │
│     ┌──────────────────────────────────────────────────────┐    │
│     │ RequestID → RealIP → Recoverer → Timeout(30s)       │    │
│     │ CORS → RequestLogger → AuthMiddleware               │    │
│     └──────────────────────────────────────────────────────┘    │
│                                                                  │
│     AuthMiddleware:                                              │
│     ├── Extract "Bearer <token>" from Authorization header      │
│     ├── Verify JWT signature (HS256)                            │
│     ├── Decode claims: {user_id, shop_id, email, role}          │
│     └── Inject into request context                             │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│  3. HANDLER: recordSale (internal/api/handlers.go)              │
│     ┌──────────────────────────────────────────────────────┐    │
│     │ a. Decode JSON body                                  │    │
│     │ b. Validate: product_id, quantity>0, price>=0,       │    │
│     │    worker_id, payment in {1,2}                       │    │
│     │ c. Extract shop_id + user_id from context            │    │
│     └──────────────────────────────────────────────────────┘    │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│  4. DATABASE TRANSACTION (internal/db/db.go)                    │
│     ┌──────────────────────────────────────────────────────┐    │
│     │ tx := db.BeginTx()                                   │    │
│     │                                                      │    │
│     │ a. SELECT stock_qty FROM products                    │    │
│     │    WHERE id = ? AND shop_id = ? AND active = true    │    │
│     │    → currentStock = 98                               │    │
│     │                                                      │    │
│     │ b. CHECK: currentStock >= quantity? (98 >= 2 ✓)      │    │
│     │    → Fail with "insufficient stock" if not           │    │
│     │                                                      │    │
│     │ c. UPDATE products SET stock_qty = stock_qty - 2     │    │
│     │    WHERE id = ? AND shop_id = ?                      │    │
│     │                                                      │    │
│     │ d. Build event data:                                 │    │
│     │    {"product_id":"...","quantity":2,"price":100,     │    │
│     │     "worker_id":"...","payment":1,"total":200}       │    │
│     │                                                      │    │
│     │ e. Compute hash chain:                               │    │
│     │    previousHash = last event_hash from DB            │    │
│     │    eventHash = SHA256(eventData + previousHash)      │    │
│     │                                                      │    │
│     │ f. INSERT INTO events:                               │    │
│     │    (shop_id, event_seq, event_type, event_data,      │    │
│     │     previous_hash, event_hash, created_by)           │    │
│     │                                                      │    │
│     │ g. tx.Commit() → ALL or NOTHING                      │    │
│     └──────────────────────────────────────────────────────┘    │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│  5. POSTGRESQL (migrations/001_initial.up.sql)                  │
│     ┌──────────────────────────────────────────────────────┐    │
│     │ events table (append-only, immutable):               │    │
│     │ ┌────┬─────────┬─────────┬──────────┬─────────────┐ │    │
│     │ │ id │ shop_id │ seq     │ type     │ event_data  │ │    │
│     │ ├────┼─────────┼─────────┼──────────┼─────────────┤ │    │
│     │ │ 1  │ shop-A  │ 1       │ stock_in │ {...}       │ │    │
│     │ │ 2  │ shop-A  │ 2       │ sale     │ {...}       │ │    │
│     │ │ 3  │ shop-A  │ 3       │ sale     │ {...}       │ │    │
│     │ └────┴─────────┴─────────┴──────────┴─────────────┘ │    │
│     │                                                      │    │
│     │ Hash chain:                                          │    │
│     │ event#1: hash = SHA256(data#1 + "")                 │    │
│     │ event#2: hash = SHA256(data#2 + hash#1)             │    │
│     │ event#3: hash = SHA256(data#3 + hash#2)             │    │
│     │                                                      │    │
│     │ Tamper any event → chain breaks → detection!        │    │
│     └──────────────────────────────────────────────────────┘    │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│  6. RESPONSE                                                     │
│     HTTP 201 Created                                            │
│     {                                                           │
│       "status": "ok",                                           │
│       "product": "prod-abc-123",                                │
│       "quantity": 2,                                            │
│       "total": 200                                              │
│     }                                                           │
│                                                                  │
│     Log: {"level":"info","msg":"sale recorded",                 │
│       "product":"prod-abc-123","qty":2,"total":200,             │
│       "shop":"shop-A","method":"POST","path":"/api/sales",      │
│       "status":201,"duration":12,"remote":"192.168.1.50"}       │
└─────────────────────────────────────────────────────────────────┘
```

### Crash Recovery Flow

```
Machine dies mid-operation → power loss / crash

  Next startup: ./pos inventory check apple
  OR: server restarts

  │
  ▼
┌─────────────────────────────────────────────────────────────────┐
│  1. OpenDB("pos.db")                                             │
│     ├── OpenStorage("pos.db") → open append-only file          │
│     ├── Replay() → walk entire file from offset 0              │
│     │   ├── Read record header: [keySize|valueSize|tombstone]  │
│     │   ├── Read key + value data                              │
│     │   ├── Verify CRC32 checksum → detect corruption          │
│     │   ├── If tombstone: delete from index                    │
│     │   └── If valid: index[key] = offset                      │
│     └── OpenSnapshotManager("pos.db.snap")                     │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│  2. Engine.loadEvents()                                          │
│     ├── Try loadSnapshot() → restore inventory from checkpoint  │
│     ├── Read event_index from DB                                │
│     ├── For i = 1 to N:                                         │
│     │   ├── db.Get("event:i")                                   │
│     │   ├── json.Unmarshal → Event                              │
│     │   ├── applyEvent(event) → replay deterministically        │
│     │   └── e.events = append(e.events, event)                 │
│     └── State fully restored — zero data loss                   │
└────────────────────────┬────────────────────────────────────────┘
                         │
                         ▼
  Command executes on restored state → user sees correct stock
```

### Ghost Mode Detection Flow

```
Terminal: ./pos ghost run
OR:       GET /api/ghost (with auth token)
OR:       ./admin ghost --shop <uuid>

  │
  ▼
┌─────────────────────────────────────────────────────────────────┐
│  RUN GHOST MODE (engine/ghost_mode.go)                          │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 1. detectVariancePatterns(events)                        │   │
│  │    ├── Group reconciliations by worker_id                │   │
│  │    ├── For each worker with 2+ reconciliations:          │   │
│  │    │   ├── Count negative variance occurrences           │   │
│  │    │   └── If short rate >= 50% → flag anomaly           │   │
│  │    └── Severity: Medium(50%) → High(80%) → Critical(95%) │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 2. detectConsecutiveShort(events)                        │   │
│  │    ├── Find longest streak of consecutive short shifts   │   │
│  │    └── 3+ streak → flag (Medium → High → Critical)       │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 3. detectPriceManipulation(events)                       │   │
│  │    ├── Group sales by (product_id, worker_id)            │   │
│  │    ├── Calculate price spread: (max - min) / min         │   │
│  │    └── If spread > 20% → flag (Low → Medium → High)      │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 4. detectOffHoursActivity(events)                        │   │
│  │    └── Any sale before 6am or after 10pm → flag (Low)    │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 5. detectStockDrift(events)                              │   │
│  │    ├── Sum all stock_in per product                      │   │
│  │    ├── Sum all sales per product                         │   │
│  │    ├── expected = stock_in - sales                       │   │
│  │    └── If expected != current → flag                     │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 6. calculateRiskScore()                                  │   │
│  │    └── Low=5pts, Medium=15pts, High=30pts, Critical=50pts│   │
│  │    └── Cap at 100                                        │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  OUTPUT:                                                         │
│  {                                                               │
│    "shop_id": "shop-A",                                         │
│    "anomalies": [                                               │
│      {                                                          │
│        "type": "variance_pattern",                              │
│        "severity": "critical",                                  │
│        "worker": "W1",                                          │
│        "description": "Worker W1 short in 5/5 reconciliations", │
│        "short_rate": 100,                                       │
│        "total_variance": -2100                                  │
│      }                                                          │
│    ],                                                           │
│    "risk_score": 85,                                            │
│    "generated_at": "2026-03-31T10:30:00Z"                       │
│  }                                                               │
└─────────────────────────────────────────────────────────────────┘
```

### Admin CLI Remote Diagnostics Flow

```
You (at home) ──SSH──→ Server ──Query──→ PostgreSQL

  │
  ▼
┌─────────────────────────────────────────────────────────────────┐
│  ./admin status                                                  │
│  ├── Ping database                                              │
│  ├── COUNT shops, users, events, active sessions                │
│  ├── pg_size_pretty() → database size                           │
│  └── pg_postmaster_start_time() → uptime                        │
│                                                                  │
│  ./admin inspect --shop <uuid>                                   │
│  ├── SELECT shop details (name, owner, currency, tax_rate)      │
│  ├── COUNT products, workers, events, sales                     │
│  ├── SUM revenue from sale events                               │
│  ├── Check active session                                       │
│  ├── Verify ledger integrity                                    │
│  └── Full shop health report                                    │
│                                                                  │
│  ./admin query "SELECT * FROM events WHERE ..."                  │
│  ├── READ-ONLY guard (blocks DELETE/DROP/INSERT/UPDATE)         │
│  ├── Execute raw SQL                                            │
│  └── Return formatted results                                   │
│                                                                  │
│  ./admin ledger --shop <uuid>                                    │
│  ├── Walk all events in order                                   │
│  ├── Verify each event_hash = SHA256(data + previous_hash)      │
│  ├── Verify chain: event[i].previous_hash == event[i-1].hash    │
│  └── Report any broken links                                    │
│                                                                  │
│  ./admin ghost --shop <uuid>                                     │
│  ├── Run variance pattern detection                             │
│  ├── Run price manipulation detection                           │
│  └── Report anomalies with severity levels                      │
│                                                                  │
│  ./admin emergency force-close-session <worker_id> --shop <uuid> │
│  ├── UPDATE sessions SET status='closed' WHERE ...              │
│  └── Instant — no restart needed                                │
│                                                                  │
│  ./admin emergency disable-user <user_id>                        │
│  ├── UPDATE shop_users SET active=false WHERE id = ?            │
│  └── User locked out immediately                                │
│                                                                  │
│  ./admin metrics                                                 │
│  ├── Database size, active connections                          │
│  ├── Cache hit ratio, dead tuples                               │
│  └── Largest table                                              │
└─────────────────────────────────────────────────────────────────┘
```

---

## Key Capabilities

### Core POS
- **Real-time inventory** with oversell prevention
- **Sales recording** with worker attribution and payment tracking (cash/M-Pesa)
- **Worker reconciliation** with automatic variance detection
- **Session management** for shift tracking
- **PDF receipt generation**

### Financial Integrity
- **Hash-chained event ledger** — every transaction is immutable and cryptographically linked
- **Tamper detection** — any alteration breaks the chain and is immediately flagged
- **Complete audit trail** with per-shop sequence numbers
- **Crash recovery** — zero data loss via event replay

### Ghost Mode (Anomaly Detection)
- **Variance patterns** — workers consistently short across reconciliations
- **Consecutive short streaks** — multiple shifts in a row with missing money
- **Price manipulation** — same product sold at different prices by same worker
- **Off-hours activity** — sales recorded outside business hours
- **Stock drift** — inventory disappearing faster than sales explain
- **Risk scoring** — 0-100 aggregate risk score per shop

### Remote Administration
- **System health** — database status, size, uptime, connection count
- **Shop inspection** — full state overview for any shop
- **Raw SQL queries** — read-only access for deep debugging
- **Emergency operations** — force-close sessions, reset PINs, fix stock, disable users
- **Performance metrics** — cache hit ratio, dead tuples, largest tables

### Multi-Tenancy
- **Row-level isolation** — every query scoped to shop_id
- **Role-based access** — owner, manager, cashier with different permissions
- **JWT authentication** — stateless, secure, with refresh tokens

---

## Quick Start

### Prerequisites
- Go 1.25.6 or later
- PostgreSQL 16 (or Docker)

### Option 1: Docker (Recommended)

```bash
# Start PostgreSQL + App
docker compose up -d

# Check status
curl http://localhost:8080/health
```

### Option 2: Local Development

```bash
# 1. Start PostgreSQL
docker compose up -d postgres

# 2. Run migrations
# (migrations auto-run on first postgres start via docker-entrypoint-initdb.d)

# 3. Set environment variables
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/minidb?sslmode=disable"
export JWT_SECRET="your-secret-key"
export HTTP_PORT=8080

# 4. Run server
go run ./cmd/server

# 5. In another terminal, create a user via psql for testing:
psql "$DATABASE_URL" -c "
  INSERT INTO shops (id, name, owner_email, password_hash)
  VALUES (gen_random_uuid(), 'Test Shop', 'admin@test.com', '\$2a\$10\$...');
"
```

### Option 3: CLI Only (Legacy, Single-Machine)

```bash
# Build
go build -o pos ./cmd/pos

# Add stock
./pos inventory add "PROD-001" 100 1500

# Record a sale
./pos sale --product "PROD-001" --qty 5 --price 2500 --worker "W001" --payment cash

# Run Ghost Mode
./pos ghost run

# Check ledger
./pos ledger show
```

---

## API Reference

### Authentication

```bash
# Login
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@test.com","password":"secret"}'

# Response
{
  "access_token": "eyJhbGci...",
  "refresh_token": "eyJhbGci...",
  "user_id": "...",
  "shop_id": "...",
  "email": "admin@test.com",
  "role": "owner"
}
```

### Sales

```bash
# Record a sale
curl -X POST http://localhost:8080/api/sales \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "product_id": "prod-abc",
    "quantity": 2,
    "price": 100,
    "worker_id": "worker-001",
    "payment": 1
  }'

# List recent sales
curl http://localhost:8080/api/sales \
  -H "Authorization: Bearer <token>"
```

### Ghost Mode

```bash
# Run anomaly detection
curl http://localhost:8080/api/ghost \
  -H "Authorization: Bearer <token>"

# Response
{
  "shop_id": "...",
  "anomalies": [...],
  "risk_score": 30,
  "generated_at": "2026-03-31T10:30:00Z"
}
```

### Admin CLI

```bash
# System health
./admin status

# Inspect a shop
./admin inspect --shop <uuid>

# Verify ledger integrity
./admin ledger --shop <uuid>

# Run Ghost Mode
./admin ghost --shop <uuid>

# Emergency: disable a user
./admin emergency disable-user <user_id>

# Emergency: force close a session
./admin emergency force-close-session <worker_id> --shop <uuid>

# Database metrics
./admin metrics

# Read-only SQL query
./admin query "SELECT COUNT(*) FROM events WHERE event_type = 'sale'"
```

---

## Project Structure

```
mini-database/
├── cmd/
│   ├── server/          # HTTP API server (main entry point)
│   │   └── main.go      # Graceful shutdown, logging, config
│   ├── admin/           # Remote administration CLI
│   │   └── main.go      # Status, inspect, query, ghost, emergency
│   ├── pos/             # Legacy CLI (single-machine, file-based)
│   │   └── main.go
│   ├── minidb/          # Legacy HTTP server (deprecated)
│   └── internal/cli/    # POS CLI commands
│       ├── root.go
│       ├── inventory.go
│       ├── sale.go
│       ├── reconcile.go
│       ├── session.go
│       ├── simulate.go
│       ├── report.go
│       ├── ledger.go
│       ├── ghost.go
│       └── session_state.go
│
├── internal/
│   ├── api/             # HTTP API layer
│   │   ├── server.go    # chi router, middleware stack, route registration
│   │   ├── middleware.go # Auth (JWT), CORS, logging, role-based access
│   │   └── handlers.go  # 25 API handlers (auth, CRUD, reports, ghost)
│   ├── auth/            # Authentication
│   │   ├── jwt.go       # JWT generation/verification, bcrypt passwords
│   │   └── jwt_test.go  # 8 unit tests
│   ├── config/          # Configuration
│   │   ├── config.go    # Environment-based config with validation
│   │   └── config_test.go # 4 unit tests
│   └── db/              # Database layer
│       ├── db.go        # PostgreSQL connection pool, transactional helper
│       └── db_test.go   # Connection tests
│
├── engine/              # Core business logic
│   ├── engine.go        # Event engine, hash chaining, persistence
│   ├── inventory.go     # Stock tracking & oversell prevention
│   ├── sales.go         # Sales processing
│   ├── reconciliation.go # Worker reconciliation
│   ├── ghost_mode.go    # Ghost Mode anomaly detection (5 rules)
│   ├── ghost_mode_test.go # 5 Ghost Mode tests
│   └── *_test.go        # 12 engine tests total
│
├── core/                # Domain models
│   ├── errors.go        # Typed error system
│   ├── stock.go         # Stock domain model
│   ├── sale.go          # Sale domain model
│   ├── reconciliation.go # Reconciliation domain model
│   ├── db/              # Custom KV store (legacy)
│   ├── record/          # Binary record encoding with CRC32
│   └── snapshot/        # MVCC snapshot management
│
├── storage/             # Low-level storage components
│   ├── event.go         # Binary event structure
│   └── log.go           # Append-only event log
│
├── ledger/              # Hash chain utilities
│   └── hash_chain.go    # SHA256 hash computation
│
├── projection/          # Read model projections
│   ├── manager.go
│   ├── inventory_projection.go
│   └── sales_projection.go
│
├── migrations/          # Database migrations
│   ├── 001_initial.up.sql   # Full schema (9 tables)
│   └── 001_initial.down.sql # Clean rollback
│
├── docker-compose.yml   # Local dev environment
├── Dockerfile           # Multi-stage build
├── Makefile             # Build, test, run commands
├── .env.example         # Environment template
└── go.mod               # Go module definition
```

---

## Database Schema

```
shops ─────────────┐
  ├── shop_users   │ (multi-tenant isolation)
  ├── products     │
  ├── workers      │
  ├── sessions     │
  ├── events       │ ← THE CORE: hash-chained, append-only
  ├── mpesa_transactions
  ├── audit_log
  └── system_metrics
```

Every business operation creates an immutable event in the `events` table:
- Each event has a per-shop sequence number (`event_seq`)
- Each event's hash includes the previous event's hash
- Tampering with any event breaks the chain
- Chain verification is O(n) and runs via `./admin ledger --shop <uuid>`

---

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run with race detection
go test -race ./...

# Run specific package
go test -v ./engine
go test -v ./internal/auth
go test -v ./internal/config
```

**Test Coverage:**
- 44 tests across engine, auth, config, db, and core/db
- All tests passing ✅
- Includes crash recovery validation, token expiration, password hashing, and Ghost Mode detection

---

## Deployment

### Single VPS (Recommended for Phase 1)

```
Hetzner CPX21 (~€7/month)
├── Caddy (reverse proxy, auto TLS)
├── Go binary (:8080)
├── PostgreSQL 16 (Docker container)
├── Daily backup → S3/Wasabi
└── Monitor: UptimeRobot + Sentry
```

### Scale Path

| Shops | Infrastructure |
|-------|---------------|
| 1-50 | Single VPS, handles it easily |
| 50-500 | Move Postgres to managed (Supabase/Neon) |
| 500-5000 | Add read replicas, separate webhook worker |
| 5000+ | Multi-region, event streaming |

---

## Roadmap

### Phase 1: Core POS (Current) ✅
- [x] Multi-tenant PostgreSQL schema
- [x] JWT authentication
- [x] HTTP API with 25 endpoints
- [x] Ghost Mode anomaly detection
- [x] Admin CLI for remote diagnostics
- [x] Hash-chained event ledger
- [x] Docker Compose for local dev
- [ ] Web dashboard (HTMX + Tailwind)
- [ ] M-Pesa Daraja API integration
- [ ] PDF receipt generation
- [ ] Daily email reports

### Phase 2: Growth
- [ ] Thermal printer support (ESC/POS)
- [ ] SMS receipts to customers
- [ ] WhatsApp business integration
- [ ] Automated daily reconciliation reports
- [ ] Batch inventory import (CSV)

### Phase 3: Platform
- [ ] REST API for third-party integrations
- [ ] Supplier ordering network
- [ ] Working capital loans based on verified sales data
- [ ] White-label for banks

---

## License

Internal retail tool — All rights reserved.

---

**Last Updated:** March 31, 2026  
**Version:** 2.0 (Multi-tenant Platform)  
**Status:** API ready ✅ | Tests passing ✅ | Admin CLI operational ✅ | Ghost Mode active ✅
