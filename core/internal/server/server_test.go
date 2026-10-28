package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agentv1 "github.com/block52/mcp/core/gen/agent/v1"
	"github.com/block52/mcp/core/internal/chain"
	"github.com/block52/mcp/core/internal/moltbook"
)

// fakeChain serves the same doubly-encoded shapes the real pokerchain does, so
// the read handlers are exercised end to end against the wire format.
func fakeChain(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/block52/pokerchain/poker/v1/list_games", func(w http.ResponseWriter, _ *http.Request) {
		games := []chain.GameListItem{{
			GameID:         "b52table1",
			Creator:        "b52creator",
			Format:         "cash",
			Variant:        "texas-holdem",
			CurrentPlayers: 2,
			GameOptions:    chain.GameOptions{MinPlayers: 2, MaxPlayers: 6, SmallBlind: "1000000", BigBlind: "2000000"},
		}}
		inner, _ := json.Marshal(games)
		_ = json.NewEncoder(w).Encode(map[string]string{"games": string(inner)})
	})

	mux.HandleFunc("/block52/pokerchain/poker/v1/game_state_public/b52table1", func(w http.ResponseWriter, _ *http.Request) {
		state := chain.GameState{
			HandNumber: 1,
			Round:      "preflop",
			NextToAct:  0,
			Players: []chain.Player{{
				Seat: 0, Address: "b52me", Status: "active", Stack: "100000000",
				LegalActions: []chain.LegalAction{{Action: "fold", Min: "0", Max: "0"}, {Action: "call", Min: "2000000", Max: "2000000"}},
			}},
		}
		inner, _ := json.Marshal(state)
		_ = json.NewEncoder(w).Encode(map[string]string{"game_state": string(inner)})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newServer(t *testing.T, rest string) *Server {
	t.Helper()
	cl, err := chain.New(chain.Config{RestEndpoint: rest, ChainID: "pokerchain"})
	if err != nil {
		t.Fatalf("chain.New: %v", err)
	}
	return New(cl, moltbook.New(moltbook.Config{}))
}

func TestListTables(t *testing.T) {
	fc := fakeChain(t)
	s := newServer(t, fc.URL)

	resp, err := s.ListTables(context.Background(), &agentv1.ListTablesRequest{})
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(resp.Tables) != 1 {
		t.Fatalf("want 1 table, got %d", len(resp.Tables))
	}
	tb := resp.Tables[0]
	if tb.GameId != "b52table1" || tb.Variant != "texas-holdem" || tb.MaxSeats != 6 || tb.BigBlind != "2000000" {
		t.Fatalf("unexpected table: %+v", tb)
	}
}

func TestListTablesCapacityFilter(t *testing.T) {
	fc := fakeChain(t)
	s := newServer(t, fc.URL)

	// Table seats 2-6; asking for tables that fit >=8 players should exclude it.
	min := uint64(8)
	resp, err := s.ListTables(context.Background(), &agentv1.ListTablesRequest{MinPlayers: &min})
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(resp.Tables) != 0 {
		t.Fatalf("want 0 tables after min=8 filter, got %d", len(resp.Tables))
	}
}

func TestGetMyViewReturnsRawState(t *testing.T) {
	fc := fakeChain(t)
	s := newServer(t, fc.URL)

	resp, err := s.GetMyView(context.Background(), &agentv1.GetMyViewRequest{GameId: "b52table1"})
	if err != nil {
		t.Fatalf("GetMyView: %v", err)
	}
	// Address derivation is Phase 2, so legal actions are empty, but the raw
	// public state must round-trip and be valid JSON with the expected round.
	var gs chain.GameState
	if err := json.Unmarshal([]byte(resp.GameStateJson), &gs); err != nil {
		t.Fatalf("game_state_json not valid JSON: %v", err)
	}
	if gs.Round != "preflop" || len(gs.Players) != 1 {
		t.Fatalf("unexpected decoded state: %+v", gs)
	}
	// No key configured in this test -> defaults to OWN ownership.
	if resp.Ownership != agentv1.Ownership_OWNERSHIP_OWN {
		t.Fatalf("ownership = %v, want OWN", resp.Ownership)
	}
}

func TestGetMyViewReportsDelegatedOwnership(t *testing.T) {
	fc := fakeChain(t)
	cl, err := chain.New(chain.Config{RestEndpoint: fc.URL, ChainID: "pokerchain", Owner: chain.OwnerDelegated})
	if err != nil {
		t.Fatalf("chain.New: %v", err)
	}
	s := New(cl, moltbook.New(moltbook.Config{}))
	resp, err := s.GetMyView(context.Background(), &agentv1.GetMyViewRequest{GameId: "b52table1"})
	if err != nil {
		t.Fatalf("GetMyView: %v", err)
	}
	if resp.Ownership != agentv1.Ownership_OWNERSHIP_DELEGATED {
		t.Fatalf("ownership = %v, want DELEGATED", resp.Ownership)
	}
}

func TestGetMyViewRequiresGameID(t *testing.T) {
	s := newServer(t, "http://127.0.0.1:1")
	if _, err := s.GetMyView(context.Background(), &agentv1.GetMyViewRequest{}); err == nil {
		t.Fatal("expected error for empty game_id")
	}
}
