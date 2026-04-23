package routeros

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Client はRouterOSへのSSH接続を管理する。
type Client struct {
	addr       string
	config     *ssh.ClientConfig
	wgIface    string
}

// NewClient はRouterOS SSHクライアントを作成する。
func NewClient(host string, port int, user, keyPath, hostKey, wgIface string) (*Client, error) {
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read SSH key %q: %w", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse SSH key: %w", err)
	}

	var hostKeyCallback ssh.HostKeyCallback
	if hostKey != "" {
		pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(hostKey))
		if err != nil {
			return nil, fmt.Errorf("parse host key: %w", err)
		}
		hostKeyCallback = ssh.FixedHostKey(pubKey)
	} else {
		log.Println("WARNING: ROUTER_HOST_KEY not set, using InsecureIgnoreHostKey")
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
	}

	return &Client{
		addr:    net.JoinHostPort(host, fmt.Sprintf("%d", port)),
		config:  config,
		wgIface: wgIface,
	}, nil
}

// exec はRouterOSでコマンドを実行し、出力を返す。
func (c *Client) exec(cmd string) (string, error) {
	conn, err := ssh.Dial("tcp", c.addr, c.config)
	if err != nil {
		return "", fmt.Errorf("SSH dial %s: %w", c.addr, err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return string(out), fmt.Errorf("exec %q: %w (output: %s)", cmd, err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// AddPeer はRouterOSにWireGuard peerを追加する。
func (c *Client) AddPeer(leaseID, pubkey, ip string) error {
	cmd := fmt.Sprintf(
		`/interface/wireguard/peers/add interface=%s public-key="%s" allowed-address=%s/32 comment="lease:%s"`,
		c.wgIface, pubkey, ip, leaseID,
	)
	_, err := c.exec(cmd)
	return err
}

// RemovePeer はRouterOSからWireGuard peerを削除する。
func (c *Client) RemovePeer(leaseID string) error {
	cmd := fmt.Sprintf(
		`/interface/wireguard/peers/remove [find where comment="lease:%s"]`,
		leaseID,
	)
	_, err := c.exec(cmd)
	return err
}

// ListPeers は wg-home 上の lease:* comment を持つ peer の lease ID 一覧を返す。
// DB と RouterOS 側の不整合 (orphan peer) を検出するための reconciliation で使用する。
func (c *Client) ListPeers() ([]string, error) {
	cmd := fmt.Sprintf(
		`:foreach p in=[/interface/wireguard/peers/find where interface="%s" comment~"^lease:"] do={:put [/interface/wireguard/peers/get $p comment]}`,
		c.wgIface,
	)
	out, err := c.exec(cmd)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "lease:") {
			ids = append(ids, strings.TrimPrefix(line, "lease:"))
		}
	}
	return ids, nil
}

// Ping はRouterOSへのSSH疎通を確認する。
func (c *Client) Ping() error {
	_, err := c.exec(":put ok")
	return err
}
