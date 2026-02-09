package ippool

import (
	"database/sql"
	"net/netip"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA busy_timeout=5000;
		CREATE TABLE IF NOT EXISTS leases (
			id TEXT PRIMARY KEY,
			client_ip TEXT NOT NULL UNIQUE,
			client_pubkey TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (unixepoch()),
			meta_json TEXT DEFAULT '{}'
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestNewPool(t *testing.T) {
	pool, err := NewPool("10.66.66.64/26")
	if err != nil {
		t.Fatal(err)
	}
	// /26 = 64 addresses, first usable = .65, last = .126
	if pool.first != netip.MustParseAddr("10.66.66.65") {
		t.Errorf("first = %v, want 10.66.66.65", pool.first)
	}
	if pool.last != netip.MustParseAddr("10.66.66.126") {
		t.Errorf("last = %v, want 10.66.66.126", pool.last)
	}
}

func TestAllocateFirstIP(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	pool, err := NewPool("10.66.66.64/26")
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	ip, err := pool.Allocate(tx)
	if err != nil {
		t.Fatal(err)
	}

	if ip != netip.MustParseAddr("10.66.66.65") {
		t.Errorf("got %v, want 10.66.66.65", ip)
	}
}

func TestAllocateSkipsUsed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// .65 を使用中として登録
	_, err := db.Exec(
		"INSERT INTO leases (id, client_ip, client_pubkey, expires_at) VALUES (?, ?, ?, unixepoch() + 3600)",
		"test-1", "10.66.66.65", "pubkey1",
	)
	if err != nil {
		t.Fatal(err)
	}

	pool, err := NewPool("10.66.66.64/26")
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	ip, err := pool.Allocate(tx)
	if err != nil {
		t.Fatal(err)
	}

	if ip != netip.MustParseAddr("10.66.66.66") {
		t.Errorf("got %v, want 10.66.66.66", ip)
	}
}

func TestAllocateIgnoresExpired(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// .65 を期限切れとして登録
	_, err := db.Exec(
		"INSERT INTO leases (id, client_ip, client_pubkey, expires_at) VALUES (?, ?, ?, unixepoch() - 1)",
		"test-expired", "10.66.66.65", "pubkey1",
	)
	if err != nil {
		t.Fatal(err)
	}

	pool, err := NewPool("10.66.66.64/26")
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	ip, err := pool.Allocate(tx)
	if err != nil {
		t.Fatal(err)
	}

	// 期限切れの .65 は無視されるので、.65 が返る
	if ip != netip.MustParseAddr("10.66.66.65") {
		t.Errorf("got %v, want 10.66.66.65", ip)
	}
}

func TestAllocatePoolExhausted(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// /30 = 4 addresses, usable = .1 and .2
	pool, err := NewPool("10.0.0.0/30")
	if err != nil {
		t.Fatal(err)
	}

	// .1 と .2 を使用中
	for i, ip := range []string{"10.0.0.1", "10.0.0.2"} {
		_, err := db.Exec(
			"INSERT INTO leases (id, client_ip, client_pubkey, expires_at) VALUES (?, ?, ?, unixepoch() + 3600)",
			"test-"+string(rune('a'+i)), ip, "pubkey",
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	_, err = pool.Allocate(tx)
	if err == nil {
		t.Error("expected error for exhausted pool")
	}
}
