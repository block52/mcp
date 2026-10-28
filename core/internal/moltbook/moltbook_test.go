package moltbook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSolveChallenge(t *testing.T) {
	cases := map[string]string{
		"3 + 4":   "7",
		"10 - 2":  "8",
		"6 * 7":   "42",
		"6 x 7":   "42",
		"2 + 2 =": "4",
	}
	for q, want := range cases {
		got, err := solveChallenge(q)
		if err != nil {
			t.Fatalf("solveChallenge(%q): %v", q, err)
		}
		if got != want {
			t.Fatalf("solveChallenge(%q) = %q, want %q", q, got, want)
		}
	}
	if _, err := solveChallenge("what color is the sky"); err == nil {
		t.Fatal("expected error for non-arithmetic challenge")
	}
}

func TestPostDisabledWithoutKey(t *testing.T) {
	c := New(Config{})
	if c.Enabled() {
		t.Fatal("should be disabled without an API key")
	}
	if _, err := c.Post(context.Background(), "t", "c", ""); err == nil {
		t.Fatal("expected error when disabled")
	}
}

// moltbook's real success shape: {"success": true, "post": {"id": "<uuid>"}}.
func writeSuccess(w http.ResponseWriter, id string) {
	_, _ = w.Write([]byte(`{"success":true,"post":{"id":"` + id + `"}}`))
}

func TestPostRateLimitSkips(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writeSuccess(w, "p1")
	}))
	defer srv.Close()

	now := time.Unix(1_700_000_000, 0)
	c := New(Config{APIKey: "k", BaseURL: srv.URL, Now: func() time.Time { return now }})

	// First post sends and returns the nested post.id.
	r1, err := c.Post(context.Background(), "t1", "c1", "")
	if err != nil || r1.Skipped || r1.PostID != "p1" {
		t.Fatalf("first post: %+v err=%v", r1, err)
	}
	// Second post 10 min later is inside the 30-min window -> skipped, no HTTP.
	now = now.Add(10 * time.Minute)
	r2, err := c.Post(context.Background(), "t2", "c2", "")
	if err != nil || !r2.Skipped {
		t.Fatalf("second post should skip: %+v err=%v", r2, err)
	}
	// 31 min after the FIRST post the window has elapsed -> sends again.
	now = now.Add(21 * time.Minute)
	r3, err := c.Post(context.Background(), "t3", "c3", "")
	if err != nil || r3.Skipped {
		t.Fatalf("third post should send: %+v err=%v", r3, err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 HTTP calls (skip made none), got %d", calls)
	}
}

func TestPostAcceptsTopLevelID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"id":"top-level-id"}`))
	}))
	defer srv.Close()
	c := New(Config{APIKey: "k", BaseURL: srv.URL, Now: func() time.Time { return time.Unix(1, 0) }})
	r, err := c.Post(context.Background(), "t", "c", "")
	if err != nil || r.PostID != "top-level-id" {
		t.Fatalf("top-level id: %+v err=%v", r, err)
	}
}

// The bug this fix targets: moltbook confirms a create with a MESSAGE on a 2xx
// ("Post created! 🦞"), often without echoing an id. That must read as SUCCESS
// (empty id ok), NOT as an error — status is authoritative, message is not.
func TestPostSuccessMessageNoIDIsNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"message":"Post created! 🦞"}`)) // 2xx, message, no id
	}))
	defer srv.Close()
	c := New(Config{APIKey: "k", BaseURL: srv.URL, Now: func() time.Time { return time.Unix(1, 0) }})
	r, err := c.Post(context.Background(), "t", "c", "")
	if err != nil {
		t.Fatalf("2xx with a success message must not error: %v", err)
	}
	if r.Skipped {
		t.Fatal("should not be skipped")
	}
	// id legitimately empty here — the post still succeeded.
}

// verification_required must be surfaced as an error, not faked as published.
func TestPostVerificationRequiredSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"verification_required":true,"verification":{"verification_code":"mv_x","challenge_text":"a word problem"}}`))
	}))
	defer srv.Close()
	c := New(Config{APIKey: "k", BaseURL: srv.URL, Now: func() time.Time { return time.Unix(1, 0) }})
	_, err := c.Post(context.Background(), "t", "c", "")
	if err == nil || !strings.Contains(err.Error(), "verification") {
		t.Fatalf("expected a verification error, got %v", err)
	}
}

// moltbook's 429 shape: {statusCode, message, hint, retry_after_seconds}.
func TestPostRateLimitedByServerSurfacesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"statusCode":429,"message":"You can only post once every 2.5 minutes","hint":"Wait 138 seconds"}`))
	}))
	defer srv.Close()
	c := New(Config{APIKey: "k", BaseURL: srv.URL, Now: func() time.Time { return time.Unix(1, 0) }})
	_, err := c.Post(context.Background(), "t", "c", "")
	if err == nil || !strings.Contains(err.Error(), "once every 2.5 minutes") {
		t.Fatalf("expected the server's rate-limit message, got %v", err)
	}
}

func TestPostErrorKeepsBodyKeyFree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	c := New(Config{APIKey: "super-secret-key", BaseURL: srv.URL, Now: func() time.Time { return time.Unix(1, 0) }})
	_, err := c.Post(context.Background(), "t", "c", "")
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if strings.Contains(err.Error(), "super-secret-key") {
		t.Fatal("error message leaked the API key")
	}
}
