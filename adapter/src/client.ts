/** Connect gRPC client for the AgentService core. */
import { createPromiseClient, type PromiseClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import { AgentService } from "./gen/agent/v1/agent_connect.js";

export type AgentClient = PromiseClient<typeof AgentService>;

export function makeClient(baseUrl: string): AgentClient {
  const transport = createGrpcTransport({
    baseUrl,
    httpVersion: "2",
  });
  return createPromiseClient(AgentService, transport);
}
