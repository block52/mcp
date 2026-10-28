#!/usr/bin/env bash
# One-command startup for the Block52 agent core (agentd).
#
#   ./scripts/start-core.sh
#
# Builds the Go core + TS adapter, then runs agentd in the foreground with config
# from a local .env (copy .env.example first). The adapter is NOT started here —
# your MCP client launches it on demand (see mcp.config.example.json).
set -euo pipefail

cd "$(dirname "$0")/.."

# Load .env if present (export every assignment).
if [[ -f .env ]]; then
  set -a; # shellcheck disable=SC1091
  source .env; set +a
fi

: "${REST_ENDPOINT:=https://node1.block52.xyz}"
: "${CHAIN_ID:=pokerchain}"
: "${LISTEN:=:50051}"
export REST_ENDPOINT CHAIN_ID LISTEN

echo "building core + adapter…"
command -v buf >/dev/null || { echo "need 'buf' (https://buf.build) to generate stubs"; exit 1; }
make gen >/dev/null
(cd core && go build -o bin/agentd ./cmd/agentd)
(cd adapter && npm install --silent && npm run build >/dev/null)

echo "adapter built: $(pwd)/adapter/dist/index.js"
echo "point your MCP client's command at that path (see mcp.config.example.json)."
echo
echo "starting agentd on ${LISTEN} → ${REST_ENDPOINT} (${CHAIN_ID})"
exec ./core/bin/agentd
