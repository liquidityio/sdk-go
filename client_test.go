package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient(Config{Env: "dev", Token: "tok", OrgID: "mlc"})
	if c.cfg.Env != "dev" {
		t.Fatalf("expected env=dev, got %s", c.cfg.Env)
	}
	if !strings.Contains(c.cfg.BDURL, "bd.dev.") {
		t.Fatalf("expected BD URL to contain bd.dev., got %s", c.cfg.BDURL)
	}
	if c.Onboarding == nil {
		t.Fatal("Onboarding client is nil")
	}
}

func TestNewClient_Overrides(t *testing.T) {
	c := NewClient(Config{
		Env:   "test",
		BDURL: "http://localhost:8080",
	})
	if c.cfg.BDURL != "http://localhost:8080" {
		t.Fatalf("expected override URL, got %s", c.cfg.BDURL)
	}
}

func TestOnboarding_Create(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/bd/onboarding") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing auth header")
		}
		if r.Header.Get("X-Org-Id") != "mlc" {
			t.Fatalf("missing org header")
		}

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["email"] != "test@liquidity.io" {
			t.Fatalf("unexpected email: %s", body["email"])
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "app_123",
			"email":        "test@liquidity.io",
			"first_name":   "Test",
			"last_name":    "User",
			"status":       "in_progress",
			"current_step": 1,
		})
	}))
	defer srv.Close()

	c := NewClient(Config{
		Env:   "dev",
		Token: "test-token",
		OrgID: "mlc",
		BDURL: srv.URL,
	})

	app, err := c.Onboarding.Create(context.Background(), CreateApplicationReq{
		Email:     "test@liquidity.io",
		FirstName: "Test",
		LastName:  "User",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.ID != "app_123" {
		t.Fatalf("expected app_123, got %s", app.ID)
	}
	if app.Status != "in_progress" {
		t.Fatalf("expected in_progress, got %s", app.Status)
	}
}

func TestOnboarding_Get(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "app_123",
			"status":       "approved",
			"current_step": 5,
		})
	}))
	defer srv.Close()

	c := NewClient(Config{BDURL: srv.URL, Token: "t", OrgID: "x"})
	app, err := c.Onboarding.Get(context.Background(), "app_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.Status != "approved" {
		t.Fatalf("expected approved, got %s", app.Status)
	}
}

func TestOnboarding_SubmitIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/identity") {
			t.Fatalf("expected /identity path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "app_123",
			"status":       "in_progress",
			"current_step": 2,
		})
	}))
	defer srv.Close()

	c := NewClient(Config{BDURL: srv.URL, Token: "t", OrgID: "x"})
	app, err := c.Onboarding.SubmitIdentity(context.Background(), "app_123", IdentityReq{
		DateOfBirth: "1990-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.CurrentStep != 2 {
		t.Fatalf("expected step 2, got %d", app.CurrentStep)
	}
}

func TestOnboarding_Screen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/screen") {
			t.Fatalf("expected /screen path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"app_id":     "app_123",
			"aml_status": "cleared",
			"kyc_status": "verified",
			"risk_level": "low",
			"risk_score": 0.1,
		})
	}))
	defer srv.Close()

	c := NewClient(Config{BDURL: srv.URL, Token: "t", OrgID: "x"})
	result, err := c.Onboarding.Screen(context.Background(), "app_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AMLStatus != "cleared" {
		t.Fatalf("expected cleared, got %s", result.AMLStatus)
	}
}

func TestOnboarding_Submit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/submit") {
			t.Fatalf("expected /submit path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "app_123",
			"status":       "submitted",
			"current_step": 5,
		})
	}))
	defer srv.Close()

	c := NewClient(Config{BDURL: srv.URL, Token: "t", OrgID: "x"})
	app, err := c.Onboarding.Submit(context.Background(), "app_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.Status != "submitted" {
		t.Fatalf("expected submitted, got %s", app.Status)
	}
}

func TestClient_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BDURL: srv.URL, Token: "t", OrgID: "x"})
	_, err := c.Onboarding.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 in error, got: %v", err)
	}
}

func TestServiceURL(t *testing.T) {
	tests := []struct {
		service string
		env     string
		want    string
	}{
		{"bd", "dev", "https://bd.dev.satschel.com"},
		{"ats", "test", "https://ats.test.satschel.com"},
		{"ta", "main", "https://ta.main.satschel.com"},
	}
	for _, tt := range tests {
		got := serviceURL(tt.service, tt.env)
		if got != tt.want {
			t.Errorf("serviceURL(%s, %s) = %s, want %s", tt.service, tt.env, got, tt.want)
		}
	}
}
