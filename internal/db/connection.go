package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB holds the database connection pool
type DB struct {
	pool       *pgxpool.Pool
	url        string
	mu         sync.RWMutex
	importGUCs []string // GUCs to apply to new connections during import
}

// Global database instance for convenience
var defaultDB *DB

// Connect establishes a connection to the database with a full connection pool
func Connect(ctx context.Context, url string) (*DB, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid connection URL: %w", err)
	}

	// Configure pool. MaxConns must stay below PostgreSQL's max_connections
	// (default 50 on many setups, minus reserved_connections). 32 leaves
	// headroom for superuser connections, pg_xpatch background workers,
	// and other clients.
	config.MaxConns = 32
	config.MinConns = 4
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	db := &DB{
		url: url,
	}

	// Register AfterConnect hook to apply import GUCs to new connections.
	// When importGUCs is set (during import), every new connection gets the
	// session-level settings automatically — no more missed connections.
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		db.mu.RLock()
		gucs := db.importGUCs
		db.mu.RUnlock()

		if len(gucs) > 0 {
			for _, guc := range gucs {
				if _, err := conn.Exec(ctx, guc); err != nil {
					return fmt.Errorf("failed to set GUC %q on new connection: %w", guc, err)
				}
			}
		}
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db.pool = pool

	return db, nil
}

// ConnectLite establishes a lightweight connection (single connection, no pool)
// Use this for quick operations like metadata lookups
func ConnectLite(ctx context.Context, url string) (*DB, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid connection URL: %w", err)
	}

	// Minimal pool - just 1 connection for quick operations
	config.MaxConns = 1
	config.MinConns = 0
	config.MaxConnLifetime = time.Minute
	config.MaxConnIdleTime = 10 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{
		pool: pool,
		url:  url,
	}

	return db, nil
}

// SetDefault sets the default database instance
func SetDefault(db *DB) {
	defaultDB = db
}

// Default returns the default database instance
func Default() *DB {
	return defaultDB
}

// Close closes the database connection
func (db *DB) Close() {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.pool != nil {
		db.pool.Close()
		db.pool = nil
	}
}

// Pool returns the underlying connection pool
func (db *DB) Pool() *pgxpool.Pool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.pool
}

// Exec executes a query without returning rows
func (db *DB) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := db.pool.Exec(ctx, sql, args...)
	return err
}

// Query executes a query and returns rows
func (db *DB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return db.pool.Query(ctx, sql, args...)
}

// QueryRow executes a query and returns a single row
func (db *DB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return db.pool.QueryRow(ctx, sql, args...)
}

// Begin starts a transaction
func (db *DB) Begin(ctx context.Context) (pgx.Tx, error) {
	return db.pool.Begin(ctx)
}

// BeginTx starts a transaction with options
func (db *DB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	return db.pool.BeginTx(ctx, opts)
}

// WithTx executes a function within a transaction
func (db *DB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	return tx.Commit(ctx)
}

// SetImportGUCs configures session-level PostgreSQL settings optimized for
// bulk import on ALL connections in the pool. These settings trade crash
// safety for speed — safe because import can resume from the temp file
// if PostgreSQL crashes.
func (db *DB) SetImportGUCs(ctx context.Context) error {
	gucs := []string{
		"SET synchronous_commit = off", // Don't flush WAL on every COMMIT (5-10x faster)
		"SET commit_delay = 100",       // Group commits within 100µs window
	}

	// Apply to all existing and future connections using AcquireFunc
	// We need to touch every connection in the pool
	poolSize := int(db.pool.Stat().MaxConns())
	seen := make(map[uint32]bool)

	for i := 0; i < poolSize*2 && len(seen) < poolSize; i++ {
		err := db.pool.AcquireFunc(ctx, func(conn *pgxpool.Conn) error {
			connID := conn.Conn().PgConn().PID()
			if seen[connID] {
				return nil // Already configured this connection
			}
			seen[connID] = true
			for _, guc := range gucs {
				if _, err := conn.Exec(ctx, guc); err != nil {
					return fmt.Errorf("failed to set GUC %q: %w", guc, err)
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to set import GUCs: %w", err)
		}
	}

	// Also set AfterConnect so any NEW connections get the GUCs
	db.mu.Lock()
	db.importGUCs = gucs
	db.mu.Unlock()

	return nil
}

// ResetImportGUCs restores default PostgreSQL session settings after import.
func (db *DB) ResetImportGUCs(ctx context.Context) error {
	db.mu.Lock()
	db.importGUCs = nil
	db.mu.Unlock()

	gucs := []string{
		"RESET synchronous_commit",
		"RESET commit_delay",
	}
	// Best-effort reset on one connection — pool connections will expire naturally
	for _, guc := range gucs {
		_ = db.Exec(ctx, guc)
	}
	return nil
}

// URL returns the connection URL
func (db *DB) URL() string {
	return db.url
}

// IsConnected returns true if the database is connected
func (db *DB) IsConnected() bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.pool != nil
}

// Ping tests the database connection
func (db *DB) Ping(ctx context.Context) error {
	return db.pool.Ping(ctx)
}
