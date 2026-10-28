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
//	poker-vm/bot/go/chaindirect.go   — REST + the doubly-encoded game_state JSON
//
// Chain facts (verified from the bot + SDK):
//
//	chainID default "pokerchain", Cosmos SDK v0.50.2, secp256k1, denom "usdc" (6dp).
//	game_state_public/{id} and list_games both wrap their JSON payload in a string
//	field that must be unmarshalled a SECOND time.
package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

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
// Reads (Phase 1) + signed writes (Phase 2) are implemented.
type Client struct {
	cfg    Config
	http   *http.Client
	wallet *wallet // nil when no key configured (read-only mode)
}

// New constructs a Client. If a mnemonic or private key is configured it derives
// the agent's b52 signing identity ("own money"); otherwise the client is
// read-only and the write methods return an error.
func New(cfg Config) (*Client, error) {
	c := &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 10 * time.Second},
	}
	switch {
	case cfg.Mnemonic != "":
		w, err := walletFromMnemonic(cfg.Mnemonic)
		if err != nil {
			return nil, fmt.Errorf("mnemonic: %w", err)
		}
		c.wallet = w
	case cfg.PrivateKeyHex != "":
		w, err := walletFromPrivateKeyHex(cfg.PrivateKeyHex)
		if err != nil {
			return nil, fmt.Errorf("private key: %w", err)
		}
		c.wallet = w
	}
	return c, nil
}

// Address returns the agent's b52 address, or "" if no key is configured.
func (c *Client) Address() string {
	if c.wallet == nil {
		return ""
	}
	return c.wallet.address()
}

// HasKey reports whether the client can sign (a key was configured).
func (c *Client) HasKey() bool { return c.wallet != nil }

// --- Wire DTOs (match the chain's game_state JSON; see bot/go/chaindirect.go) ---

// LegalAction is embedded per-player in the state — no separate query needed.
type LegalAction struct {
	Action string `json:"action"`
	Min    string `json:"min"`
	Max    string `json:"max"`
}

// Player is one seat in the public state. Hole cards are absent in the public
// view for opponents (that is exactly what makes it spectator-safe).
type Player struct {
	Seat         int           `json:"seat"`
	Address      string        `json:"address"`
	Status       string        `json:"status"`
	Stack        string        `json:"stack"`
	LegalActions []LegalAction `json:"legalActions"`
}

// GameState is the decoded inner game_state. actionCount stays 0 on-chain — key
// progress off len(PreviousActions) instead (bot/go cdFull.progress()).
type GameState struct {
	HandNumber      int               `json:"handNumber"`
	Round           string            `json:"round"`
	NextToAct       int               `json:"nextToAct"` // SEAT number, not an address
	ActionCount     int               `json:"actionCount"`
	Players         []Player          `json:"players"`
	PreviousActions []json.RawMessage `json:"previousActions"`
	Results         []json.RawMessage `json:"results"`
}

// GameOptions mirrors the SDK GameOptionsDTO fields we surface for discovery.
type GameOptions struct {
	MinPlayers int    `json:"minPlayers"`
	MaxPlayers int    `json:"maxPlayers"`
	SmallBlind string `json:"smallBlind"`
	BigBlind   string `json:"bigBlind"`
}

// GameListItem is one entry from list_games (SDK GameListItem).
type GameListItem struct {
	GameID         string      `json:"gameId"`
	Creator        string      `json:"creator"`
	Format         string      `json:"format"`
	Variant        string      `json:"variant"`
	Name           string      `json:"name,omitempty"`
	GameOptions    GameOptions `json:"gameOptions"`
	CurrentPlayers int         `json:"currentPlayers"`
}

// --- Reads (Phase 1) ---

// getJSON performs a GET and decodes the body into v.
func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.RestEndpoint+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, v)
}

// GameStateJSON returns the raw inner game_state JSON for a table (the public,
// opponent-hidden view). The endpoint double-encodes: {"game_state":"<json>"}.
func (c *Client) GameStateJSON(ctx context.Context, gameID string) (string, error) {
	var outer struct {
		GameState string `json:"game_state"`
	}
	if err := c.getJSON(ctx, "/block52/pokerchain/poker/v1/game_state_public/"+gameID, &outer); err != nil {
		return "", err
	}
	if outer.GameState == "" {
		return "", fmt.Errorf("game %s: empty game_state", gameID)
	}
	return outer.GameState, nil
}

