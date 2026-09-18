# cassette — MCP (Model Context Protocol)

cassette records and replays MCP tool calls the same way it does LLM HTTP traffic:
record once against a real MCP server, then replay byte-exact and offline — the
child server process is never launched on replay.

## Record / replay in Go

Wrap your `*mcp.ClientSession` (anything satisfying `cassette.MCPCaller`) with
`Cassette.MCP`:

```go
c, _ := cassette.Open("testdata/tools.yaml", cassette.Options{Mode: mode})
caller := c.MCP(realSession) // realSession may be nil in replay — it's never used
res, err := caller.CallTool(ctx, &mcp.CallToolParams{Name: "add", Arguments: args})
```

- **Record** delegates to the real session and stores the params + result JSON.
- **Replay** serves the recorded result keyed by tool name + canonicalized
  arguments, touching no transport — proof the server is never exec'd.

Matching is on the tool name and the *canonical* arguments (key order and
whitespace don't matter), so a replayed call with logically-equal args still hits.

## Serve a recorded MCP server — `cassette mcp-serve`

```sh
cassette mcp-serve session.yaml
```

Exposes a recorded session as a real **stdio JSON-RPC** MCP server, so any MCP
client (an IDE, an agent, Claude Desktop) can talk to the recording with zero
subprocess launch and zero network — the MCP analogue of `cassette serve`.
`initialize`/`ping` are synthesized; `tools/list` and `tools/call` replay recorded
results using the exact same matching as record/replay.

## Record a live session via a proxy — `cassette mcp-proxy`

```sh
cassette mcp-proxy -o session.yaml -- npx @modelcontextprotocol/server-everything
cassette mcp-serve session.yaml      # replay — no subprocess, no network
```

When you can't wrap the client in Go, `mcp-proxy` records from the outside: it
spawns the real MCP server as a subprocess and sits transparently between your MCP
client (this process's stdin/stdout) and that server, forwarding every
newline-delimited JSON-RPC frame verbatim in both directions while teeing each
`tools/call` and `tools/list` (paired by JSON-RPC id) into a cassette. It
reimplements no MCP semantics — pure passthrough + record — and tool results are
scrubbed on save. Replay is `mcp-serve`, so the recorded session never launches the
subprocess again.

## Author an MCP cassette by hand — `kind: mcp` screenplays

`cassette author` compiles a terse screenplay into a replayable cassette with no
capture at all. Mark a turn `kind: mcp` to author MCP calls directly:

```yaml
turns:
  - kind: mcp
    method: tools/list
    tool_calls:
      - {name: add, args: '{"type":"object"}'}
  - kind: mcp
    method: tools/call      # the default
    tool: add
    args: '{"a":2,"b":3}'
    text: "5"               # synthesizes a CallToolResult; or set `result:` for raw JSON
  - kind: mcp
    tool: divide
    args: '{"a":1,"b":0}'
    tool_error: "division by zero"   # synthesizes an isError result
```

The match key is derived from the tool name + canonical arguments, identical to a
recorded call, so authored MCP cassettes replay through `mcp-serve` and pass
`cassette conformance`. A committed example lives at
[`cmd/cassette/testdata/mcp/calculator.screenplay.yaml`](../cmd/cassette/testdata/mcp/calculator.screenplay.yaml).

## Detect breaking server changes — `cassette mcp-diff`

As an MCP server evolves, a committed golden recording becomes the contract. Record
the server again and diff the two recordings to catch backward-incompatible changes:

```sh
cassette mcp-proxy -o golden.yaml -- my-server      # the committed baseline
# …server updated…
cassette mcp-proxy -o new.yaml    -- my-server
cassette mcp-diff golden.yaml new.yaml --fail-on-breaking   # CI gate (nonzero on a break)
```

`mcp-diff` compares the advertised `tools/list` input schemas and the `tools/call`
outcomes and flags the **breaking** changes — a tool removed, an input schema made
stricter (a property removed, a type changed, or a new required property), or a call
that used to succeed and now errors. Additive changes (a new tool, a new optional
property) are reported but don't fail the gate. Pair it with `cassette verify`/
`attest` (secret-free + signed manifest) that other MCP record/replay tools lack.

## MCP in the analysis tools

Recorded MCP turns decode into the same transcript model as HTTP, so `doc`,
`explain`, `dataset`, `diff`, and `cost` all see the tool side of the agentic
stack — a tool call renders as a `ToolCall` (name + canonical args) and the
result's text becomes the turn's text. See the [CLI reference](cli.md).

For why MCP keys are derived from tool name + arguments (and not a wire path), see
[`DECISIONS.md`](../DECISIONS.md).
