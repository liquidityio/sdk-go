// liqctl — Liquidity platform CLI.
//
// Uses the Go SDK to call the canonical onboarding pipeline.
// Designed to be invoked standalone or composed into the Base CLI
// as `base user` subcommands via the extension registry.
//
// Usage:
//
//	liqctl user create --email z@liquidity.io --org mlc --first-name Z --last-name Kay
//	liqctl user onboard --app-id <id> --step identity --data '{"date_of_birth":"1990-01-01"}'
//	liqctl user onboard --app-id <id> --step document --file ./dl.pdf --type drivers_license
//	liqctl user onboard --app-id <id> --submit
//	liqctl user seed --from ~/work/liquidity/state/devnet/users.json --env dev
//	liqctl key rotate --org <slug> [--all-orgs] [--dry-run]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	sdk "github.com/liquidityio/sdk-go"
)

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(1)
	}

	resource := os.Args[1]
	action := os.Args[2]

	switch resource {
	case "user":
		c := sdk.NewClient(sdk.Config{})
		ctx := context.Background()
		switch action {
		case "create":
			cmdCreate(ctx, c, os.Args[3:])
		case "onboard":
			cmdOnboard(ctx, c, os.Args[3:])
		case "status":
			cmdStatus(ctx, c, os.Args[3:])
		case "seed":
			cmdSeed(ctx, c, os.Args[3:])
		case "import":
			cmdImport(ctx, c, os.Args[3:])
		case "import-status":
			cmdImportStatus(ctx, c, os.Args[3:])
		case "import-result":
			cmdImportResult(ctx, c, os.Args[3:])
		case "export":
			cmdExport(ctx, c, os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown user action: %s\n", action)
			usage()
			os.Exit(1)
		}
	case "key":
		switch action {
		case "rotate":
			cmdKeyRotate(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown key action: %s\n", action)
			usage()
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown resource: %s\n", resource)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: liqctl <resource> <action> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "user create         --email <email> --first-name <name> --last-name <name> [--phone <phone>]")
	fmt.Fprintln(os.Stderr, "user onboard        --app-id <id> --step <identity|document|screen|submit> [--data <json>]")
	fmt.Fprintln(os.Stderr, "user status         --app-id <id>")
	fmt.Fprintln(os.Stderr, "user seed           --from <users.json> --env <dev|test|main>")
	fmt.Fprintln(os.Stderr, "user import         --zip <users.zip> --env <dev|test|main>")
	fmt.Fprintln(os.Stderr, "user import-status  <import_id>")
	fmt.Fprintln(os.Stderr, "user import-result  <import_id> --out <results.zip>")
	fmt.Fprintln(os.Stderr, "user export         <user-id-or-email> --out <export.zip> [--persist]")
	fmt.Fprintln(os.Stderr, "key  rotate         --org <slug> [--all-orgs] [--dry-run] [--apply]")
	fmt.Fprintln(os.Stderr, "                    Print rotation plan and run preflight checks. See")
	fmt.Fprintln(os.Stderr, "                    universe/docs/internal/runbooks/key-rotation.md")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Environment: LIQUIDITY_ENV, LIQUIDITY_TOKEN, LIQUIDITY_ORG_ID, BD_URL, TA_URL, ATS_URL,")
	fmt.Fprintln(os.Stderr, "             KMS_URL, KMS_TOKEN")
}

func cmdCreate(ctx context.Context, c *sdk.Client, args []string) {
	flags := parseFlags(args)
	email := flags["email"]
	firstName := flags["first-name"]
	lastName := flags["last-name"]
	phone := flags["phone"]
	country := flags["country"]

	if email == "" || firstName == "" || lastName == "" {
		fmt.Fprintln(os.Stderr, "error: --email, --first-name, --last-name are required")
		os.Exit(1)
	}

	app, err := c.Onboarding.Create(ctx, sdk.CreateApplicationReq{
		Email:     email,
		FirstName: firstName,
		LastName:  lastName,
		Phone:     phone,
		Country:   country,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printJSON(app)
}

func cmdOnboard(ctx context.Context, c *sdk.Client, args []string) {
	flags := parseFlags(args)
	appID := flags["app-id"]
	step := flags["step"]

	if appID == "" {
		fmt.Fprintln(os.Stderr, "error: --app-id is required")
		os.Exit(1)
	}

	// --submit shortcut (step 5).
	if _, ok := flags["submit"]; ok || step == "submit" {
		app, err := c.Onboarding.Submit(ctx, appID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(app)
		return
	}

	switch step {
	case "identity":
		data := flags["data"]
		if data == "" {
			fmt.Fprintln(os.Stderr, "error: --data is required for identity step")
			os.Exit(1)
		}
		var req sdk.IdentityReq
		if err := json.Unmarshal([]byte(data), &req); err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid --data JSON: %v\n", err)
			os.Exit(1)
		}
		app, err := c.Onboarding.SubmitIdentity(ctx, appID, req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(app)

	case "document":
		filePath := flags["file"]
		docType := flags["type"]
		if filePath == "" || docType == "" {
			fmt.Fprintln(os.Stderr, "error: --file and --type are required for document step")
			os.Exit(1)
		}
		f, err := os.Open(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		doc, err := c.Documents.Upload(ctx, appID, docType, filePath, f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(doc)

	case "screen":
		result, err := c.Onboarding.Screen(ctx, appID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(result)

	default:
		fmt.Fprintf(os.Stderr, "error: unknown step %q (identity, document, screen, submit)\n", step)
		os.Exit(1)
	}
}

func cmdStatus(ctx context.Context, c *sdk.Client, args []string) {
	flags := parseFlags(args)
	appID := flags["app-id"]
	if appID == "" {
		fmt.Fprintln(os.Stderr, "error: --app-id is required")
		os.Exit(1)
	}
	app, err := c.Onboarding.Get(ctx, appID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	printJSON(app)
}

type seedUser struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Phone       string `json:"phone"`
}

func cmdSeed(ctx context.Context, c *sdk.Client, args []string) {
	flags := parseFlags(args)
	fromFile := flags["from"]
	if fromFile == "" {
		fmt.Fprintln(os.Stderr, "error: --from is required")
		os.Exit(1)
	}

	data, err := os.ReadFile(fromFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", fromFile, err)
		os.Exit(1)
	}

	var users []seedUser
	if err := json.Unmarshal(data, &users); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing %s: %v\n", fromFile, err)
		os.Exit(1)
	}

	fmt.Printf("Seeding %d users...\n", len(users))
	created, skipped, failed := 0, 0, 0

	for i, u := range users {
		firstName := u.FirstName
		lastName := u.LastName
		if firstName == "" && u.DisplayName != "" {
			parts := strings.SplitN(u.DisplayName, " ", 2)
			firstName = parts[0]
			if len(parts) > 1 {
				lastName = parts[1]
			}
		}

		app, err := c.Onboarding.Create(ctx, sdk.CreateApplicationReq{
			Email:     u.Email,
			FirstName: firstName,
			LastName:  lastName,
			Phone:     u.Phone,
		})
		if err != nil {
			fmt.Printf("[%d/%d] FAIL %s: %v\n", i+1, len(users), u.Email, err)
			failed++
			continue
		}

		// Run through remaining steps.
		_, _ = c.Onboarding.SubmitIdentity(ctx, app.ID, sdk.IdentityReq{
			DateOfBirth: "1990-01-01",
		})
		_, _ = c.Onboarding.Screen(ctx, app.ID)
		result, submitErr := c.Onboarding.Submit(ctx, app.ID)
		if submitErr != nil {
			fmt.Printf("[%d/%d] PARTIAL %s (app=%s): submit failed: %v\n", i+1, len(users), u.Email, app.ID, submitErr)
			failed++
			continue
		}

		fmt.Printf("[%d/%d] OK %s (app=%s, status=%s)\n", i+1, len(users), u.Email, result.ID, result.Status)
		created++
	}

	fmt.Printf("\n--- Summary ---\n")
	fmt.Printf("Total:   %d\n", len(users))
	fmt.Printf("Created: %d\n", created)
	fmt.Printf("Skipped: %d\n", skipped)
	fmt.Printf("Failed:  %d\n", failed)
}

// parseFlags is a simple --key value parser. Bare --flag sets key="true".
func parseFlags(args []string) map[string]string {
	m := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				m[key] = args[i+1]
				i++
			} else {
				m[key] = "true"
			}
		}
	}
	return m
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func cmdImport(ctx context.Context, c *sdk.Client, args []string) {
	flags := parseFlags(args)
	zipPath := flags["zip"]
	if zipPath == "" {
		fmt.Fprintln(os.Stderr, "error: --zip is required")
		os.Exit(1)
	}

	job, err := c.Onboarding.ImportZip(ctx, zipPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Import started: %s (%d users)\n", job.ImportID, job.Count)

	// Poll until done.
	for job.Status == "processing" {
		time.Sleep(2 * time.Second)
		job, err = c.Onboarding.ImportStatus(ctx, job.ImportID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error polling: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  [%d/%d] succeeded=%d failed=%d\n",
			job.Processed, job.Count, job.Succeeded, job.Failed)
	}

	fmt.Printf("\nImport %s: %d succeeded, %d failed\n", job.Status, job.Succeeded, job.Failed)
}

func cmdImportStatus(ctx context.Context, c *sdk.Client, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: import_id required")
		os.Exit(1)
	}
	importID := args[0]

	job, err := c.Onboarding.ImportStatus(ctx, importID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	printJSON(job)
}

func cmdImportResult(ctx context.Context, c *sdk.Client, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: import_id required")
		os.Exit(1)
	}
	importID := args[0]
	flags := parseFlags(args[1:])
	outPath := flags["out"]
	if outPath == "" {
		outPath = "import-" + importID + "-results.zip"
	}

	if err := c.Onboarding.DownloadImportResult(ctx, importID, outPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Downloaded to %s\n", outPath)
}

func cmdExport(ctx context.Context, c *sdk.Client, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: user-id-or-email required")
		os.Exit(1)
	}
	userID := args[0]
	flags := parseFlags(args[1:])
	outPath := flags["out"]
	if outPath == "" {
		outPath = "user-export-" + userID + ".zip"
	}
	persist := flags["persist"] == "true"

	if err := c.Users.Export(ctx, userID, outPath, persist); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Exported to %s\n", outPath)
}
