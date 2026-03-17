package integrations

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ExternalDocument struct {
	SourceID string
	Title    string
	Content  string
	URL      string
	Tags     []string
	Metadata map[string]any
}

type Connector interface {
	Name() string
	Collect(ctx context.Context, telegramUserID int64) ([]ExternalDocument, error)
}

type Service struct {
	connectors         []Connector
	db                 *sql.DB
	tokenEncryptionKey []byte
	oauthStateSecret   string
	githubClientID     string
	githubClientSecret string
	githubRedirectURI  string
	googleClientID     string
	googleClientSecret string
	googleRedirectURI  string
}

func NewService(database *sql.DB) *Service {
	s := &Service{
		db:                 database,
		tokenEncryptionKey: deriveTokenKey(strings.TrimSpace(os.Getenv("TOKEN_ENCRYPTION_KEY"))),
		oauthStateSecret:   strings.TrimSpace(os.Getenv("OAUTH_STATE_SECRET")),
		githubClientID:     strings.TrimSpace(os.Getenv("GITHUB_CLIENT_ID")),
		githubClientSecret: strings.TrimSpace(os.Getenv("GITHUB_CLIENT_SECRET")),
		githubRedirectURI:  strings.TrimSpace(os.Getenv("GITHUB_REDIRECT_URI")),
		googleClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		googleClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")),
		googleRedirectURI:  strings.TrimSpace(os.Getenv("GOOGLE_REDIRECT_URI")),
	}
	s.connectors = []Connector{
		NewGitHubConnectorFromEnv(s.getGitHubTokenForUser),
		NewGoogleDocsConnectorFromEnv(s.getGoogleTokenForUser),
		NewGoogleCalendarConnectorFromEnv(s.getGoogleTokenForUser),
	}
	if s.db != nil {
		_ = s.ensureAuthTables(context.Background())
	}
	return s
}

func (s *Service) GitHubConnectURL(telegramUserID int64) string {
	if s.githubClientID == "" || s.githubRedirectURI == "" {
		return "GitHub OAuth is not configured. Set GITHUB_CLIENT_ID and GITHUB_REDIRECT_URI."
	}
	state, err := s.createOAuthState(telegramUserID)
	if err != nil {
		return "GitHub OAuth state generation failed. Set OAUTH_STATE_SECRET."
	}
	v := url.Values{}
	v.Set("client_id", s.githubClientID)
	v.Set("redirect_uri", s.githubRedirectURI)
	v.Set("scope", "repo read:org")
	v.Set("state", state)
	return "https://github.com/login/oauth/authorize?" + v.Encode()
}

func (s *Service) GoogleConnectURL(telegramUserID int64) string {
	if s.googleClientID == "" || s.googleRedirectURI == "" {
		return "Google OAuth is not configured. Set GOOGLE_CLIENT_ID and GOOGLE_REDIRECT_URI."
	}
	state, err := s.createOAuthState(telegramUserID)
	if err != nil {
		return "Google OAuth state generation failed. Set OAUTH_STATE_SECRET."
	}
	v := url.Values{}
	v.Set("client_id", s.googleClientID)
	v.Set("redirect_uri", s.googleRedirectURI)
	v.Set("response_type", "code")
	v.Set("access_type", "offline")
	v.Set("prompt", "consent")
	v.Set("scope", strings.Join([]string{
		"https://www.googleapis.com/auth/documents.readonly",
		"https://www.googleapis.com/auth/drive.readonly",
		"https://www.googleapis.com/auth/calendar.readonly",
		"https://www.googleapis.com/auth/userinfo.email",
	}, " "))
	v.Set("state", state)
	return "https://accounts.google.com/o/oauth2/v2/auth?" + v.Encode()
}

func (s *Service) CollectAll(ctx context.Context, telegramUserID int64) ([]ExternalDocument, error) {
	docs := make([]ExternalDocument, 0)
	for _, connector := range s.connectors {
		batch, err := connector.Collect(ctx, telegramUserID)
		if err != nil {
			return nil, fmt.Errorf("%s connector failed: %w", connector.Name(), err)
		}
		docs = append(docs, batch...)
	}
	return docs, nil
}

