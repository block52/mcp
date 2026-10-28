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

	"github.com/block52/mcp/core/internal/chain"
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
	}

	cl, err := chain.New(cfg)
	if err != nil {
		log.Fatalf("chain client: %v", err)
	}
	_ = server.New(cl) // TODO(phase1): RegisterAgentServiceServer once stubs exist

	addr := env("LISTEN", ":50051")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	gs := grpc.NewServer()
	// TODO(phase1): agentv1.RegisterAgentServiceServer(gs, srv)

	log.Printf("agentd listening on %s (chain=%s rest=%s)", addr, cfg.ChainID, cfg.RestEndpoint)
	if err := gs.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
