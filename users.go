package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// UsersClient wraps user-level BD endpoints.
type UsersClient struct {
	c *Client
}

// Export downloads the user export zip.
func (u *UsersClient) Export(ctx context.Context, userID, outPath string, persist bool) error {
	var url string
	var method string
	if persist {
		url = u.c.bdURL("/v1/bd/users/" + userID + "/export")
		method = "POST"
	} else {
		url = u.c.bdURL("/v1/bd/users/" + userID + "/export.zip")
		method = "GET"
	}

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if u.c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+u.c.cfg.Token)
	}
	if u.c.cfg.OrgID != "" {
		req.Header.Set("X-Org-Id", u.c.cfg.OrgID)
	}

	resp, err := u.c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	if persist {
		// Persistent: response is JSON with file_id.
		var result struct {
			FileID    string `json:"file_id"`
			ExpiresAt string `json:"expires_at"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
		fmt.Printf("Persisted export: file_id=%s expires=%s\n", result.FileID, result.ExpiresAt)
		return nil
	}

	// Stream: write zip to file.
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// ExportPersistent creates a persistent export and returns the file ID.
func (u *UsersClient) ExportPersistent(ctx context.Context, userID string) (string, error) {
	raw, _, err := u.c.doWithIdem(ctx, "POST", u.c.bdURL("/v1/bd/users/"+userID+"/export"), map[string]any{"persist": true}, "")
	if err != nil {
		return "", err
	}
	var result struct {
		FileID string `json:"file_id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return result.FileID, nil
}
