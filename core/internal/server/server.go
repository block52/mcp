// Package server implements agent.v1.AgentService over the chain client.
//
// Until `buf generate` produces core/gen/agent/v1, the generated
// AgentServiceServer interface and request/response types don't exist yet, so
// this file intentionally does NOT implement the gRPC interface. It defines the
// handler struct and the shape of each RPC so Phase 1/2 can fill the bodies and
// wire it with RegisterAgentServiceServer.
package server

import "github.com/block52/mcp/core/internal/chain"

// Server holds dependencies for the AgentService handlers.
type Server struct {
	Chain *chain.Client
}

func New(c *chain.Client) *Server { return &Server{Chain: c} }

// Handlers to implement once stubs are generated (Phase 1 reads, Phase 2 writes):
//
//	ListTables(ctx, *ListTablesRequest) (*ListTablesResponse, error)  // Chain.ListGames
//	GetMyView(ctx, *GetMyViewRequest) (*GetMyViewResponse, error)     // GameStateJSON + LegalActions
//	WaitForTurn(ctx, *WaitForTurnRequest) (*WaitForTurnResponse, error) // WS relay until nextToAct==me
//	Join(ctx, *JoinRequest) (*TxResponse, error)                     // Chain.JoinGame
//	Act(ctx, *ActRequest) (*TxResponse, error)                       // Chain.PerformAction (sync)
//	Leave(ctx, *LeaveRequest) (*TxResponse, error)                   // Chain.LeaveGame
