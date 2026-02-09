package lease

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/shinbunbun/peer-issuer/internal/ippool"
	"github.com/shinbunbun/peer-issuer/internal/routeros"
)

// Lease はリース情報を表す。
type Lease struct {
	ID        string `json:"lease_id"`
	ClientIP  string `json:"client_ip"`
	ExpiresAt int64  `json:"expires_at"`
}

// CreateResult はリース作成の結果を表す。
type CreateResult struct {
	LeaseID            string `json:"lease_id"`
	ClientIP           string `json:"client_ip"`
	ServerPubkey       string `json:"server_pubkey"`
	Endpoint           string `json:"endpoint"`
	MTU                int    `json:"mtu"`
	PersistentKeepalive int   `json:"persistent_keepalive"`
}

// Service はリースのビジネスロジックを提供する。
type Service struct {
	db             *sql.DB
	pool           *ippool.Pool
	router         *routeros.Client
	serverPubkey   string
	endpoint       string
	mtu            int
	keepalive      int
	defaultTTL     int
}

// NewService はリースサービスを作成する。
func NewService(db *sql.DB, pool *ippool.Pool, router *routeros.Client, serverPubkey, endpoint string, mtu, keepalive, defaultTTL int) *Service {
	return &Service{
		db:           db,
		pool:         pool,
		router:       router,
		serverPubkey: serverPubkey,
		endpoint:     endpoint,
		mtu:          mtu,
		keepalive:    keepalive,
		defaultTTL:   defaultTTL,
	}
}

// Create は新しいリースを作成する。
func (s *Service) Create(clientPubkey string, ttlSeconds int, meta map[string]string) (*CreateResult, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = s.defaultTTL
	}
	if ttlSeconds < 60 {
		ttlSeconds = 60
	}
	if ttlSeconds > 86400 {
		ttlSeconds = 86400
	}

	leaseID := uuid.New().String()
	expiresAt := time.Now().Unix() + int64(ttlSeconds)

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		metaJSON = []byte("{}")
	}

	// BEGIN IMMEDIATE で排他ロック
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// IMMEDIATE ロック取得
	if _, err := tx.Exec("SELECT 1"); err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}

	// 期限切れ lease を先に掃除
	if _, err := tx.Exec("DELETE FROM leases WHERE expires_at <= unixepoch()"); err != nil {
		return nil, fmt.Errorf("clean expired: %w", err)
	}

	// 空きIP割り当て
	ip, err := s.pool.Allocate(tx)
	if err != nil {
		return nil, fmt.Errorf("allocate IP: %w", err)
	}
	clientIP := ip.String()

	// DB に INSERT
	_, err = tx.Exec(
		"INSERT INTO leases (id, client_ip, client_pubkey, expires_at, meta_json) VALUES (?, ?, ?, ?, ?)",
		leaseID, clientIP, clientPubkey, expiresAt, string(metaJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("insert lease: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// RouterOS に peer 追加
	if err := s.router.AddPeer(leaseID, clientPubkey, clientIP); err != nil {
		// 補償: DB から削除
		log.Printf("RouterOS AddPeer failed, compensating: %v", err)
		if _, delErr := s.db.Exec("DELETE FROM leases WHERE id = ?", leaseID); delErr != nil {
			log.Printf("compensation delete failed: %v", delErr)
		}
		return nil, fmt.Errorf("add peer to RouterOS: %w", err)
	}

	return &CreateResult{
		LeaseID:            leaseID,
		ClientIP:           clientIP,
		ServerPubkey:       s.serverPubkey,
		Endpoint:           s.endpoint,
		MTU:                s.mtu,
		PersistentKeepalive: s.keepalive,
	}, nil
}

// Release はリースを解放する。
func (s *Service) Release(leaseID string) error {
	var exists bool
	err := s.db.QueryRow("SELECT 1 FROM leases WHERE id = ?", leaseID).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("lease %q not found", leaseID)
	}
	if err != nil {
		return fmt.Errorf("query lease: %w", err)
	}

	// RouterOS から peer 削除
	if err := s.router.RemovePeer(leaseID); err != nil {
		log.Printf("RouterOS RemovePeer failed (will still delete from DB): %v", err)
	}

	// DB から削除
	if _, err := s.db.Exec("DELETE FROM leases WHERE id = ?", leaseID); err != nil {
		return fmt.Errorf("delete lease: %w", err)
	}

	return nil
}
