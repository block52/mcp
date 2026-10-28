// Package chain wraps the Block52 pokerchain: REST reads + signed Cosmos Msg
// broadcast. The write path builds and signs the same protos the Go bot does
// (/pokerchain.poker.v1.MsgPerformAction / MsgJoinGame / MsgLeaveGame), so this
// package is the Go-native twin of @block52/poker-vm-sdk's SigningCosmosClient.
//
// Reference (to port, not import):
//
//	poker-vm/bot/go/cosmos.go        — MsgPerformAction/MsgJoinGame structs,
//	                                    EncodeMsgPerformAction, encodeAny, signDoc
//	poker-vm/bot/go/derive_addr.go   — bech32 prefix "b52" / "b52pub"
//	poker-vm/bot/go/chaindirect.go   — REST: /block52/pokerchain/poker/v1/...
//
// Chain facts (verified from the bot):
//
//	chainID default "pokerchain", Cosmos SDK v0.50.2, secp256k1, denom "usdc" (6dp).
//	Public spectator read exists on-chain: GET .../poker/v1/game_state_public/{id}
package chain

import "context"

// Config holds endpoints and the agent's signing identity.
type Config struct {
	RestEndpoint string // e.g. http://localhost:1317
	RpcEndpoint  string // e.g. http://localhost:26657
	WsEndpoint   string // WS relay, e.g. ws://localhost:8585
	ChainID      string // "pokerchain"
	// Mnemonic OR PrivateKeyHex identifies the agent. "Own money" = the agent's
	// own key. "Human's money" (Phase 5) = a delegated, spend-capped key.
	Mnemonic      string
	PrivateKeyHex string
}

// Client is the chain-facing surface the gRPC server depends on.
// TODO(phase1): implement reads. TODO(phase2): implement signed writes.
type Client struct {
	cfg  Config
	addr string // derived b52... address
}

// New constructs a Client and derives the agent's address from its key.
func New(cfg Config) (*Client, error) {
	// TODO(phase2): derive address via bech32 "b52" prefix (see bot/go/derive_addr.go).
	return &Client{cfg: cfg}, nil
}

// Address returns the agent's b52 address.
func (c *Client) Address() string { return c.addr }

// --- Reads (Phase 1) ---

// GameStateJSON returns the raw chain game_state JSON for a table (caller's view,
// includes own hole cards). GET /block52/pokerchain/poker/v1/game_state/{id}.
func (c *Client) GameStateJSON(ctx context.Context, gameID string) (string, error) {
	panic("TODO(phase1): REST GET game_state/{id}")
}

// ListGames returns joinable tables. GET .../poker/v1/games (+ optional filters).
func (c *Client) ListGames(ctx context.Context, minP, maxP uint64) ([]byte, error) {
	panic("TODO(phase1): REST GET games")
}

// LegalActions is a dedicated chain query (NOT derived from state).
// GET .../poker/v1/legal_actions/{id}?player={addr}.
func (c *Client) LegalActions(ctx context.Context, gameID, player string) ([]byte, error) {
	panic("TODO(phase1): REST GET legal_actions/{id}")
}

// --- Writes (Phase 2): build + sign + broadcast Cosmos Msgs ---

// PerformAction signs MsgPerformAction and broadcasts in SYNC mode (sub-second),
// mirroring the SDK's performActionSync. data carries Omaha card selectors.
func (c *Client) PerformAction(ctx context.Context, gameID, action, amount, data string) (string, error) {
	panic("TODO(phase2): build+sign MsgPerformAction, broadcast sync")
}

// JoinGame signs MsgJoinGame.
func (c *Client) JoinGame(ctx context.Context, gameID string, seat uint32, buyIn string) (string, error) {
	panic("TODO(phase2): build+sign MsgJoinGame")
}

// LeaveGame signs MsgLeaveGame.
func (c *Client) LeaveGame(ctx context.Context, gameID string, finishingOrder []string) (string, error) {
	panic("TODO(phase2): build+sign MsgLeaveGame")
}
