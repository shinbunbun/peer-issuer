package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/shinbunbun/peer-issuer/internal/lease"
	"github.com/shinbunbun/peer-issuer/internal/routeros"
)

// Handler はHTTPリクエストを処理する。
type Handler struct {
	svc    *lease.Service
	router *routeros.Client
}

// NewRouter はHTTPルーターを作成する。
func NewRouter(svc *lease.Service, router *routeros.Client) http.Handler {
	h := &Handler{svc: svc, router: router}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("POST /lease", h.createLease)
	mux.HandleFunc("POST /release", h.releaseLease)
	return mux
}

type leaseRequest struct {
	ClientPubkey string `json:"client_pubkey"`
	TTLSeconds   int    `json:"ttl_seconds"`
}

type releaseRequest struct {
	LeaseID string `json:"lease_id"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	if err := h.router.Ping(); err != nil {
		log.Printf("healthz: RouterOS ping failed: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "RouterOS unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) createLease(w http.ResponseWriter, r *http.Request) {
	var req leaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON"})
		return
	}

	if !isValidWGPubkey(req.ClientPubkey) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid client_pubkey: must be 44-char base64 WireGuard key"})
		return
	}

	// Authentik ヘッダーからメタ情報を収集（将来用）
	meta := make(map[string]string)
	if v := r.Header.Get("X-Authentik-Username"); v != "" {
		meta["authentik_username"] = v
	}

	result, err := h.svc.Create(req.ClientPubkey, req.TTLSeconds, meta)
	if err != nil {
		log.Printf("create lease: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) releaseLease(w http.ResponseWriter, r *http.Request) {
	var req releaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON"})
		return
	}

	if req.LeaseID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "lease_id is required"})
		return
	}

	if err := h.svc.Release(req.LeaseID); err != nil {
		log.Printf("release lease: %v", err)
		writeJSON(w, http.StatusNotFound, errorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// isValidWGPubkey はWireGuard公開鍵のフォーマットを検証する。
// base64エンコードされた32バイト = 44文字（末尾 =）
func isValidWGPubkey(key string) bool {
	if len(key) != 44 {
		return false
	}
	if key[43] != '=' {
		return false
	}
	return true
}
