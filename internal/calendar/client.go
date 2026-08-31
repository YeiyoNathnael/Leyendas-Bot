package calendar

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// OAuthScopes needed for Calendar and Google Meet scheduling.
var Scopes = []string{
	calendar.CalendarScope,
	calendar.CalendarEventsScope,
}

// NewOAuthConfig creates standard OAuth2 configuration for Google Calendar.
func NewOAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       Scopes,
	}
}

// Service wraps the Google Calendar v3 service and configuration.
type Service struct {
	srv    *calendar.Service
	config *oauth2.Config
	ts     oauth2.TokenSource
}

// TokenRefreshedCallback is an optional callback when a new token is minted.
type TokenRefreshedCallback func(token *oauth2.Token)

// PersistingTokenSource wraps an underlying TokenSource to notify when access tokens change.
type PersistingTokenSource struct {
	mu         sync.Mutex
	underlying oauth2.TokenSource
	lastToken  *oauth2.Token
	onRefresh  TokenRefreshedCallback
}

func (pts *PersistingTokenSource) Token() (*oauth2.Token, error) {
	pts.mu.Lock()
	defer pts.mu.Unlock()

	newToken, err := pts.underlying.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve/refresh oauth2 token: %w", err)
	}

	if pts.lastToken == nil || newToken.AccessToken != pts.lastToken.AccessToken {
		if pts.onRefresh != nil {
			pts.onRefresh(newToken)
		}
		pts.lastToken = newToken
	}

	return newToken, nil
}

// NewService initializes a new Calendar Service using stored refresh token.
func NewService(ctx context.Context, clientID, clientSecret string, refreshToken string, onRefresh TokenRefreshedCallback) (*Service, error) {
	cfg := NewOAuthConfig(clientID, clientSecret, "")
	initialToken := &oauth2.Token{
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-1 * time.Hour), // Triggers immediate initial refresh if needed
	}

	baseSource := cfg.TokenSource(ctx, initialToken)
	persistingSource := &PersistingTokenSource{
		underlying: baseSource,
		lastToken:  initialToken,
		onRefresh:  onRefresh,
	}

	srv, err := calendar.NewService(ctx, option.WithTokenSource(persistingSource))
	if err != nil {
		return nil, fmt.Errorf("failed to create google calendar service: %w", err)
	}

	return &Service{
		srv:    srv,
		config: cfg,
		ts:     persistingSource,
	}, nil
}

// RawService returns the underlying *calendar.Service.
func (s *Service) RawService() *calendar.Service {
	return s.srv
}