// GameState fetches and decodes the inner state in one step.
func (c *Client) GameState(ctx context.Context, gameID string) (*GameState, error) {
	raw, err := c.GameStateJSON(ctx, gameID)
	if err != nil {
		return nil, err
	}
	var gs GameState
	if err := json.Unmarshal([]byte(raw), &gs); err != nil {
		return nil, fmt.Errorf("decode game_state for %s: %w", gameID, err)
	}
	return &gs, nil
}

// ListGames returns all joinable tables. list_games double-encodes under "games".
func (c *Client) ListGames(ctx context.Context) ([]GameListItem, error) {
	var outer struct {
		Games string `json:"games"`
	}
	if err := c.getJSON(ctx, "/block52/pokerchain/poker/v1/list_games", &outer); err != nil {
		return nil, err
	}
	if outer.Games == "" || outer.Games == "null" {
		return nil, nil
	}
	var games []GameListItem
	if err := json.Unmarshal([]byte(outer.Games), &games); err != nil {
		return nil, fmt.Errorf("decode list_games: %w", err)
	}
	return games, nil
}

// PlayerBySeat returns the player seated at seat, or nil.
func (gs *GameState) PlayerBySeat(seat int) *Player {
	for i := range gs.Players {
		if gs.Players[i].Seat == seat {
			return &gs.Players[i]
		}
	}
	return nil
}

// PlayerByAddress returns the player with the given address, or nil.
func (gs *GameState) PlayerByAddress(addr string) *Player {
	for i := range gs.Players {
		if gs.Players[i].Address == addr {
			return &gs.Players[i]
		}
	}
	return nil
}

// IsTurn reports whether the player at addr is next to act. NextToAct is a seat
// number, so resolve the address to its seat and compare.
func (gs *GameState) IsTurn(addr string) bool {
	p := gs.PlayerByAddress(addr)
	return p != nil && p.Seat == gs.NextToAct
}

// --- Writes (Phase 2): build + sign + broadcast Cosmos Msgs ---

// errNoKey is returned by the write methods when the client has no signing key.
var errNoKey = fmt.Errorf("no signing key configured (read-only client)")

// parseMicro parses a micro-USDC string amount into uint64. Empty = 0.
func parseMicro(amount string) (uint64, error) {
	if amount == "" {
		return 0, nil
	}
	v, err := strconv.ParseUint(amount, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q: %w", amount, err)
	}
	return v, nil
}

// PerformAction signs MsgPerformAction and broadcasts in SYNC mode (sub-second),
// mirroring the SDK's performActionSync. data carries Omaha card selectors.
func (c *Client) PerformAction(ctx context.Context, gameID, action, amount, data string) (string, error) {
	if c.wallet == nil {
		return "", errNoKey
	}
	amt, err := parseMicro(amount)
	if err != nil {
		return "", err
	}
	msg := encodeMsgPerformAction(c.wallet.address(), gameID, action, amt, data)
	return c.signAndBroadcast(ctx, c.wallet, "/pokerchain.poker.v1.MsgPerformAction", msg, "act via block52-mcp")
}

// JoinGame signs MsgJoinGame.
func (c *Client) JoinGame(ctx context.Context, gameID string, seat uint32, buyIn string) (string, error) {
	if c.wallet == nil {
		return "", errNoKey
	}
	amt, err := parseMicro(buyIn)
	if err != nil {
		return "", err
	}
	msg := encodeMsgJoinGame(c.wallet.address(), gameID, uint64(seat), amt)
	return c.signAndBroadcast(ctx, c.wallet, "/pokerchain.poker.v1.MsgJoinGame", msg, "join via block52-mcp")
}

// LeaveGame signs MsgLeaveGame. finishingOrder is SNG-only and currently unused
// for the cash-game subset (matches the bot's cash-game LeaveGame).
func (c *Client) LeaveGame(ctx context.Context, gameID string, finishingOrder []string) (string, error) {
	if c.wallet == nil {
		return "", errNoKey
	}
	msg := encodeMsgLeaveGame(c.wallet.address(), gameID)
	return c.signAndBroadcast(ctx, c.wallet, "/pokerchain.poker.v1.MsgLeaveGame", msg, "leave via block52-mcp")
}
