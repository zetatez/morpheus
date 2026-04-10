# Morpheus

A local AI agent runtime with tool execution, session persistence, MCP support, and interactive TUI.

## Features

- **Agent Runtime**: Iterative tool-calling with automatic retry
- **Multi-Agent**: Parallel coordination (up to 9 agents) with DAG scheduling
- **3-Layer Memory**: Working (8KB) + Episodic (100 events) + Semantic (24KB)
- **Tools**: File ops, shell, LSP, Git, web fetch
- **MCP**: Full MCP client (stdio/HTTP/SSE)
- **Session**: SQLite persistence with context compression
- **Multi-Model**: OpenAI, DeepSeek, MiniMax, GLM, Gemini, Claude, etc.
- **TUI**: Streaming responses, auto-scroll, request queuing

## Quick Start

```bash
go build -o morpheus ./cmd/morpheus
./morpheus repl              # TUI mode (requires bun)
./morpheus serve              # API server only
```

## Configuration

`config.yaml` (priority: `./` > `~/.config/morpheus/` > `~/.morpheus/`):

```yaml
workspace_root: ./
planner:
  provider: minimax
  model: MiniMax-M2.7
  api_key: ${MINIMAX_API_KEY}
server:
  listen: :8080
session:
  path: ~/.local/share/morpheus/sessions
  retention: 720h
mcp:
  servers:
    - name: filesystem
      transport: stdio
      command: npx -y @modelcontextprotocol/server-filesystem .
permissions:
  confirm_above: high
```

### Agent Modes

 | Mode    | Description                                 |
 | ------  | -------------                               |
 | `build` | Full access - execute commands, write files |
 | `plan`  | Read-only - plans without executing         |

### Interactive Confirmation

Reply `approve` or `deny` when prompted. Set `auto_approve: false` for interactive mode.

## Tools

 | Tool                           | Description           |
 | ------                         | -------------         |
 | `fs.read/write/edit/glob/grep` | File operations       |
 | `cmd.exec`                     | Shell commands        |
 | `lsp.query`                    | LSP operations        |
 | `web.fetch`                    | Fetch web pages       |
 | `mcp.query`                    | MCP server management |
 | `agent.run`                    | Run sub-agent         |

## Subagents

`implementer`, `explorer`, `reviewer`, `architect`, `tester`, `devops`, `data`, `security`, `docs`, `shell-python-operator`

## Skills

Invoke with `@skill`: `@review`, `@test`, `@docs`, `@refactor`, `@debug`, `@security`, `@git`, `@explain`, `@optimize`

Custom skills in `~/.config/morpheus/skills/`

## Memory System

```
User Input → Working Memory (8KB) → Agent Loop → Episodic Memory (100 events)
                                                       ↓
                                          Semantic Memory (24KB)
                                          (extracted patterns)
```

## Architecture

```
CLI / REPL / API
    │
    ▼
Core Runtime (Agent Loop, Coordinator, Effect System)
    │
    ├──▶ Tools (fs, cmd, lsp, mcp, webfetch, ...)
    │        │
    │        └──▶ MCP Server (stdio/http/sse)
    │
    ├──▶ Orchestrator ──▶ Policy Engine
    │
    ├──▶ Memory (Working, Episodic, Semantic)
    │
    └──▶ Session (SQLite)
```

### Core Modules

- `internal/app/`: Agent loop, coordinator, memory, effect runtime
- `internal/tools/`: Tool implementations (fs, cmd, lsp, mcp, webfetch, etc.)
- `internal/convo/`: Conversation & compression
- `internal/planner/llm/`: Multi-model adapter
- `internal/exec/`: Orchestrator & policy engine
- `internal/sync/`: Event bus & distributed locking (flock)
- `internal/storage/`: Storage abstraction layer
- `internal/app/reflection/`: Root cause analysis & recovery
- `internal/effect/`: Effect system for dependency injection

## Development

```bash
go run ./cmd/morpheus          # Run
go test ./...                  # Test
go build -o morpheus ./cmd/morpheus  # Build
```

## Tech Stack

Go 1.25 · Solid.js/Bun · SQLite · Cobra + Viper · uber/zap

## License

MIT
