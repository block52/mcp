/**
 * Block52 Poker MCP adapter.
 *
 * Thin MCP server (stdio) that turns the typed AgentService gRPC core into tools
 * an LLM agent can call. It holds NO poker logic and NO keys — the core signs.
 *
 * Tools:
 *   do(action, params)   join | act | leave | sit-out | top-up | post
 *   get_my_view(table_id)
 *   list_tables(min?, max?)
 *   wait_for_turn(table_id)
 *
 * TODO(phase3): import the generated Connect client from ./gen/agent/v1 (produced
 * by `buf generate`) and dial CORE_ADDR. Wire each tool to the matching RPC.
 */

// import { Server } from "@modelcontextprotocol/sdk/server/index.js";
// import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
// import { createClient } from "@connectrpc/connect";
// import { createGrpcTransport } from "@connectrpc/connect-node";
// import { AgentService } from "./gen/agent/v1/agent_connect.js";

const CORE_ADDR = process.env.CORE_ADDR ?? "http://localhost:50051";

const TOOLS = [
  {
    name: "do",
    description:
      "Perform a poker action. action one of: join, act, leave, sit-out, top-up, post. " +
      "params carries the rest, e.g. do('act', {table_id, action:'raise', amount:'50000000'}).",
    inputSchema: {
      type: "object",
      properties: {
        action: {
          type: "string",
          enum: ["join", "act", "leave", "sit-out", "top-up", "post"],
        },
        params: { type: "object" },
      },
      required: ["action", "params"],
    },
  },
  {
    name: "get_my_view",
    description:
      "Full table state including MY hole cards plus MY legal actions. Call before acting.",
    inputSchema: {
      type: "object",
      properties: { table_id: { type: "string" } },
      required: ["table_id"],
    },
  },
  {
    name: "list_tables",
    description: "Discover joinable tables, optionally filtered by seat count.",
    inputSchema: {
      type: "object",
      properties: {
        min_players: { type: "number" },
        max_players: { type: "number" },
      },
    },
  },
  {
    name: "wait_for_turn",
    description: "Block until it is my turn to act at the table (or timeout).",
    inputSchema: {
      type: "object",
      properties: {
        table_id: { type: "string" },
        timeout_seconds: { type: "number" },
      },
      required: ["table_id"],
    },
  },
] as const;

async function main(): Promise<void> {
  // TODO(phase3): construct the MCP Server, register ListTools -> TOOLS and
  // CallTool -> dispatch to the AgentService Connect client at CORE_ADDR.
  //   "do" with action "act"   -> core.Act({ gameId, action, amount, data })
  //   "do" with action "join"  -> core.Join(...)
  //   "do" with action "post"  -> moltbook (Phase 6)
  //   get_my_view / list_tables / wait_for_turn -> matching RPCs
  // Then: await server.connect(new StdioServerTransport());
  console.error(
    `[block52-poker-mcp] scaffold only — ${TOOLS.length} tools declared, core=${CORE_ADDR}. Phase 3 wires them.`,
  );
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
