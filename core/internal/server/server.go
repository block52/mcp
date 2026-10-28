// Package server implements agent.v1.AgentService over the chain client.
//
// Phase 1 implements the read path: ListTables, GetMyView, WaitForTurn.
// The write path (Join/Act/Leave) returns Unimplemented until Phase 2.
package server

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/block52/mcp/core/gen/agent/v1"
	"github.com/block52/mcp/core/internal/chain"
	"github.com/block52/mcp/core/internal/moltbook"
)

// Server implements agentv1.AgentServiceServer.
type Server struct {
	agentv1.UnimplementedAgentServiceServer
	chain    *chain.Client
	moltbook *moltbook.Client
}

func New(c *chain.Client, mb *moltbook.Client) *Server {
	return &Server{chain: c, moltbook: mb}
}

// ListTables returns joinable tables, optionally filtered by seat capacity.
func (s *Server) ListTables(ctx context.Context, req *agentv1.ListTablesRequest) (*agentv1.ListTablesResponse, error) {
	games, err := s.chain.ListGames(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "list games: %v", err)
	}

	out := make([]*agentv1.Table, 0, len(games))
	for _, g := range games {
		// Filter by capacity: the table must be able to seat within [min,max].
		if req.MinPlayers != nil && uint64(g.GameOptions.MaxPlayers) < *req.MinPlayers {
			continue
		}
		if req.MaxPlayers != nil && uint64(g.GameOptions.MinPlayers) > *req.MaxPlayers {
			continue
		}
		out = append(out, &agentv1.Table{
			GameId:     g.GameID,
			Variant:    g.Variant,
			Format:     g.Format,
			Seated:     uint32(g.CurrentPlayers),
			MaxSeats:   uint32(g.GameOptions.MaxPlayers),
			SmallBlind: g.GameOptions.SmallBlind,
			BigBlind:   g.GameOptions.BigBlind,
		})
	}
	return &agentv1.ListTablesResponse{Tables: out}, nil
}

// GetMyView returns the caller's table view: full (public) state JSON plus the
// caller's own legal actions and whether it is their turn.
func (s *Server) GetMyView(ctx context.Context, req *agentv1.GetMyViewRequest) (*agentv1.GetMyViewResponse, error) {
	if req.GameId == "" {
		return nil, status.Error(codes.InvalidArgument, "game_id is required")
	}
	raw, err := s.chain.GameStateJSON(ctx, req.GameId)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "game state: %v", err)
	}
	gs, err := s.chain.GameState(ctx, req.GameId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "decode state: %v", err)
	}

	addr := s.chain.Address()
	resp := &agentv1.GetMyViewResponse{
		GameStateJson: raw,
		MyAddress:     addr,
		Ownership:     ownershipEnum(s.chain.Owner()),
	}
	// Address derivation lands in Phase 2; until then we can still return the
	// state and leave legal actions / my_turn empty.
	if addr != "" {
		if me := gs.PlayerByAddress(addr); me != nil {
			resp.LegalActions = toProtoLegal(me.LegalActions)
			resp.MyTurn = me.Seat == gs.NextToAct
		}
	}
	return resp, nil
}

