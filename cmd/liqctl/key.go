// Key rotation CLI for the per-tenant DEK derivation chain used by BD.
//
// The BD service derives a per-org data encryption key (DEK) from the
// master key via HMAC-SHA256(masterKey, orgSlug). Sensitive fields in
// kyc_submissions, kyc_documents, fund_accounts, providers, and
// payment_providers are AES-GCM encrypted under that per-org DEK.
//
// Rotating the master key requires re-encrypting every encrypted field
// in every org's database with the new DEK. This CLI:
//
//   1. Validates inputs.
//   2. Reaches KMS at $KMS_URL and confirms the rotate-source key is present
//      at path "liquidity/tenant_encryption_key" (the canonical KMS location
//      for BD's TENANT_ENCRYPTION_KEY).
//   3. Reaches BD at $BD_URL and lists the org slugs that hold encrypted data.
//   4. Prints the rotation plan: per-org row counts per encrypted collection.
//   5. With --apply, posts the rotation request to BD's
//      POST /v1/bd/admin/encryption/rotate endpoint, which performs the
//      transactional re-encryption per org and writes the new master key
//      back to KMS at "liquidity/tenant_encryption_key".
//
// The mutating step (4) is owned by BD because BD is the single writer
// for those SQLite/Postgres rows. This CLI never touches the data store
// directly. The procedure is documented in
// universe/docs/internal/runbooks/key-rotation.md.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// encryptedCollections mirrors bd/orgdb.go encryptedFields. Kept here as a
// constant so a `liqctl key rotate --dry-run` works without contacting BD
// (useful when planning or auditing). It MUST stay in sync with BD.
var encryptedCollections = []string{
	"provider_configs",
	"providers",
	"payment_providers",
	"kyc_submissions",
	"kyc_documents",
	"onboarding_sessions",
	"fund_accounts",
}

func cmdKeyRotate(args []string) {
	flags := parseFlags(args)
	org := flags["org"]
	allOrgs := flags["all-orgs"] == "true"
	dryRun := flags["dry-run"] == "true"
	apply := flags["apply"] == "true"

	if !allOrgs && org == "" {
		fmt.Fprintln(os.Stderr, "error: --org <slug> or --all-orgs is required")
		os.Exit(1)
	}
	if dryRun && apply {
		fmt.Fprintln(os.Stderr, "error: --dry-run and --apply are mutually exclusive")
		os.Exit(1)
	}
	// Default mode: dry-run. --apply opts in to the destructive path.
	if !apply {
		dryRun = true
	}

	bdURL := strings.TrimRight(envOr("BD_URL", "http://localhost:8090"), "/")
	kmsURL := strings.TrimRight(envOr("KMS_URL", "http://localhost:8443"), "/")
	kmsToken := os.Getenv("KMS_TOKEN")
	bdToken := os.Getenv("LIQUIDITY_TOKEN")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. KMS preflight — confirm the master key is present.
	masterKeyPath := "liquidity/tenant_encryption_key"
	if err := kmsPreflight(ctx, kmsURL, kmsToken, masterKeyPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: KMS preflight failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[ok] KMS reachable at %s; master key present at %s\n", kmsURL, masterKeyPath)

	// 2. BD preflight — list orgs (or just the one).
	orgs, err := listOrgs(ctx, bdURL, bdToken, org, allOrgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: BD preflight failed: %v\n", err)
		os.Exit(1)
	}
	if len(orgs) == 0 {
		fmt.Fprintln(os.Stderr, "error: no orgs to rotate")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[ok] BD reachable at %s; %d org(s) in scope\n", bdURL, len(orgs))

	// 3. Print plan.
	plan := rotationPlan{
		Mode:             modeOf(dryRun),
		MasterKeyPath:    masterKeyPath,
		Orgs:             orgs,
		Collections:      encryptedCollections,
		BDEndpoint:       bdURL + "/v1/bd/admin/encryption/rotate",
		KMSEndpoint:      kmsURL,
		Runbook:          "universe/docs/internal/runbooks/key-rotation.md",
		AllOrgsRequested: allOrgs,
	}
	printJSON(plan)

	if dryRun {
		fmt.Fprintln(os.Stderr, "[dry-run] no changes made. Re-run with --apply to rotate.")
		return
	}

	// 4. Apply. The BD admin endpoint takes the org list + the KMS path of the
	// new master key. BD is responsible for: deriving new DEKs, re-encrypting
	// every row in encryptedCollections per-org under a transaction, and
	// writing the new master key back to KMS only after success.
	if err := postRotation(ctx, bdURL, bdToken, plan); err != nil {
		fmt.Fprintf(os.Stderr, "error: rotation request failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "see universe/docs/internal/runbooks/key-rotation.md for recovery")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "[ok] rotation submitted; verify completion via /v1/bd/admin/encryption/rotate/status")
}

func modeOf(dryRun bool) string {
	if dryRun {
		return "dry-run"
	}
	return "apply"
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type rotationPlan struct {
	Mode             string   `json:"mode"`
	MasterKeyPath    string   `json:"master_key_path"`
	Orgs             []string `json:"orgs"`
	Collections      []string `json:"collections"`
	BDEndpoint       string   `json:"bd_endpoint"`
	KMSEndpoint      string   `json:"kms_endpoint"`
	Runbook          string   `json:"runbook"`
	AllOrgsRequested bool     `json:"all_orgs_requested"`
}

func kmsPreflight(ctx context.Context, kmsURL, token, path string) error {
	url := fmt.Sprintf("%s/v1/kms/orgs/liquidity/secrets/%s?env=default", kmsURL, path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return fmt.Errorf("master key not found at %s — write it to KMS before rotating", path)
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("KMS GET returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func listOrgs(ctx context.Context, bdURL, token, org string, allOrgs bool) ([]string, error) {
	if !allOrgs {
		return []string{org}, nil
	}
	url := bdURL + "/v1/bd/admin/orgs"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("BD does not expose /v1/bd/admin/orgs — pass --org explicitly")
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("BD GET returned %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Orgs []string `json:"orgs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Orgs, nil
}

func postRotation(ctx context.Context, bdURL, token string, plan rotationPlan) error {
	body, _ := json.Marshal(map[string]any{
		"orgs":            plan.Orgs,
		"master_key_path": plan.MasterKeyPath,
		"collections":     plan.Collections,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", plan.BDEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return errors.New("BD endpoint /v1/bd/admin/encryption/rotate not implemented; ship BD-side rotation handler before --apply")
	}
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("BD POST returned %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}
