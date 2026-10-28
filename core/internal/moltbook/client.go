// Package moltbook posts an agent's game narration to moltbook.com, "a social
// network for AI agents". It is best-effort and OFF the gameplay critical path.
//
// Constraints that shape this client (from moltbook.com/skill.md):
//   - Auth: Authorization: Bearer <api_key>. The key is a secret that only ever
//     goes to moltbook.com — it is NEVER logged here.
//   - Rate limit: 1 post / 30 min. We gate locally and SKIP (not queue, not
//     block) a post inside the window, so gameplay is never stalled.
//   - A post may return a math challenge that must be solved and resubmitted.
//
// Account registration (POST /agents/register) is a one-time human-in-the-loop
// step (email + X verification) — see cmd/moltbook-register, not this client.
package moltbook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://www.moltbook.com"
	postInterval   = 30 * time.Minute // moltbook: 1 post / 30 min
	defaultSubmolt = "general"
)

// Client posts to moltbook. Construct via New.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
	now     func() time.Time // injectable clock (tests)

	mu       sync.Mutex
	lastPost time.Time // zero = never posted
}

// Config configures the client. Now/BaseURL default when zero (production).
type Config struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
	Now     func() time.Time
}

func New(cfg Config) *Client {
	c := &Client{
		apiKey:  cfg.APIKey,
		baseURL: cfg.BaseURL,
		http:    cfg.HTTP,
		now:     cfg.Now,
	}
	if c.baseURL == "" {
		c.baseURL = defaultBaseURL
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 15 * time.Second}
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// Enabled reports whether an API key is configured.
func (c *Client) Enabled() bool { return c.apiKey != "" }

// PostResult is the outcome of Post. Skipped means the rate-limit window had not
// elapsed and nothing was sent — not an error.
type PostResult struct {
	PostID  string
	Skipped bool
}

// Post publishes a post, respecting the 30-min rate limit. If the window has not
// elapsed since the last successful post, it returns Skipped=true WITHOUT making
// a network call. A math challenge in the response is solved and resubmitted once.
func (c *Client) Post(ctx context.Context, title, content, submolt string) (PostResult, error) {
	if !c.Enabled() {
		return PostResult{}, fmt.Errorf("moltbook disabled: no MOLTBOOK_API_KEY")
	}
	if submolt == "" {
		submolt = defaultSubmolt
	}

	// Rate-limit gate: skip (don't block, don't queue) inside the window.
	c.mu.Lock()
	if !c.lastPost.IsZero() && c.now().Sub(c.lastPost) < postInterval {
		c.mu.Unlock()
		return PostResult{Skipped: true}, nil
	}
	c.mu.Unlock()

	body := map[string]string{"submolt_name": submolt, "title": title, "content": content}
	id, err := c.submit(ctx, body, false)
	if err != nil {
		return PostResult{}, err
	}

	c.mu.Lock()
	c.lastPost = c.now()
	c.mu.Unlock()
	return PostResult{PostID: id}, nil
}

// postResponse is moltbook's post reply. A non-empty Challenge means the post was
// NOT published and must be resubmitted with the solved answer.
type postResponse struct {
	PostID    string `json:"post_id"`
	ID        string `json:"id"` // some responses use "id"
	Challenge *struct {
		Question string `json:"question"` // e.g. "3 + 4"
		Token    string `json:"token"`    // opaque, echoed back with the answer
	} `json:"challenge"`
	Error string `json:"error"`
}

// submit POSTs to /api/v1/posts, solving a math challenge once if asked.
func (c *Client) submit(ctx context.Context, body map[string]string, isRetry bool) (string, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/posts", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("moltbook post: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		// Do not echo the response body unbounded — it could contain nothing
		// sensitive, but keep errors terse and key-free.
		return "", fmt.Errorf("moltbook post: status %d", resp.StatusCode)
	}

	var pr postResponse
	if err := json.Unmarshal(data, &pr); err != nil {
		return "", fmt.Errorf("moltbook post: decode response: %w", err)
	}
	if pr.Error != "" {
		return "", fmt.Errorf("moltbook post: %s", pr.Error)
	}

	if pr.Challenge != nil {
		if isRetry {
			return "", fmt.Errorf("moltbook post: challenge not accepted after solving")
		}
		answer, err := solveChallenge(pr.Challenge.Question)
		if err != nil {
			return "", fmt.Errorf("moltbook post: %w", err)
		}
		next := map[string]string{}
		for k, v := range body {
			next[k] = v
		}
		next["challenge_token"] = pr.Challenge.Token
		next["challenge_answer"] = answer
		return c.submit(ctx, next, true)
	}

	if pr.PostID != "" {
		return pr.PostID, nil
	}
	return pr.ID, nil
}
