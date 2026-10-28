# Security model — keys & money

An agent plays with **real on-chain funds**. The core signs `MsgPerformAction` /
`MsgJoinGame` / `MsgLeaveGame` with whatever key it is given. Read this before
pointing an agent at a wallet.

## Ownership modes

Set `OWNER` in the core's environment. It does **not** change signing — it makes
*whose money* explicit (logged at startup, returned in `get_my_view.ownership`).

| `OWNER` | Meaning | Risk |
|---------|---------|------|
| `own` (default) | The key is the agent's own. It risks its own bankroll. | Bounded by that wallet's balance. |
| `delegated` | **Full key hand-off** — a human handed the agent their key. | The agent has FULL control and can spend the **entire** balance. There is **no on-chain spend cap.** |

## Full key hand-off (`OWNER=delegated`) — the rules

This is the simplest delegation model and the one this project implements. It has
**zero isolation**: the key *is* the money. Treat it accordingly.

1. **Use a dedicated / throwaway bankroll only.** Fund a fresh wallet with exactly
   what you are willing to let the agent lose, and hand *that* key over. Never hand
   over a key that also holds savings, staking, or bridge funds.
2. **There is no cap.** The core will sign any legal action the agent chooses,
   including betting the whole stack. It cannot refuse an over-budget action because
   there is no budget — only the wallet balance.
3. **The agent can also move funds off the table.** A key is a key: anything the
   chain lets that address do (leave with the stack, future transfers), the agent
   can do. Segregation is your only protection.
4. **Rotate after use.** When the engagement ends, move remaining funds out and stop
   using the key. Assume anything the agent process touched could be logged.
5. **Keys come from the environment** (`MNEMONIC` / `PRIVATE_KEY`). Do not bake them
   into images, commit them, or pass them to anything other than the core. The MCP
   adapter never sees the key — it only talks gRPC to the core.

## What a spend cap would require (not built)

A true capped delegation (agent may lose at most X) needs enforcement the key
holder cannot bypass: either an on-chain authz/allowance granted to a session key,
or a co-signing gateway that rejects over-cap transactions. A cap enforced only
inside this core would be advisory — the key could sign around it. Until that
exists, `delegated` is full hand-off, full stop.
