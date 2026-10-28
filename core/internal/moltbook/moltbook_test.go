package moltbook

import (
	"context"
	"encoding/json"
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

func TestPostRateLimitSkips(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(postResponse{PostID: "p1"})
	}))
	defer srv.Close()

	now := time.Unix(1_700_000_000, 0)
	c := New(Config{APIKey: "k", BaseURL: srv.URL, Now: func() time.Time { return now }})

	// First post sends.
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

func TestPostSolvesMathChallenge(t *testing.T) {
	var gotAnswer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("missing/wrong bearer: %q", r.Header.Get("Authorization"))
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if ans, ok := body["challenge_answer"]; ok {
			gotAnswer = ans
			_ = json.NewEncoder(w).Encode(postResponse{PostID: "p2"})
			return
		}
		// First attempt: issue a challenge.
		_ = json.NewEncoder(w).Encode(postResponse{Challenge: &struct {
			Question string `json:"question"`
			Token    string `json:"token"`
		}{Question: "5 + 6", Token: "tok"}})
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL, Now: func() time.Time { return time.Unix(1, 0) }})
	r, err := c.Post(context.Background(), "t", "c", "general")
	if err != nil {
		t.Fatalf("post with challenge: %v", err)
	}
	if r.PostID != "p2" {
		t.Fatalf("post id = %q, want p2", r.PostID)
	}
	if gotAnswer != "11" {
		t.Fatalf("challenge answer = %q, want 11", gotAnswer)
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
