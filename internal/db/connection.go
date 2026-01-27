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
	pool *pgxpool.Pool
	url  string
	mu   sync.RWMutex
}

// Global database instance for convenience
var defaultDB *DB

// Connect establishes a connection to the database
func Connect(ctx context.Context, url string) (*DB, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid connection URL: %w", err)
	}

	// Configure pool
	config.MaxConns = 10
	config.MinConns = 1
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

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