func (s *Service) HandleGitHubCallback(ctx context.Context, code, state string) (int64, error) {
	if s.githubClientID == "" || s.githubClientSecret == "" || s.githubRedirectURI == "" {
		return 0, errors.New("github oauth callback is not configured")
	}
	userID, err := s.parseOAuthState(state)
	if err != nil {
		return 0, err
	}

	token, scope, err := s.exchangeGitHubCode(ctx, code)
	if err != nil {
		return 0, err
	}
	login, email, err := s.fetchGitHubIdentity(ctx, token)
	if err != nil {
		return 0, err
	}

	if err := s.storeGitHubIntegration(ctx, userID, login, email, token, scope); err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *Service) HandleGoogleCallback(ctx context.Context, code, state string) (int64, error) {
	if s.googleClientID == "" || s.googleClientSecret == "" || s.googleRedirectURI == "" {
		return 0, errors.New("google oauth callback is not configured")
	}
	userID, err := s.parseOAuthState(state)
	if err != nil {
		return 0, err
	}

	accessToken, refreshToken, scope, expiresAt, err := s.exchangeGoogleCode(ctx, code)
	if err != nil {
		return 0, err
	}
	email, err := s.fetchGoogleIdentity(ctx, accessToken)
	if err != nil {
		return 0, err
	}

	if err := s.storeGoogleIntegration(ctx, userID, email, accessToken, refreshToken, scope, expiresAt); err != nil {
		return 0, err
	}
	return userID, nil
}

type GitHubConnector struct {
	fallbackToken string
	repos         []string
	maxFiles      int
	client        *http.Client
	tokenResolver func(ctx context.Context, telegramUserID int64) (string, error)
}

