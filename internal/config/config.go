package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config はアプリケーション全体の設定を保持する。
type Config struct {
	ListenAddr     string
	DBPath         string
	CIPoolCIDR     string
	RouterHost     string
	RouterSSHPort  int
	RouterUser     string
	RouterSSHKey   string
	RouterHostKey  string
	RouterWgIF     string
	WGServerPubkey string
	WGEndpoint     string
	WGMTU          int
	WGKeepalive    int
	DefaultTTL     int
}

// Load は環境変数から設定を読み込む。
func Load() (*Config, error) {
	c := &Config{
		ListenAddr:     envOrDefault("LISTEN_ADDR", "127.0.0.1:8088"),
		DBPath:         envOrDefault("DB_PATH", "/var/lib/peer-issuer/leases.db"),
		CIPoolCIDR:     envOrDefault("CI_POOL_CIDR", "10.66.66.64/26"),
		RouterHost:     envOrDefault("ROUTER_HOST", "192.168.1.1"),
		RouterUser:     envOrDefault("ROUTER_USER", "admin"),
		RouterSSHKey:   os.Getenv("ROUTER_SSH_KEY"),
		RouterHostKey:  os.Getenv("ROUTER_HOST_KEY"),
		RouterWgIF:     envOrDefault("ROUTER_WG_IF", "wg-home"),
		WGServerPubkey: os.Getenv("WG_SERVER_PUBKEY"),
		WGEndpoint:     os.Getenv("WG_ENDPOINT"),
	}

	var err error

	c.RouterSSHPort, err = envOrDefaultInt("ROUTER_SSH_PORT", 22)
	if err != nil {
		return nil, fmt.Errorf("ROUTER_SSH_PORT: %w", err)
	}

	c.WGMTU, err = envOrDefaultInt("WG_MTU", 1420)
	if err != nil {
		return nil, fmt.Errorf("WG_MTU: %w", err)
	}

	c.WGKeepalive, err = envOrDefaultInt("WG_KEEPALIVE", 25)
	if err != nil {
		return nil, fmt.Errorf("WG_KEEPALIVE: %w", err)
	}

	c.DefaultTTL, err = envOrDefaultInt("DEFAULT_TTL", 86400)
	if err != nil {
		return nil, fmt.Errorf("DEFAULT_TTL: %w", err)
	}

	if c.RouterSSHKey == "" {
		return nil, fmt.Errorf("ROUTER_SSH_KEY is required")
	}
	if c.WGServerPubkey == "" {
		return nil, fmt.Errorf("WG_SERVER_PUBKEY is required")
	}
	if c.WGEndpoint == "" {
		return nil, fmt.Errorf("WG_ENDPOINT is required")
	}

	return c, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q: %w", v, err)
	}
	return n, nil
}
