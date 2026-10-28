package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
)

func TestMigrate_IsIdempotentAndAddsObservabilityColumns(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := Connect(ctx, url)
	if err != nil {
		t.Skipf("cannot connect to database: %v", err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, pool, logger); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}

	var n int
	err = pool.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'request_logs' AND column_name IN ('cache_hit','attempts','failover_count','first_error_category','usage_estimated','request_id')
	`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("expected 6 observability columns, found %d", n)
	}
}
