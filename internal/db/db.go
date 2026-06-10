package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS leases (
    id TEXT PRIMARY KEY,
    client_ip TEXT NOT NULL UNIQUE,
    client_pubkey TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    meta_json TEXT DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_leases_expires_at ON leases(expires_at);
`

// Open はSQLiteデータベースを開き、スキーマを適用する。
func Open(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	// _txlock=immediate で db.Begin() を BEGIN IMMEDIATE にし、
	// トランザクション開始時に write ロックを取得する。これにより
	// read→write のロック昇格（busy_timeout が無視され即 SQLITE_BUSY になる
	// デッドロック回避パス）を避け、書き込み競合は busy_timeout で待てるようにする。
	dsn := fmt.Sprintf("file:%s?_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// 単一コネクションに直列化する。lease 作成ハンドラと cleaner goroutine が
	// 別コネクションで同時に書き込むと SQLITE_BUSY が発生しうるため、
	// SQLite への全アクセスを 1 コネクションに集約して競合自体を無くす。
	// (CI peer 発行は低トラフィックなのでスループット低下は問題にならない)
	db.SetMaxOpenConns(1)

	// SQLite のパフォーマンスと安全性の設定
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %q: %w", p, err)
		}
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return db, nil
}
