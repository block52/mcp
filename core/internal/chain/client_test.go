package chain

import "testing"

func sampleState() *GameState {
	return &GameState{
		NextToAct: 3,
		Players: []Player{
			{Seat: 1, Address: "b52alice"},
			{Seat: 3, Address: "b52bob"},
		},
	}
}

func TestIsTurnBySeat(t *testing.T) {
	gs := sampleState()
	// NextToAct is a SEAT number (3), which is bob's seat — not an index.
	if !gs.IsTurn("b52bob") {
		t.Fatal("bob (seat 3) should be to act")
	}
	if gs.IsTurn("b52alice") {
		t.Fatal("alice (seat 1) should not be to act")
	}
	if gs.IsTurn("b52nobody") {
		t.Fatal("unknown address should not be to act")
	}
}

func TestPlayerLookups(t *testing.T) {
	gs := sampleState()
	if p := gs.PlayerBySeat(3); p == nil || p.Address != "b52bob" {
		t.Fatalf("PlayerBySeat(3) = %+v", p)
	}
	if p := gs.PlayerByAddress("b52alice"); p == nil || p.Seat != 1 {
		t.Fatalf("PlayerByAddress(alice) = %+v", p)
	}
	if gs.PlayerBySeat(99) != nil {
		t.Fatal("PlayerBySeat(99) should be nil")
	}
}