func NewGitHubConnectorFromEnv(tokenResolver func(ctx context.Context, telegramUserID int64) (string, error)) *GitHubConnector {
	repoCSV := strings.TrimSpace(os.Getenv("GITHUB_REPOS"))
	repos := make([]string, 0)
	if repoCSV != "" {
		for _, item := range strings.Split(repoCSV, ",") {
			repo := strings.TrimSpace(item)
			if repo != "" {
				repos = append(repos, repo)
			}
		}
	}

	maxFiles := 200
	if raw := strings.TrimSpace(os.Getenv("GITHUB_MAX_FILES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxFiles = parsed
		}
	}

	return &GitHubConnector{
		fallbackToken: strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		repos:         repos,
		maxFiles:      maxFiles,
		tokenResolver: tokenResolver,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *GitHubConnector) Name() string { return "github" }

func (c *GitHubConnector) Collect(ctx context.Context, telegramUserID int64) ([]ExternalDocument, error) {
	token := c.fallbackToken
	if c.tokenResolver != nil {
		resolved, err := c.tokenResolver(ctx, telegramUserID)
		if err == nil && resolved != "" {
			token = resolved
		}
	}

	if token == "" {
		return nil, errors.New("GITHUB_TOKEN is missing")
	}
	if len(c.repos) == 0 {
		return nil, errors.New("GITHUB_REPOS is missing")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	docs := make([]ExternalDocument, 0)

	for _, repo := range c.repos {
		repoMeta, err := c.fetchRepoMetadata(ctx, token, repo)
		if err != nil {
			return nil, err
		}

		fileDocs, err := c.fetchRepoFiles(ctx, token, repo, repoMeta.DefaultBranch, telegramUserID, now)
		if err != nil {
			return nil, err
		}
		docs = append(docs, fileDocs...)

		issueDocs, err := c.fetchIssues(ctx, token, repo, telegramUserID, now)
		if err != nil {
			return nil, err
		}
		docs = append(docs, issueDocs...)

		prDocs, err := c.fetchPullRequests(ctx, token, repo, telegramUserID, now)
		if err != nil {
			return nil, err
		}
		docs = append(docs, prDocs...)
	}

	return docs, nil
}

func (c *GitHubConnector) fetchRepoMetadata(ctx context.Context, token, repo string) (*githubRepoMetadata, error) {
	path := fmt.Sprintf("https://api.github.com/repos/%s", repo)
	var out githubRepoMetadata
	if err := c.getJSON(ctx, token, path, &out); err != nil {
		return nil, fmt.Errorf("github repo metadata failed for %s: %w", repo, err)
	}
	if out.DefaultBranch == "" {
		out.DefaultBranch = "main"
	}
	return &out, nil
}

func (c *GitHubConnector) fetchRepoFiles(ctx context.Context, token, repo, branch string, telegramUserID int64, now string) ([]ExternalDocument, error) {
	path := fmt.Sprintf("https://api.github.com/repos/%s/git/trees/%s?recursive=1", repo, branch)
	var tree githubTreeResponse
	if err := c.getJSON(ctx, token, path, &tree); err != nil {
		return nil, fmt.Errorf("github tree listing failed for %s: %w", repo, err)
	}

	paths := make([]string, 0)
	for _, item := range tree.Tree {
		if item.Type != "blob" {
			continue
		}
		if includePath(item.Path) {
			paths = append(paths, item.Path)
		}
	}

	sort.Strings(paths)
	if len(paths) > c.maxFiles {
		paths = paths[:c.maxFiles]
	}

	results := make([]ExternalDocument, 0, len(paths))
	for _, p := range paths {
		content, err := c.fetchFileContent(ctx, token, repo, p, branch)
		if err != nil {
			continue
		}

		results = append(results, ExternalDocument{
			SourceID: fmt.Sprintf("github:%s", repo),
			Title:    fmt.Sprintf("%s:%s", repo, p),
			Content:  content,
			URL:      fmt.Sprintf("https://github.com/%s/blob/%s/%s", repo, branch, p),
			Tags:     []string{"github", "source-code", filepath.Ext(p)},
			Metadata: map[string]any{"user_id": telegramUserID, "repo": repo, "path": p, "synced_at": now, "kind": "file"},
		})
	}

	return results, nil
}

func (c *GitHubConnector) fetchFileContent(ctx context.Context, token, repo, path, branch string) (string, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=%s", repo, path, branch)
	var response githubContentResponse
	if err := c.getJSON(ctx, token, endpoint, &response); err != nil {
		return "", err
	}

	encoded := strings.ReplaceAll(response.Content, "\n", "")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (c *GitHubConnector) fetchIssues(ctx context.Context, token, repo string, telegramUserID int64, now string) ([]ExternalDocument, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/issues?state=open&per_page=20", repo)
	var issues []githubIssue
	if err := c.getJSON(ctx, token, endpoint, &issues); err != nil {
		return nil, fmt.Errorf("github issues fetch failed for %s: %w", repo, err)
	}

	results := make([]ExternalDocument, 0)
	for _, issue := range issues {
		if issue.PullRequest.URL != "" {
			continue
		}
		content := fmt.Sprintf("Title: %s\nState: %s\nBody:\n%s", issue.Title, issue.State, issue.Body)
		results = append(results, ExternalDocument{
			SourceID: fmt.Sprintf("github:%s", repo),
			Title:    fmt.Sprintf("Issue #%d: %s", issue.Number, issue.Title),
			Content:  content,
			URL:      issue.HTMLURL,
			Tags:     []string{"github", "issue"},
			Metadata: map[string]any{"user_id": telegramUserID, "repo": repo, "number": issue.Number, "synced_at": now, "kind": "issue"},
		})
	}

	return results, nil
}

func (c *GitHubConnector) fetchPullRequests(ctx context.Context, token, repo string, telegramUserID int64, now string) ([]ExternalDocument, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/pulls?state=open&per_page=20", repo)
	var prs []githubPR
	if err := c.getJSON(ctx, token, endpoint, &prs); err != nil {
		return nil, fmt.Errorf("github pull requests fetch failed for %s: %w", repo, err)
	}

	results := make([]ExternalDocument, 0, len(prs))
	for _, pr := range prs {
		content := fmt.Sprintf("Title: %s\nState: %s\nBody:\n%s", pr.Title, pr.State, pr.Body)
		results = append(results, ExternalDocument{
			SourceID: fmt.Sprintf("github:%s", repo),
			Title:    fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title),
			Content:  content,
			URL:      pr.HTMLURL,
			Tags:     []string{"github", "pull-request"},
			Metadata: map[string]any{"user_id": telegramUserID, "repo": repo, "number": pr.Number, "synced_at": now, "kind": "pull-request"},
		})
	}

	return results, nil
}

func (c *GitHubConnector) getJSON(ctx context.Context, token, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("github api status %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Service) createOAuthState(userID int64) (string, error) {
	if s.oauthStateSecret == "" {
		return "", errors.New("OAUTH_STATE_SECRET is missing")
	}
	raw := fmt.Sprintf("%d:%d", userID, time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(s.oauthStateSecret))
	_, _ = mac.Write([]byte(raw))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(raw + "." + sig)), nil
}

func (s *Service) parseOAuthState(encoded string) (int64, error) {
	if s.oauthStateSecret == "" {
		return 0, errors.New("OAUTH_STATE_SECRET is missing")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return 0, errors.New("invalid oauth state encoding")
	}
	parts := strings.SplitN(string(decoded), ".", 2)
	if len(parts) != 2 {
		return 0, errors.New("invalid oauth state format")
	}
	raw, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, []byte(s.oauthStateSecret))
	_, _ = mac.Write([]byte(raw))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return 0, errors.New("oauth state signature mismatch")
	}

	chunks := strings.Split(raw, ":")
	if len(chunks) < 1 {
		return 0, errors.New("oauth state has no user id")
	}
	userID, err := strconv.ParseInt(chunks[0], 10, 64)
	if err != nil || userID <= 0 {
		return 0, errors.New("oauth state user id is invalid")
	}
	return userID, nil
}

