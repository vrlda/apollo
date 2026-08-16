---
description: SQLite and SQL tuning — indexing, EXPLAIN QUERY PLAN, query optimization, WAL mode, common patterns
---

# SQL Tuning Skill

## SQLite Performance Essentials

### WAL Mode (critical for concurrent reads)
```sql
PRAGMA journal_mode=WAL;    -- survives restart
PRAGMA synchronous=NORMAL;  -- safe + faster than FULL
PRAGMA cache_size=-32000;   -- 32MB page cache
PRAGMA temp_store=MEMORY;
```

### EXPLAIN QUERY PLAN
```sql
EXPLAIN QUERY PLAN SELECT * FROM users WHERE email = 'a@b.com';
-- Look for: "SCAN TABLE users" (bad) vs "SEARCH TABLE users USING INDEX" (good)
```

## Indexing Strategy

### When to add an index
- Column appears in WHERE, JOIN ON, ORDER BY, GROUP BY
- Column has high cardinality (many distinct values)
- Table has > 10k rows and query runs frequently

### Common index patterns
```sql
-- Simple index
CREATE INDEX idx_users_email ON users(email);

-- Composite index (order matters! put equality first, then range)
CREATE INDEX idx_orders_user_date ON orders(user_id, created_at DESC);

-- Covering index (includes all columns the query needs)
CREATE INDEX idx_orders_cover ON orders(user_id, status) INCLUDE (total, created_at);

-- Partial index (for boolean or enum filters)
CREATE INDEX idx_active_users ON users(email) WHERE active = 1;
```

### Don't index
- Columns you always scan fully (e.g. boolean with 2 values on a small table)
- Rarely-queried columns on write-heavy tables (indexes slow down INSERT/UPDATE)

## Query Optimization Patterns

### N+1 Problem Fix
```sql
-- Bad: SELECT user for each order
-- Good: JOIN them once
SELECT o.id, o.total, u.name
FROM orders o
JOIN users u ON u.id = o.user_id
WHERE o.status = 'pending';
```

### Pagination (use keyset, not OFFSET for large tables)
```sql
-- Bad (slow on large tables): LIMIT 20 OFFSET 10000
-- Good (keyset pagination):
SELECT * FROM orders
WHERE created_at < ? AND id < ?
ORDER BY created_at DESC, id DESC
LIMIT 20;
```

### Batch Inserts
```sql
-- Instead of looping INSERT:
INSERT INTO events (type, payload) VALUES
  ('click', '{}'),
  ('view', '{}'),
  ('submit', '{}');
-- Or use: BEGIN TRANSACTION + multiple inserts + COMMIT
```

### UPSERT
```sql
INSERT INTO settings (key, value)
VALUES ('theme', 'dark')
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
```

## SQLite in Go (database/sql)
```go
// Always use prepared statements for repeated queries
stmt, err := db.Prepare("INSERT INTO logs (msg) VALUES (?)")
defer stmt.Close()
stmt.Exec("hello")

// Transactions for batch operations
tx, _ := db.Begin()
for _, item := range items {
    tx.Exec("INSERT INTO ...", item)
}
tx.Commit()

// Row scanning
row := db.QueryRow("SELECT id, name FROM users WHERE id = ?", id)
var u User
row.Scan(&u.ID, &u.Name)
```

## Table Design Checklist
- Every table has an integer PRIMARY KEY (rowid alias in SQLite)
- Timestamps as `INTEGER` (Unix epoch) — faster comparisons than TEXT
- Use `NOT NULL DEFAULT` where possible — avoids NULL handling complexity
- Foreign keys: `PRAGMA foreign_keys = ON;` (off by default in SQLite!)
