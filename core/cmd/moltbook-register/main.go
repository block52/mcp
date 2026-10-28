// Command moltbook-register performs the ONE machine step of moltbook agent
// onboarding: POST /api/v1/agents/register. It prints the api_key and the claim
// URL, then the human-only steps (email + X/Twitter verification) that moltbook
// requires before the key goes live. This cannot be fully automated — moltbook
// requires a verified human owner by design.
//
// Usage:
//
//	go run ./cmd/moltbook-register -name "MyAgent" -desc "plays poker on block52"
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	name := flag.String("name", "", "agent name (required)")
	desc := flag.String("desc", "", "short agent description (required)")
	baseURL := flag.String("base-url", "https://www.moltbook.com", "moltbook base URL")
	flag.Parse()

	if *name == "" || *desc == "" {
		flag.Usage()
		os.Exit(2)
	}

	body, _ := json.Marshal(map[string]string{"name": *name, "description": *desc})
	req, err := http.NewRequest(http.MethodPost, *baseURL+"/api/v1/agents/register", bytes.NewReader(body))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		log.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		log.Fatalf("register: status %d: %s", resp.StatusCode, string(raw))
	}

	var out struct {
		APIKey   string `json:"api_key"`
		ClaimURL string `json:"claim_url"`
		Code     string `json:"verification_code"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		log.Fatalf("decode response: %v (body: %s)", err, string(raw))
	}

	fmt.Println("Registered. SAVE YOUR API KEY NOW — it is shown only once.")
	fmt.Println()
	fmt.Printf("  MOLTBOOK_API_KEY=%s\n", out.APIKey)
	fmt.Println()
	fmt.Println("Next (human-only — moltbook requires a verified owner):")
	fmt.Printf("  1. Open the claim URL and verify your email: %s\n", out.ClaimURL)
	fmt.Printf("  2. Verify ownership on X/Twitter (code: %s).\n", out.Code)
	fmt.Println("  3. Then set MOLTBOOK_API_KEY in the agentd environment.")
}
