package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
)

// DocumentsClient wraps document upload endpoints.
type DocumentsClient struct {
	c *Client
}

// DocumentUpload is the response from a document upload.
type DocumentUpload struct {
	ID           string `json:"id"`
	DocumentType string `json:"documentType"`
	FileName     string `json:"fileName"`
	S3Key        string `json:"s3Key"`
	FileSize     int64  `json:"fileSize"`
	UploadedAt   string `json:"uploadedAt"`
}

// Upload uploads a document for an onboarding application (step 3).
func (d *DocumentsClient) Upload(ctx context.Context, appID, docType, filename string, file io.Reader) (*DocumentUpload, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	w.WriteField("sessionId", appID)
	w.WriteField("type", docType)

	part, err := w.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("copy file: %w", err)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, "POST",
		d.c.bdURL("/v1/bd/onboarding/"+appID+"/documents"), &buf)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if d.c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+d.c.cfg.Token)
	}
	if d.c.cfg.OrgID != "" {
		req.Header.Set("X-Org-Id", d.c.cfg.OrgID)
	}

	resp, err := d.c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var doc DocumentUpload
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &doc, nil
}

// List returns documents uploaded for an application.
func (d *DocumentsClient) List(ctx context.Context, appID string) ([]DocumentUpload, error) {
	raw, _, err := d.c.do(ctx, "GET", d.c.bdURL("/v1/bd/onboarding/"+appID+"/documents"), nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Items []DocumentUpload `json:"items"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return result.Items, nil
}
