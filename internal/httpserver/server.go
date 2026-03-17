package httpserver

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/app"
)

type Server struct {
	syncService        *app.SyncService
	integrationService *app.IntegrationService
}

func New(syncService *app.SyncService, integrationService *app.IntegrationService) *Server {
	return &Server{syncService: syncService, integrationService: integrationService}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/metrics", s.metrics)
	mux.HandleFunc("/webhooks/github", s.githubWebhook)
	mux.HandleFunc("/oauth/github/callback", s.githubOAuthCallback)
	mux.HandleFunc("/oauth/google/callback", s.googleOAuthCallback)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	m := s.syncService.MetricsSnapshot()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w,
		"takhtit_sync_total %d\n"+
			"takhtit_sync_success_total %d\n"+
			"takhtit_sync_failed_total %d\n"+
			"takhtit_sync_last_status %q\n"+
			"takhtit_sync_last_message %q\n"+
			"takhtit_sync_last_started_at %q\n"+
			"takhtit_sync_last_ended_at %q\n",
		m.TotalRuns,
		m.SuccessfulRuns,
		m.FailedRuns,
		m.LastStatus,
		m.LastMessage,
		m.LastStartedAt.UTC().Format(time.RFC3339),
		m.LastEndedAt.UTC().Format(time.RFC3339),
	)
}

func (s *Server) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if !verifyGitHubSignature(payload, r.Header.Get("X-Hub-Signature-256"), os.Getenv("GITHUB_WEBHOOK_SECRET")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	event := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if event == "" {
		http.Error(w, "missing event", http.StatusBadRequest)
		return
	}

	userID := int64(0)
	if raw := r.URL.Query().Get("user_id"); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr == nil {
			userID = parsed
		}
	}

	if userID == 0 {
		var envelope struct {
			Sender struct {
				ID int64 `json:"id"`
			} `json:"sender"`
		}
		_ = json.Unmarshal(payload, &envelope)
		userID = envelope.Sender.ID
	}

	if userID == 0 {
		http.Error(w, "missing user id for sync target", http.StatusBadRequest)
		return
	}

	status := s.syncService.TriggerForUser(context.Background(), userID)
	log.Printf("github webhook event=%s user=%d status=%s", event, userID, status)

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("accepted"))
}

func (s *Server) githubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	userID, err := s.integrationService.GitHubOAuthCallback(r.Context(), code, state)
	if err != nil {
		http.Error(w, "oauth callback failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	msg := fmt.Sprintf("GitHub connection successful for telegram user %d. You can return to Telegram and run /sync.", userID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(msg))
}

func (s *Server) googleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	userID, err := s.integrationService.GoogleOAuthCallback(r.Context(), code, state)
	if err != nil {
		http.Error(w, "oauth callback failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	msg := fmt.Sprintf("Google connection successful for telegram user %d. You can return to Telegram and run /sync.", userID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(msg))
}

func verifyGitHubSignature(payload []byte, signatureHeader, secret string) bool {
	if secret == "" {
		return false
	}
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}
	signature := strings.TrimPrefix(signatureHeader, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}