func (s *Service) exchangeGitHubCode(ctx context.Context, code string) (token string, scope string, err error) {
	v := url.Values{}
	v.Set("client_id", s.githubClientID)
	v.Set("client_secret", s.githubClientSecret)
	v.Set("code", code)
	v.Set("redirect_uri", s.githubRedirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(v.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("github token exchange failed: %s", string(body))
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", err
	}
	if payload.AccessToken == "" {
		return "", "", errors.New("github token exchange returned empty token")
	}
	return payload.AccessToken, payload.Scope, nil
}

func (s *Service) exchangeGoogleCode(ctx context.Context, code string) (accessToken string, refreshToken string, scope string, expiresAt string, err error) {
	v := url.Values{}
	v.Set("client_id", s.googleClientID)
	v.Set("client_secret", s.googleClientSecret)
	v.Set("code", code)
	v.Set("redirect_uri", s.googleRedirectURI)
	v.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(v.Encode()))
	if err != nil {
		return "", "", "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", "", "", fmt.Errorf("google token exchange failed: %s", string(body))
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", "", "", err
	}
	if payload.AccessToken == "" {
		return "", "", "", "", errors.New("google token exchange returned empty access token")
	}

	expiresAt = time.Now().UTC().Add(time.Duration(payload.ExpiresIn) * time.Second).Format(time.RFC3339)
	return payload.AccessToken, payload.RefreshToken, payload.Scope, expiresAt, nil
}

