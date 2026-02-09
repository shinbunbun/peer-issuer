package ippool

import (
	"database/sql"
	"fmt"
	"net/netip"
)

// Pool はCIDRレンジからIPアドレスを割り当てる。
type Pool struct {
	prefix netip.Prefix
	first  netip.Addr
	last   netip.Addr
}

// NewPool はCIDR文字列からIPプールを作成する。
func NewPool(cidr string) (*Pool, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse CIDR %q: %w", cidr, err)
	}

	if !prefix.Addr().Is4() {
		return nil, fmt.Errorf("only IPv4 is supported")
	}

	first, last := rangeFromPrefix(prefix)
	return &Pool{prefix: prefix, first: first, last: last}, nil
}

// Allocate はトランザクション内で空きIPを割り当てる。
// tx は BEGIN IMMEDIATE で開始されていることを期待する。
func (p *Pool) Allocate(tx *sql.Tx) (netip.Addr, error) {
	used := make(map[netip.Addr]bool)

	rows, err := tx.Query("SELECT client_ip FROM leases WHERE expires_at > unixepoch()")
	if err != nil {
		return netip.Addr{}, fmt.Errorf("query used IPs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ipStr string
		if err := rows.Scan(&ipStr); err != nil {
			return netip.Addr{}, fmt.Errorf("scan IP: %w", err)
		}
		addr, err := netip.ParseAddr(ipStr)
		if err != nil {
			continue
		}
		used[addr] = true
	}
	if err := rows.Err(); err != nil {
		return netip.Addr{}, fmt.Errorf("iterate IPs: %w", err)
	}

	// 最小の空きIPを返す
	for addr := p.first; addr.Compare(p.last) <= 0; addr = addr.Next() {
		if !used[addr] {
			return addr, nil
		}
	}

	return netip.Addr{}, fmt.Errorf("no available IPs in pool %s", p.prefix)
}

// rangeFromPrefix はCIDRプレフィックスから割り当て可能な最初と最後のアドレスを返す。
// ネットワークアドレスとブロードキャストアドレスは除外する。
func rangeFromPrefix(prefix netip.Prefix) (netip.Addr, netip.Addr) {
	addr := prefix.Addr()
	bits := prefix.Bits()

	// ホストビット数からアドレス数を計算
	hostBits := 32 - bits
	numAddrs := uint32(1) << hostBits

	// ネットワークアドレスのバイト表現
	a4 := addr.As4()
	base := uint32(a4[0])<<24 | uint32(a4[1])<<16 | uint32(a4[2])<<8 | uint32(a4[3])

	// /31 と /32 はネットワーク/ブロードキャスト除外なし
	if hostBits <= 1 {
		lastIP := base + numAddrs - 1
		return addr, netip.AddrFrom4([4]byte{
			byte(lastIP >> 24), byte(lastIP >> 16), byte(lastIP >> 8), byte(lastIP),
		})
	}

	firstIP := base + 1
	lastIP := base + numAddrs - 2

	first := netip.AddrFrom4([4]byte{
		byte(firstIP >> 24), byte(firstIP >> 16), byte(firstIP >> 8), byte(firstIP),
	})
	last := netip.AddrFrom4([4]byte{
		byte(lastIP >> 24), byte(lastIP >> 16), byte(lastIP >> 8), byte(lastIP),
	})

	return first, last
}
