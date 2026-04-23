package lease

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// StartCleaner は期限切れリースを定期的に掃除し、
// RouterOS 側と DB の整合性を取る goroutine を開始する。
func (s *Service) StartCleaner(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("cleaner: shutting down")
				return
			case <-ticker.C:
				// DB に無いが RouterOS に残る orphan peer を先に掃除。
				// (DB 消失 / /release 失敗で生じた残存 peer を回収)
				s.reconcileOrphans()
				// DB にある期限切れ lease を掃除。
				s.cleanExpired()
			}
		}
	}()
}

// reconcileOrphans は RouterOS 側にあるが DB に無い peer を削除する。
// peer-issuer の lease DB が emptyDir / pod 再起動で消失したケースや、
// /release が silent fail したケースで生じる orphan peer を回収する。
func (s *Service) reconcileOrphans() {
	routerIDs, err := s.router.ListPeers()
	if err != nil {
		log.Printf("reconcile: list router peers: %v", err)
		return
	}
	for _, id := range routerIDs {
		var dummy int
		err := s.db.QueryRow("SELECT 1 FROM leases WHERE id = ?", id).Scan(&dummy)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			log.Printf("reconcile: query lease %s: %v", id, err)
			continue
		}
		log.Printf("reconcile: orphan peer lease:%s not in DB, removing from RouterOS", id)
		if rerr := s.router.RemovePeer(id); rerr != nil {
			log.Printf("reconcile: remove orphan %s: %v", id, rerr)
		}
	}
}

func (s *Service) cleanExpired() {
	rows, err := s.db.Query("SELECT id FROM leases WHERE expires_at <= unixepoch()")
	if err != nil {
		log.Printf("cleaner: query expired: %v", err)
		return
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			log.Printf("cleaner: scan: %v", err)
			continue
		}
		ids = append(ids, id)
	}

	for _, id := range ids {
		if err := s.router.RemovePeer(id); err != nil {
			log.Printf("cleaner: remove peer %s from RouterOS: %v (will retry)", id, err)
			continue
		}
		if _, err := s.db.Exec("DELETE FROM leases WHERE id = ?", id); err != nil {
			log.Printf("cleaner: delete lease %s from DB: %v", id, err)
		} else {
			log.Printf("cleaner: cleaned expired lease %s", id)
		}
	}
}