func (s *Service) fetchGitHubIdentity(ctx context.Context, token string) (login string, email string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("github identity fetch failed: %s", string(body))
	}

	var profile struct {
		Login string `json:"login"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return "", "", err
	}
	return profile.Login, profile.Email, nil
}

func (s *Service) fetchGoogleIdentity(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("google identity fetch failed: %s", string(body))
	}

	var profile struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return "", err
	}
	return profile.Email, nil
}

func (s *Service) ensureAuthTables(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS users (
    telegram_id TEXT PRIMARY KEY,
    telegram_username TEXT,
    display_name TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS integrations (
    id TEXT PRIMARY KEY,
    telegram_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    account_email TEXT,
    access_token_enc TEXT NOT NULL,
    refresh_token_enc TEXT,
    scopes TEXT NOT NULL,
    expires_at TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (telegram_id) REFERENCES users(telegram_id)
);
CREATE TABLE IF NOT EXISTS sync_jobs (
	id TEXT PRIMARY KEY,
	telegram_user_id TEXT NOT NULL,
	status TEXT NOT NULL,
	started_at TEXT NOT NULL,
	completed_at TEXT,
	message TEXT
);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_user_started ON sync_jobs(telegram_user_id, started_at DESC);
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	telegram_chat_id TEXT NOT NULL,
	telegram_user_id TEXT NOT NULL,
	started_at TEXT NOT NULL,
	last_activity_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_activity ON chat_sessions(last_activity_at DESC);
CREATE TABLE IF NOT EXISTS pending_questions (
	id TEXT PRIMARY KEY,
	telegram_chat_id TEXT NOT NULL,
	telegram_message_id TEXT NOT NULL,
	telegram_user_id TEXT NOT NULL,
	question TEXT NOT NULL,
	created_at TEXT NOT NULL,
	answered_at TEXT,
	dispatched_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_pending_questions_state ON pending_questions(answered_at, dispatched_at, created_at);
`
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

func (s *Service) storeGitHubIntegration(ctx context.Context, userID int64, login, email, token, scopes string) error {
	if s.db == nil {
		return errors.New("database is not initialized")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	telegramID := strconv.FormatInt(userID, 10)

	if _, err := s.db.ExecContext(ctx, `
INSERT INTO users (telegram_id, telegram_username, display_name, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(telegram_id) DO UPDATE SET
  telegram_username = excluded.telegram_username,
  display_name = excluded.display_name,
  updated_at = excluded.updated_at
`, telegramID, login, login, now, now); err != nil {
		return fmt.Errorf("failed to upsert user: %w", err)
	}

	integrationID := fmt.Sprintf("github:%s", telegramID)
	encryptedToken, err := s.encryptToken(token)
	if err != nil {
		return fmt.Errorf("failed to encrypt github token: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO integrations (
    id, telegram_id, provider, account_email, access_token_enc, refresh_token_enc,
    scopes, expires_at, status, created_at, updated_at
) VALUES (?, ?, 'github', ?, ?, '', ?, NULL, 'active', ?, ?)
ON CONFLICT(id) DO UPDATE SET
    account_email = excluded.account_email,
    access_token_enc = excluded.access_token_enc,
    scopes = excluded.scopes,
    updated_at = excluded.updated_at,
    status = 'active'
`, integrationID, telegramID, email, encryptedToken, scopes, now, now)
	if err != nil {
		return fmt.Errorf("failed to upsert github integration: %w", err)
	}
	return nil
}

func (s *Service) storeGoogleIntegration(ctx context.Context, userID int64, email, accessToken, refreshToken, scopes, expiresAt string) error {
	if s.db == nil {
		return errors.New("database is not initialized")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	telegramID := strconv.FormatInt(userID, 10)
	encryptedAccessToken, err := s.encryptToken(accessToken)
	if err != nil {
		return fmt.Errorf("failed to encrypt google access token: %w", err)
	}
	encryptedRefreshToken, err := s.encryptToken(refreshToken)
	if err != nil {
		return fmt.Errorf("failed to encrypt google refresh token: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
INSERT INTO users (telegram_id, telegram_username, display_name, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(telegram_id) DO UPDATE SET
  telegram_username = excluded.telegram_username,
  display_name = excluded.display_name,
  updated_at = excluded.updated_at
`, telegramID, email, email, now, now); err != nil {
		return fmt.Errorf("failed to upsert user: %w", err)
	}

	integrationID := fmt.Sprintf("google:%s", telegramID)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO integrations (
    id, telegram_id, provider, account_email, access_token_enc, refresh_token_enc,
	scopes, expires_at, status, created_at, updated_at
) VALUES (?, ?, 'google', ?, ?, ?, ?, ?, 'active', ?, ?)
ON CONFLICT(id) DO UPDATE SET
    account_email = excluded.account_email,
    access_token_enc = excluded.access_token_enc,
	refresh_token_enc = CASE WHEN excluded.refresh_token_enc = '' THEN integrations.refresh_token_enc ELSE excluded.refresh_token_enc END,
    scopes = excluded.scopes,
	expires_at = excluded.expires_at,
    updated_at = excluded.updated_at,
    status = 'active'
`, integrationID, telegramID, email, encryptedAccessToken, encryptedRefreshToken, scopes, expiresAt, now, now)
	if err != nil {
		return fmt.Errorf("failed to upsert google integration: %w", err)
	}
	return nil
}

func (s *Service) getGitHubTokenForUser(ctx context.Context, telegramUserID int64) (string, error) {
	if s.db == nil {
		return "", errors.New("database is not initialized")
	}
	telegramID := strconv.FormatInt(telegramUserID, 10)
	row := s.db.QueryRowContext(ctx, `
SELECT access_token_enc
FROM integrations
WHERE telegram_id = ? AND provider = 'github' AND status = 'active'
ORDER BY updated_at DESC
LIMIT 1
`, telegramID)

	var token string
	if err := row.Scan(&token); err != nil {
		return "", err
	}
	return s.decryptToken(token)
}

func (s *Service) getGoogleTokenForUser(ctx context.Context, telegramUserID int64) (string, error) {
	if s.db == nil {
		return "", errors.New("database is not initialized")
	}
	telegramID := strconv.FormatInt(telegramUserID, 10)
	row := s.db.QueryRowContext(ctx, `
SELECT id, access_token_enc, refresh_token_enc, expires_at
FROM integrations
WHERE telegram_id = ? AND provider = 'google' AND status = 'active'
ORDER BY updated_at DESC
LIMIT 1
`, telegramID)

	var integrationID, encryptedAccess string
	var encryptedRefresh, expiresAt sql.NullString
	if err := row.Scan(&integrationID, &encryptedAccess, &encryptedRefresh, &expiresAt); err != nil {
		return "", err
	}

	accessToken, err := s.decryptToken(encryptedAccess)
	if err != nil {
		return "", err
	}
	refreshToken, err := s.decryptToken(encryptedRefresh.String)
	if err != nil {
		return "", err
	}

	if tokenExpired(expiresAt.String) && refreshToken != "" {
		newAccessToken, newExpiresAt, refreshErr := s.refreshGoogleAccessToken(ctx, refreshToken)
		if refreshErr != nil {
			_ = s.markIntegrationStatus(ctx, integrationID, "revoked")
			return "", refreshErr
		}
		encryptedNewAccess, encErr := s.encryptToken(newAccessToken)
		if encErr != nil {
			return "", encErr
		}
		now := time.Now().UTC().Format(time.RFC3339)
		_, _ = s.db.ExecContext(ctx, `
UPDATE integrations
SET access_token_enc = ?, expires_at = ?, updated_at = ?, status = 'active'
WHERE id = ?
`, encryptedNewAccess, newExpiresAt, now, integrationID)
		return newAccessToken, nil
	}

	return accessToken, nil
}

func deriveTokenKey(secret string) []byte {
	if strings.TrimSpace(secret) == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func (s *Service) encryptToken(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	if len(s.tokenEncryptionKey) == 0 {
		return value, nil
	}
	block, err := aes.NewCipher(s.tokenEncryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(value), nil)
	payload := append(nonce, ciphertext...)
	return "enc:v1:" + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (s *Service) decryptToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "enc:v1:") {
		return value, nil
	}
	if len(s.tokenEncryptionKey) == 0 {
		return "", errors.New("encrypted token cannot be decrypted without TOKEN_ENCRYPTION_KEY")
	}
	encoded := strings.TrimPrefix(value, "enc:v1:")
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.tokenEncryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) <= nonceSize {
		return "", errors.New("invalid encrypted token payload")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func tokenExpired(expiresAt string) bool {
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return false
	}
	return time.Now().UTC().After(t.Add(-1 * time.Minute))
}

func (s *Service) refreshGoogleAccessToken(ctx context.Context, refreshToken string) (string, string, error) {
	v := url.Values{}
	v.Set("client_id", s.googleClientID)
	v.Set("client_secret", s.googleClientSecret)
	v.Set("refresh_token", refreshToken)
	v.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(v.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("google refresh token exchange failed: %s", string(body))
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", err
	}
	if payload.AccessToken == "" {
		return "", "", errors.New("google refresh token exchange returned empty access token")
	}
	expiresAt := time.Now().UTC().Add(time.Duration(payload.ExpiresIn) * time.Second).Format(time.RFC3339)
	return payload.AccessToken, expiresAt, nil
}

func (s *Service) markIntegrationStatus(ctx context.Context, integrationID, status string) error {
	if s.db == nil || strings.TrimSpace(integrationID) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE integrations
SET status = ?, updated_at = ?
WHERE id = ?
`, status, time.Now().UTC().Format(time.RFC3339), integrationID)
	return err
}

func includePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	allowed := map[string]bool{
		".go":   true,
		".ts":   true,
		".tsx":  true,
		".js":   true,
		".jsx":  true,
		".py":   true,
		".md":   true,
		".txt":  true,
		".rst":  true,
		".json": true,
		".yaml": true,
		".yml":  true,
		".toml": true,
		".mod":  true,
		".sql":  true,
	}

	if allowed[ext] {
		return true
	}

	base := strings.ToLower(filepath.Base(path))
	if base == "readme" || strings.HasPrefix(base, "readme.") || base == "dockerfile" {
		return true
	}

	return false
}

