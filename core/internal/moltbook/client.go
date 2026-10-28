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
	id, err := c.submit(ctx, body)
	if err != nil {
		return PostResult{}, err
	}

	c.mu.Lock()
	c.lastPost = c.now()
	c.mu.Unlock()
	return PostResult{PostID: id}, nil
}

// postResponse models moltbook's /api/v1/posts reply across its real shapes
// (confirmed against the live API):
//
//	success:  {"success": true, "post": {"id": "<uuid>", ...}}  (id may also be top-level)
//	error:    {"statusCode": 429, "message": "...", "hint": "...", "retry_after_seconds": N}
//	verify:   {"verification_required": true, "verification": {"verification_code","challenge_text","expires_at"}}
//
// A post is published only when an id is present. verification_required means it
// is HELD pending a separate POST /api/v1/verify with the solved challenge.
type postResponse struct {
	Success bool `json:"success"`
	// id can arrive top-level or nested under "post".
	ID   string `json:"id"`
	Post *struct {
		ID string `json:"id"`
	} `json:"post"`

	VerificationRequired bool `json:"verification_required"`
	Verification         *struct {
		Code      string `json:"verification_code"`
		Challenge string `json:"challenge_text"`
	} `json:"verification"`

	// error shape (also present on 4xx/5xx bodies).
	Message string `json:"message"`
	Hint    string `json:"hint"`
	ErrorV  string `json:"error"` // older/alternate error field
}

func (p *postResponse) postID() string {
	if p.ID != "" {
		return p.ID
	}
	if p.Post != nil {
		return p.Post.ID
	}
	return ""
}

// errText builds a terse, key-free error string from whatever the body carried.
func (p *postResponse) errText() string {
	switch {
	case p.Message != "":
		if p.Hint != "" {
			return p.Message + " (" + p.Hint + ")"
		}
		return p.Message
	case p.ErrorV != "":
		return p.ErrorV
	default:
		return ""
	}
}

// submit POSTs to /api/v1/posts and returns the published post id (which may be
// empty — moltbook confirms a create with a message rather than always echoing an
// id). HTTP STATUS is authoritative: `message` appears on BOTH success
// ("Post created! 🦞") and error ("You can only post…"), so it must never be read
// as an error on a 2xx. A verification_required hold is surfaced explicitly (the
// separate /verify word-problem flow is not implemented — see README).
func (c *Client) submit(ctx context.Context, body map[string]string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/posts", bytes.NewReader(mustJSON(body)))
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

	var pr postResponse
	// Best-effort decode; a non-JSON body still yields a clean status error below.
	_ = json.Unmarshal(data, &pr)

	// Status is the source of truth. message/hint explain a failure here.
	if resp.StatusCode >= 400 {
		if msg := pr.errText(); msg != "" {
			return "", fmt.Errorf("moltbook post: status %d: %s", resp.StatusCode, msg)
		}
		return "", fmt.Errorf("moltbook post: status %d", resp.StatusCode)
	}

	// 2xx. A hold still means NOT published.
	if pr.VerificationRequired {
		return "", fmt.Errorf("moltbook post: held for verification (manual /verify required; challenge not auto-solvable)")
	}

	// Published. id may be present (nested or top-level) or absent (moltbook
	// confirmed via message). Either way a 2xx non-hold is a successful post.
	return pr.postID(), nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
