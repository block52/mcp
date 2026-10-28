// Command agentd runs the Block52 AgentService gRPC core.
//
// It builds and signs Cosmos protos directly (Go-native, no Node runtime) and
// exposes the typed agent.v1.AgentService. The MCP adapter and non-LLM bots are
// its clients.
package main

import (
	"log"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	agentv1 "github.com/block52/mcp/core/gen/agent/v1"
	"github.com/block52/mcp/core/internal/chain"
	"github.com/block52/mcp/core/internal/moltbook"
	"github.com/block52/mcp/core/internal/server"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := chain.Config{
		RestEndpoint:  env("REST_ENDPOINT", "http://localhost:1317"),
		RpcEndpoint:   env("RPC_ENDPOINT", "http://localhost:26657"),
		WsEndpoint:    env("WS_ENDPOINT", "ws://localhost:8585"),
		ChainID:       env("CHAIN_ID", "pokerchain"),
		Mnemonic:      os.Getenv("MNEMONIC"),
		PrivateKeyHex: os.Getenv("PRIVATE_KEY"),
		Owner:         env("OWNER", chain.OwnerOwn),
	}

	cl, err := chain.New(cfg)
	if err != nil {
		log.Fatalf("chain client: %v", err)
	}
	mb := moltbook.New(moltbook.Config{
		APIKey:  os.Getenv("MOLTBOOK_API_KEY"),
		BaseURL: os.Getenv("MOLTBOOK_BASE_URL"), // empty -> moltbook.com (staging/test override)
	})
	srv := server.New(cl, mb)

	// Make "whose money" unmistakable at startup. A full key hand-off has no
	// on-chain cap — the agent can spend the entire balance.
	if cl.HasKey() {
		if cl.Owner() == chain.OwnerDelegated {
			log.Printf("WARNING: OWNER=delegated — agent %s holds a HUMAN's key with FULL control and NO spend cap. Use a dedicated/throwaway bankroll only.", cl.Address())
		} else {
			log.Printf("agent %s playing with its OWN money", cl.Address())
		}
	} else {
		log.Printf("no key configured — read-only (agent cannot join/act)")
	}
	if mb.Enabled() {
		log.Printf("moltbook posting enabled")
	} else {
		log.Printf("moltbook posting disabled (no MOLTBOOK_API_KEY)")
	}

	addr := env("LISTEN", ":50051")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	gs := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(gs, srv)
	reflection.Register(gs) // lets grpcurl / clients introspect the service

	log.Printf("agentd listening on %s (chain=%s rest=%s)", addr, cfg.ChainID, cfg.RestEndpoint)
	if err := gs.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
