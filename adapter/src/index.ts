#!/usr/bin/env node
/**
 * Block52 Poker MCP adapter.
 *
 * Thin MCP server (stdio) that turns the typed AgentService gRPC core into tools
 * an LLM agent can call. It holds NO poker logic and NO keys — the core signs.
 *
 * Tools:
 *   do(action, params)   join | act | leave  (post -> Phase 6)
 *   get_my_view(table_id)
 *   list_tables(min_players?, max_players?)
 *   wait_for_turn(table_id, timeout_seconds?)
 */
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { makeClient, type AgentClient } from "./client.js";

const CORE_ADDR = process.env.CORE_ADDR ?? "http://localhost:50051";

/** Wrap a JSON-able value as an MCP text result. */
function ok(value: unknown) {
  return { content: [{ type: "text" as const, text: JSON.stringify(value, null, 2) }] };
}
function fail(message: string) {
  return { isError: true, content: [{ type: "text" as const, text: message }] };
}

// --- do() sub-action handlers ---

const doParams = {
  join: z.object({
    table_id: z.string(),
    seat: z.number().int().nonnegative(),
    buy_in: z.string().describe("micro-USDC, string for BigInt safety"),
  }),
  act: z.object({
    table_id: z.string(),
    action: z.string().describe("fold | check | call | bet | raise | ..."),
    amount: z.string().optional().describe("micro-USDC; required for bet/raise/top-up"),
    data: z.string().optional().describe("e.g. packed card selectors for Omaha"),
  }),
  leave: z.object({
    table_id: z.string(),
    finishing_order: z.array(z.string()).optional(),
  }),
} as const;

async function runDo(client: AgentClient, action: string, params: unknown) {
  switch (action) {
    case "join": {
      const p = doParams.join.parse(params);
      const r = await client.join({ gameId: p.table_id, seat: p.seat, buyIn: p.buy_in });
      return r.tx;
    }
    case "act": {
      const p = doParams.act.parse(params);
      const r = await client.act({ gameId: p.table_id, action: p.action, amount: p.amount, data: p.data });
      return r.tx;
    }
    case "leave": {
      const p = doParams.leave.parse(params);
      const r = await client.leave({ gameId: p.table_id, finishingOrder: p.finishing_order ?? [] });
      return r.tx;
    }
    case "post":
      throw new Error("moltbook posting lands in Phase 6");
    default:
      throw new Error(`unknown action: ${action}`);
  }
}

function registerTools(server: McpServer, client: AgentClient) {
  server.registerTool(
    "do",
    {
      description:
        "Perform a poker action. action one of: join, act, leave (post -> later). " +
        "params carries the rest, e.g. do('act', {table_id, action:'raise', amount:'50000000'}).",
      inputSchema: {
        action: z.enum(["join", "act", "leave", "post"]),
        params: z.record(z.unknown()),
      },
    },
    async ({ action, params }) => {
      try {
        return ok(await runDo(client, action, params));
      } catch (e) {
        return fail(e instanceof Error ? e.message : String(e));
      }
    },
  );

  server.registerTool(
    "get_my_view",
    {
      description:
        "Full table state including MY hole cards plus MY legal actions and whether it is my turn. Call before acting.",
      inputSchema: { table_id: z.string() },
    },
    async ({ table_id }) => {
      try {
        const r = await client.getMyView({ gameId: table_id });
        return ok({
          my_address: r.myAddress,
          my_turn: r.myTurn,
          legal_actions: r.legalActions,
          game_state: JSON.parse(r.gameStateJson || "null"),
        });
      } catch (e) {
        return fail(e instanceof Error ? e.message : String(e));
      }
    },
  );

  server.registerTool(
    "list_tables",
    {
      description: "Discover joinable tables, optionally filtered by seat capacity.",
      inputSchema: {
        min_players: z.number().int().positive().optional(),
        max_players: z.number().int().positive().optional(),
      },
    },
    async ({ min_players, max_players }) => {
      try {
        const r = await client.listTables({
          minPlayers: min_players !== undefined ? BigInt(min_players) : undefined,
          maxPlayers: max_players !== undefined ? BigInt(max_players) : undefined,
        });
        return ok(r.tables);
      } catch (e) {
        return fail(e instanceof Error ? e.message : String(e));
      }
    },
  );

  server.registerTool(
    "wait_for_turn",
    {
      description: "Block until it is my turn to act at the table (or timeout).",
      inputSchema: {
        table_id: z.string(),
        timeout_seconds: z.number().int().positive().optional(),
      },
    },
    async ({ table_id, timeout_seconds }) => {
      try {
        const r = await client.waitForTurn({ gameId: table_id, timeoutSeconds: timeout_seconds });
        return ok({ my_turn: r.myTurn, timed_out: r.timedOut, legal_actions: r.legalActions });
      } catch (e) {
        return fail(e instanceof Error ? e.message : String(e));
      }
    },
  );
}

async function main(): Promise<void> {
  const client = makeClient(CORE_ADDR);
  const server = new McpServer({ name: "block52-poker", version: "0.0.1" });
  registerTools(server, client);
  await server.connect(new StdioServerTransport());
  // stderr so it doesn't corrupt the stdio JSON-RPC channel.
  console.error(`[block52-poker-mcp] ready; core=${CORE_ADDR}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
