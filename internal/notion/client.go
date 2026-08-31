package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// Client is a production-grade rate-limited client for the Notion REST API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
	limiter    *rate.Limiter
	maxRetries int
}

// NewClient initializes a new Notion API client with rate limiting (3 req/sec average).
func NewClient(token string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		baseURL:    APIBaseURL,
		token:      token,
		limiter:    rate.NewLimiter(rate.Every(333*time.Millisecond), 5), // 3 req/sec with burst of 5
		maxRetries: 3,
	}
}

// SetBaseURL allows overriding the API endpoint for unit testing.
func (c *Client) SetBaseURL(url string) {
	c.baseURL = url
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, body any) ([]byte, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
	}

	reqURL := fmt.Sprintf("%s%s", c.baseURL, endpoint)

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter wait: %w", err)
		}

		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Notion-Version", APIVersion)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("execute request: %w", err)
		}

		respBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response body: %w", err)
		}

		// Handle HTTP 429 Rate Limit
		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt == c.maxRetries {
				return nil, fmt.Errorf("notion rate limit exceeded after %d retries: %s", attempt, string(respBytes))
			}

			retryAfterSec := 1
			if h := resp.Header.Get("Retry-After"); h != "" {
				if s, err := strconv.Atoi(h); err == nil && s > 0 {
					retryAfterSec = s
				}
			}

			backoff := time.Duration(retryAfterSec)*time.Second + time.Duration(rand.Intn(400))*time.Millisecond
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("notion api error [status %d]: %s", resp.StatusCode, string(respBytes))
		}

		return respBytes, nil
	}

	return nil, fmt.Errorf("max retries exceeded for %s %s", method, endpoint)
}

// QueryDatabase executes a query on a database.
func (c *Client) QueryDatabase(ctx context.Context, databaseID string, req *QueryDatabaseRequest) (*QueryDatabaseResponse, error) {
	endpoint := fmt.Sprintf("/databases/%s/query", databaseID)
	data, err := c.doRequest(ctx, http.MethodPost, endpoint, req)
	if err != nil {
		return nil, err
	}

	var resp QueryDatabaseResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal database query response: %w", err)
	}
	return &resp, nil
}

// CreatePage creates a new page in a database.
func (c *Client) CreatePage(ctx context.Context, req *CreatePageRequest) (*Page, error) {
	data, err := c.doRequest(ctx, http.MethodPost, "/pages", req)
	if err != nil {
		return nil, err
	}

	var page Page
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("unmarshal create page response: %w", err)
	}
	return &page, nil
}

// UpdatePageProperties updates properties on an existing page.
func (c *Client) UpdatePageProperties(ctx context.Context, pageID string, req *UpdatePageRequest) (*Page, error) {
	endpoint := fmt.Sprintf("/pages/%s", pageID)
	data, err := c.doRequest(ctx, http.MethodPatch, endpoint, req)
	if err != nil {
		return nil, err
	}

	var page Page
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("unmarshal update page response: %w", err)
	}
	return &page, nil
}