type githubRepoMetadata struct {
	DefaultBranch string `json:"default_branch"`
}

type githubTreeResponse struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
}

type githubContentResponse struct {
	Content string `json:"content"`
}

type githubIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PullRequest struct {
		URL string `json:"url"`
	} `json:"pull_request"`
}

type githubPR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

type GoogleDocsConnector struct {
	fallbackToken string
	maxDocs       int
	client        *http.Client
	tokenResolver func(ctx context.Context, telegramUserID int64) (string, error)
}

func NewGoogleDocsConnectorFromEnv(tokenResolver func(ctx context.Context, telegramUserID int64) (string, error)) *GoogleDocsConnector {
	maxDocs := 20
	if raw := strings.TrimSpace(os.Getenv("GOOGLE_DOCS_MAX")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxDocs = parsed
		}
	}
	return &GoogleDocsConnector{
		fallbackToken: strings.TrimSpace(os.Getenv("GOOGLE_TOKEN")),
		maxDocs:       maxDocs,
		tokenResolver: tokenResolver,
		client:        &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *GoogleDocsConnector) Name() string { return "google_docs" }

func (c *GoogleDocsConnector) Collect(ctx context.Context, telegramUserID int64) ([]ExternalDocument, error) {
	token := c.fallbackToken
	if c.tokenResolver != nil {
		resolved, err := c.tokenResolver(ctx, telegramUserID)
		if err == nil && resolved != "" {
			token = resolved
		}
	}
	if token == "" {
		return nil, errors.New("google token is missing")
	}

	query := url.QueryEscape("mimeType='application/vnd.google-apps.document' and trashed=false")
	endpoint := fmt.Sprintf("https://www.googleapis.com/drive/v3/files?pageSize=%d&fields=files(id,name,modifiedTime,webViewLink)&q=%s", c.maxDocs, query)

	var listResp struct {
		Files []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			ModifiedTime string `json:"modifiedTime"`
			WebViewLink  string `json:"webViewLink"`
		} `json:"files"`
	}
	if err := c.getJSON(ctx, token, endpoint, &listResp); err != nil {
		return nil, fmt.Errorf("google docs list failed: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	results := make([]ExternalDocument, 0, len(listResp.Files))
	for _, f := range listResp.Files {
		text, err := c.fetchDocText(ctx, token, f.ID)
		if err != nil {
			continue
		}
		results = append(results, ExternalDocument{
			SourceID: fmt.Sprintf("google_docs:%s", f.ID),
			Title:    f.Name,
			Content:  text,
			URL:      f.WebViewLink,
			Tags:     []string{"google-docs", "document"},
			Metadata: map[string]any{"user_id": telegramUserID, "doc_id": f.ID, "modified_time": f.ModifiedTime, "synced_at": now},
		})
	}
	return results, nil
}

func (c *GoogleDocsConnector) fetchDocText(ctx context.Context, token, docID string) (string, error) {
	endpoint := fmt.Sprintf("https://docs.googleapis.com/v1/documents/%s", docID)
	var doc struct {
		Title string `json:"title"`
		Body  struct {
			Content []struct {
				Paragraph struct {
					Elements []struct {
						TextRun struct {
							Content string `json:"content"`
						} `json:"textRun"`
					} `json:"elements"`
				} `json:"paragraph"`
			} `json:"content"`
		} `json:"body"`
	}
	if err := c.getJSON(ctx, token, endpoint, &doc); err != nil {
		return "", err
	}

	var b strings.Builder
	if doc.Title != "" {
		b.WriteString(doc.Title)
		b.WriteString("\n\n")
	}
	for _, block := range doc.Body.Content {
		for _, el := range block.Paragraph.Elements {
			if txt := strings.TrimSpace(el.TextRun.Content); txt != "" {
				b.WriteString(txt)
				b.WriteString("\n")
			}
		}
	}
	return b.String(), nil
}

func (c *GoogleDocsConnector) getJSON(ctx context.Context, token, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("google docs api status %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type GoogleCalendarConnector struct {
	fallbackToken string
	maxEvents     int
	calendarID    string
	client        *http.Client
	tokenResolver func(ctx context.Context, telegramUserID int64) (string, error)
}

func NewGoogleCalendarConnectorFromEnv(tokenResolver func(ctx context.Context, telegramUserID int64) (string, error)) *GoogleCalendarConnector {
	maxEvents := 25
	if raw := strings.TrimSpace(os.Getenv("GOOGLE_CALENDAR_MAX_EVENTS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxEvents = parsed
		}
	}
	calendarID := strings.TrimSpace(os.Getenv("GOOGLE_CALENDAR_ID"))
	if calendarID == "" {
		calendarID = "primary"
	}
	return &GoogleCalendarConnector{
		fallbackToken: strings.TrimSpace(os.Getenv("GOOGLE_TOKEN")),
		maxEvents:     maxEvents,
		calendarID:    calendarID,
		tokenResolver: tokenResolver,
		client:        &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *GoogleCalendarConnector) Name() string { return "google_calendar" }

func (c *GoogleCalendarConnector) Collect(ctx context.Context, telegramUserID int64) ([]ExternalDocument, error) {
	token := c.fallbackToken
	if c.tokenResolver != nil {
		resolved, err := c.tokenResolver(ctx, telegramUserID)
		if err == nil && resolved != "" {
			token = resolved
		}
	}
	if token == "" {
		return nil, errors.New("google token is missing")
	}

	minTime := url.QueryEscape(time.Now().UTC().Format(time.RFC3339))
	calID := url.PathEscape(c.calendarID)
	endpoint := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events?singleEvents=true&orderBy=startTime&maxResults=%d&timeMin=%s", calID, c.maxEvents, minTime)

	var listResp struct {
		Items []struct {
			ID       string `json:"id"`
			Summary  string `json:"summary"`
			Status   string `json:"status"`
			HTMLLink string `json:"htmlLink"`
			Start    struct {
				Date     string `json:"date"`
				DateTime string `json:"dateTime"`
			} `json:"start"`
			End struct {
				Date     string `json:"date"`
				DateTime string `json:"dateTime"`
			} `json:"end"`
			Description string `json:"description"`
			Location    string `json:"location"`
		} `json:"items"`
	}
	if err := c.getJSON(ctx, token, endpoint, &listResp); err != nil {
		return nil, fmt.Errorf("google calendar list failed: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	results := make([]ExternalDocument, 0, len(listResp.Items))
	for _, ev := range listResp.Items {
		start := ev.Start.DateTime
		if start == "" {
			start = ev.Start.Date
		}
		end := ev.End.DateTime
		if end == "" {
			end = ev.End.Date
		}
		content := fmt.Sprintf("Summary: %s\nStatus: %s\nStart: %s\nEnd: %s\nLocation: %s\nDescription:\n%s", ev.Summary, ev.Status, start, end, ev.Location, ev.Description)
		results = append(results, ExternalDocument{
			SourceID: fmt.Sprintf("google_calendar:%s", ev.ID),
			Title:    ev.Summary,
			Content:  content,
			URL:      ev.HTMLLink,
			Tags:     []string{"google-calendar", "event"},
			Metadata: map[string]any{"user_id": telegramUserID, "event_id": ev.ID, "synced_at": now},
		})
	}
	return results, nil
}

func (c *GoogleCalendarConnector) getJSON(ctx context.Context, token, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("google calendar api status %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
