package db_test

import (
	"context"
	"sync"
	"testing"

	"luma.folk3s.com/server/internal/db"
	"luma.folk3s.com/server/internal/testdb"
)

func TestMigrate_Idempotent(t *testing.T) {
	pool := testdb.Empty(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		v, err := db.Migrate(ctx, pool)
		if err != nil {
			t.Fatalf("migrate %d: %v", i, err)
		}
		if v != db.LatestVersion() {
			t.Fatalf("version = %d, want %d", v, db.LatestVersion())
		}
	}
	for _, table := range []string{"users", "sessions", "invites", "blocks", "conversations", "direct_pairs", "members", "messages", "files", "user_events"} {
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&ok); err != nil || !ok {
			t.Errorf("table %s missing (err=%v)", table, err)
		}
	}
}

func TestMigrate_ConcurrentSafe(t *testing.T) {
	pool := testdb.Empty(t)
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := db.Migrate(context.Background(), pool)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != db.LatestVersion() {
		t.Fatalf("schema_migrations rows = %d, want %d", n, db.LatestVersion())
	}
}
