package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

// OnboardingClient wraps the BD onboarding pipeline.
type OnboardingClient struct {
	c *Client
}

// Application is the response from the onboarding API.
type Application struct {
	ID          string         `json:"id"`
	UserID      string         `json:"user_id"`
	OrgID       string         `json:"org_id"`
	Email       string         `json:"email"`
	FirstName   string         `json:"first_name"`
	LastName    string         `json:"last_name"`
	Phone       string         `json:"phone"`
	Country     string         `json:"country"`
	Status      string         `json:"status"`
	CurrentStep int            `json:"current_step"`
	KYCStatus   string         `json:"kyc_status"`
	AMLStatus   string         `json:"aml_status"`
	RiskLevel   string         `json:"risk_level"`
	Steps       []StepStatus   `json:"steps"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

// StepStatus tracks a single onboarding step.
type StepStatus struct {
	Step        int    `json:"step"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// CreateApplicationReq is step 1 input.
type CreateApplicationReq struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone,omitempty"`
	Country   string `json:"country,omitempty"`
}

// IdentityReq is step 2 input.
type IdentityReq struct {
	DateOfBirth  string `json:"date_of_birth"`
	SSN          string `json:"ssn,omitempty"`
	AddressLine1 string `json:"address_line1,omitempty"`
	AddressLine2 string `json:"address_line2,omitempty"`
	City         string `json:"city,omitempty"`
	State        string `json:"state,omitempty"`
	ZipCode      string `json:"zip_code,omitempty"`
}

// ScreenResult is step 4 output.
type ScreenResult struct {
	AppID     string  `json:"app_id"`
	AMLStatus string  `json:"aml_status"`
	KYCStatus string  `json:"kyc_status"`
	RiskLevel string  `json:"risk_level"`
	RiskScore float64 `json:"risk_score"`
}

// Create starts a new onboarding application (step 1).
func (o *OnboardingClient) Create(ctx context.Context, req CreateApplicationReq) (*Application, error) {
	raw, _, err := o.c.doWithIdem(ctx, "POST", o.c.bdURL("/v1/bd/onboarding"), req, "")
	if err != nil {
		return nil, err
	}
	var app Application
	if err := json.Unmarshal(raw, &app); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &app, nil
}

// Get returns the current status of an application.
func (o *OnboardingClient) Get(ctx context.Context, appID string) (*Application, error) {
	raw, _, err := o.c.do(ctx, "GET", o.c.bdURL("/v1/bd/onboarding/"+appID), nil)
	if err != nil {
		return nil, err
	}
	var app Application
	if err := json.Unmarshal(raw, &app); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &app, nil
}

// SubmitIdentity submits identity info (step 2).
func (o *OnboardingClient) SubmitIdentity(ctx context.Context, appID string, req IdentityReq) (*Application, error) {
	raw, _, err := o.c.doWithIdem(ctx, "POST", o.c.bdURL("/v1/bd/onboarding/"+appID+"/identity"), req, "")
	if err != nil {
		return nil, err
	}
	var app Application
	if err := json.Unmarshal(raw, &app); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &app, nil
}

// Screen runs compliance screening (step 4).
func (o *OnboardingClient) Screen(ctx context.Context, appID string) (*ScreenResult, error) {
	raw, _, err := o.c.doWithIdem(ctx, "POST", o.c.bdURL("/v1/bd/onboarding/"+appID+"/screen"), nil, "")
	if err != nil {
		return nil, err
	}
	var result ScreenResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &result, nil
}

// Submit finalizes and submits the application (step 5).
func (o *OnboardingClient) Submit(ctx context.Context, appID string) (*Application, error) {
	raw, _, err := o.c.doWithIdem(ctx, "POST", o.c.bdURL("/v1/bd/onboarding/"+appID+"/submit"), nil, "")
	if err != nil {
		return nil, err
	}
	var app Application
	if err := json.Unmarshal(raw, &app); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &app, nil
}

// ImportJob is the response from a bulk import.
type ImportJob struct {
	ImportID  string `json:"import_id"`
	Status    string `json:"status"`
	Count     int    `json:"count"`
	Processed int    `json:"processed"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
}

// ImportZip uploads a zip file for bulk onboarding.
func (o *OnboardingClient) ImportZip(ctx context.Context, zipPath string) (*ImportJob, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("zip", filepath.Base(zipPath))
	if err != nil {
		return nil, fmt.Errorf("create form: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("copy: %w", err)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, "POST",
		o.c.bdURL("/v1/bd/onboarding-imports"), &buf)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if o.c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+o.c.cfg.Token)
	}
	if o.c.cfg.OrgID != "" {
		req.Header.Set("X-Org-Id", o.c.cfg.OrgID)
	}

	resp, err := o.c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var job ImportJob
	if err := json.Unmarshal(body, &job); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &job, nil
}

// ImportStatus returns the status of a bulk import.
func (o *OnboardingClient) ImportStatus(ctx context.Context, importID string) (*ImportJob, error) {
	raw, _, err := o.c.do(ctx, "GET", o.c.bdURL("/v1/bd/onboarding-imports/"+importID), nil)
	if err != nil {
		return nil, err
	}
	var job ImportJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &job, nil
}

// DownloadImportResult downloads the result zip for a completed import.
func (o *OnboardingClient) DownloadImportResult(ctx context.Context, importID, outPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET",
		o.c.bdURL("/v1/bd/onboarding-imports/"+importID+"/result.zip"), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if o.c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+o.c.cfg.Token)
	}
	if o.c.cfg.OrgID != "" {
		req.Header.Set("X-Org-Id", o.c.cfg.OrgID)
	}

	resp, err := o.c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