// WaitForTurn blocks until it is the caller's turn or the timeout elapses.
// Phase 1 polls the public state; a WS-relay subscription (bot/go relayConnect)
// can replace the poll later for lower latency.
func (s *Server) WaitForTurn(ctx context.Context, req *agentv1.WaitForTurnRequest) (*agentv1.WaitForTurnResponse, error) {
	if req.GameId == "" {
		return nil, status.Error(codes.InvalidArgument, "game_id is required")
	}
	addr := s.chain.Address()
	if addr == "" {
		return nil, status.Error(codes.FailedPrecondition, "no agent address (key derivation lands in Phase 2)")
	}

	timeout := 60 * time.Second
	if req.TimeoutSeconds != nil {
		timeout = time.Duration(*req.TimeoutSeconds) * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()

	for {
		gs, err := s.chain.GameState(ctx, req.GameId)
		if err == nil {
			if me := gs.PlayerByAddress(addr); me != nil && me.Seat == gs.NextToAct {
				return &agentv1.WaitForTurnResponse{
					MyTurn:       true,
					LegalActions: toProtoLegal(me.LegalActions),
				}, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-deadline.C:
			return &agentv1.WaitForTurnResponse{TimedOut: true}, nil
		case <-tick.C:
		}
	}
}

// Join seats the agent at a table with a buy-in. -> MsgJoinGame.
func (s *Server) Join(ctx context.Context, req *agentv1.JoinRequest) (*agentv1.JoinResponse, error) {
	if req.GameId == "" {
		return nil, status.Error(codes.InvalidArgument, "game_id is required")
	}
	hash, err := s.chain.JoinGame(ctx, req.GameId, req.Seat, req.BuyIn)
	return &agentv1.JoinResponse{Tx: txResult(hash, err)}, grpcErr(err)
}

// Act performs a poker action (sync broadcast). -> MsgPerformAction.
func (s *Server) Act(ctx context.Context, req *agentv1.ActRequest) (*agentv1.ActResponse, error) {
	if req.GameId == "" || req.Action == "" {
		return nil, status.Error(codes.InvalidArgument, "game_id and action are required")
	}
	amount, data := "", ""
	if req.Amount != nil {
		amount = *req.Amount
	}
	if req.Data != nil {
		data = *req.Data
	}
	hash, err := s.chain.PerformAction(ctx, req.GameId, req.Action, amount, data)
	return &agentv1.ActResponse{Tx: txResult(hash, err)}, grpcErr(err)
}

// Leave removes the agent from a table. -> MsgLeaveGame.
func (s *Server) Leave(ctx context.Context, req *agentv1.LeaveRequest) (*agentv1.LeaveResponse, error) {
	if req.GameId == "" {
		return nil, status.Error(codes.InvalidArgument, "game_id is required")
	}
	hash, err := s.chain.LeaveGame(ctx, req.GameId, req.FinishingOrder)
	return &agentv1.LeaveResponse{Tx: txResult(hash, err)}, grpcErr(err)
}

// Post publishes the agent's narration to moltbook (best-effort, rate-limited).
func (s *Server) Post(ctx context.Context, req *agentv1.PostRequest) (*agentv1.PostResponse, error) {
	if req.Title == "" || req.Content == "" {
		return nil, status.Error(codes.InvalidArgument, "title and content are required")
	}
	if s.moltbook == nil || !s.moltbook.Enabled() {
		return nil, status.Error(codes.FailedPrecondition, "moltbook not configured (set MOLTBOOK_API_KEY)")
	}
	res, err := s.moltbook.Post(ctx, req.Title, req.Content, req.Submolt)
	if err != nil {
		return &agentv1.PostResponse{Error: err.Error()}, status.Errorf(codes.Unavailable, "%v", err)
	}
	return &agentv1.PostResponse{PostId: res.PostID, Skipped: res.Skipped}, nil
}

// txResult builds the Tx payload; on error it carries the message so callers
// that ignore the gRPC status still see why it failed.
func txResult(hash string, err error) *agentv1.Tx {
	if err != nil {
		return &agentv1.Tx{Success: false, Error: err.Error()}
	}
	return &agentv1.Tx{Success: true, TxHash: hash}
}

// grpcErr maps a chain write error to a gRPC status (nil on success).
func grpcErr(err error) error {
	if err == nil {
		return nil
	}
	return status.Errorf(codes.Unavailable, "%v", err)
}

// ownershipEnum maps the chain client's string ownership to the proto enum.
func ownershipEnum(owner string) agentv1.Ownership {
	if owner == chain.OwnerDelegated {
		return agentv1.Ownership_OWNERSHIP_DELEGATED
	}
	return agentv1.Ownership_OWNERSHIP_OWN
}

func toProtoLegal(in []chain.LegalAction) []*agentv1.LegalAction {
	out := make([]*agentv1.LegalAction, 0, len(in))
	for _, a := range in {
		out = append(out, &agentv1.LegalAction{Action: a.Action, Min: a.Min, Max: a.Max})
	}
	return out
}
