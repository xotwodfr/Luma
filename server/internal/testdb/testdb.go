// Package testdb gives each test its own throwaway PostgreSQL database on the
// disposable luma-test-pg container (see server/Makefile: testdb-up).
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"luma.folk3s.com/server/internal/db"
)

const defaultURL = "postgres://postgres:test@127.0.0.1:55432/postgres?sslmode=disable"

func adminURL() string {
	if u := os.Getenv("LUMA_TEST_DATABASE_URL"); u != "" {
		return u
	}
	return defaultURL
}

// Empty creates a fresh, unmigrated database and drops it when the test ends.
func Empty(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminURL())
	if err != nil {
		t.Fatalf("testdb: connect admin: %v", err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "luma_t_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatalf("testdb: create database (is luma-test-pg running? make testdb-up): %v", err)
	}
	url := strings.Replace(adminURL(), "/postgres?", "/"+name+"?", 1)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close()
	})
	return pool
}

// URL returns the connection string of the database behind pool, for tests that start a second server.
func URL(pool *pgxpool.Pool) string {
	return pool.Config().ConnString()
}

// New creates a fresh, fully migrated database.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool := Empty(t)
	if _, err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	return pool
}
