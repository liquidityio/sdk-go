// Package sdk provides a typed HTTP client for the Liquidity platform APIs.
//
// Usage:
//
//	c := sdk.NewClient(sdk.Config{Env: "dev", Token: "...", OrgID: "mlc"})
//	app, err := c.Onboarding.Create(ctx, sdk.CreateApplicationReq{...})
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config controls which platform services the client talks to.
type Config struct {
	// Env selects the host set: dev, test, main.
	Env   string
	Token string
	OrgID string

	// Override individual service URLs.
	BDURL  string
	TAURL  string
	ATSURL string
	IAMURL string
}

// Client wraps all platform API surfaces.
type Client struct {
	cfg  Config
	http *http.Client

	Onboarding *OnboardingClient
	Documents  *DocumentsClient
	Accounts   *AccountsClient
	Portfolio  *PortfolioClient
	Orders     *OrdersClient
	Trading    *TradingClient
}

// NewClient returns a configured platform client.
func NewClient(cfg Config) *Client {
	if cfg.Env == "" {
		cfg.Env = os.Getenv("LIQUIDITY_ENV")
	}
	if cfg.Env == "" {
		cfg.Env = "dev"
	}
	if cfg.Token == "" {
		cfg.Token = os.Getenv("LIQUIDITY_TOKEN")
	}
	if cfg.OrgID == "" {
		cfg.OrgID = os.Getenv("LIQUIDITY_ORG_ID")
	}

	if cfg.BDURL == "" {
		cfg.BDURL = os.Getenv("BD_URL")
	}
	if cfg.BDURL == "" {
		cfg.BDURL = serviceURL("bd", cfg.Env)
	}
	if cfg.TAURL == "" {
		cfg.TAURL = os.Getenv("TA_URL")
	}
	if cfg.TAURL == "" {
		cfg.TAURL = serviceURL("ta", cfg.Env)
	}
	if cfg.ATSURL == "" {
		cfg.ATSURL = os.Getenv("ATS_URL")
	}
	if cfg.ATSURL == "" {
		cfg.ATSURL = serviceURL("ats", cfg.Env)
	}
	if cfg.IAMURL == "" {
		cfg.IAMURL = os.Getenv("IAM_URL")
	}
	if cfg.IAMURL == "" {
		cfg.IAMURL = serviceURL("iam", cfg.Env)
	}

	hc := &http.Client{Timeout: 30 * time.Second}

	c := &Client{cfg: cfg, http: hc}
	c.Onboarding = &OnboardingClient{c: c}
	c.Documents = &DocumentsClient{c: c}
	c.Accounts = &AccountsClient{c: c}
	c.Portfolio = &PortfolioClient{c: c}
	c.Orders = &OrdersClient{c: c}
	c.Trading = &TradingClient{c: c}
	return c
}

func serviceURL(service, env string) string {
	domain := "satschel.com"
	switch env {
	case "main":
		return fmt.Sprintf("https://%s.main.%s", service, domain)
	case "test":
		return fmt.Sprintf("https://%s.test.%s", service, domain)
	default:
		return fmt.Sprintf("https://%s.dev.%s", service, domain)
	}
}

// do executes an HTTP request with auth and org headers.
func (c *Client) do(ctx context.Context, method, url string, body any) (json.RawMessage, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
	if c.cfg.OrgID != "" {
		req.Header.Set("X-Org-Id", c.cfg.OrgID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(respBody), 200))
	}

	return json.RawMessage(respBody), resp.StatusCode, nil
}

// doWithIdem executes with an Idempotency-Key header.
func (c *Client) doWithIdem(ctx context.Context, method, url string, body any, idemKey string) (json.RawMessage, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
	if c.cfg.OrgID != "" {
		req.Header.Set("X-Org-Id", c.cfg.OrgID)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(respBody), 200))
	}

	return json.RawMessage(respBody), resp.StatusCode, nil
}

func truncateStr(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// bdURL returns the full URL for a BD endpoint.
func (c *Client) bdURL(path string) string {
	return strings.TrimRight(c.cfg.BDURL, "/") + path
}

// atsURL returns the full URL for an ATS endpoint.
func (c *Client) atsURL(path string) string {
	return strings.TrimRight(c.cfg.ATSURL, "/") + path
}

// taURL returns the full URL for a TA endpoint.
func (c *Client) taURL(path string) string {
	return strings.TrimRight(c.cfg.TAURL, "/") + path
}
