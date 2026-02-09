package lease

import (
	"context"
	"log"
	"time"
)

// StartCleaner は期限切れリースを定期的に掃除するgoroutineを開始する。
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
				s.cleanExpired()
			}
		}
	}()
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
