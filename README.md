# Morpheus

Morpheus is a local AI agent runtime with tool execution, session persistence, MCP protocol support, and an interactive TUI client.

## Features

- **AI Agent Runtime**: Iterative agent execution with tool calling
- **Shell/Python Task Execution**: Strong bias toward solving complex tasks with shell commands and short Python automation
- **Multi-Agent Coordination**: Built-in coordinator with parallel task execution
- **Subagents**: Built-in roles for coordination (implementer, explorer, reviewer, architect)
- **Automation Specialist**: Dedicated `shell-python-operator` subagent for command-heavy and script-heavy tasks
- **Tool Ecosystem**: File operations, command execution, LSP, Git, web fetch
- **MCP Protocol**: Full MCP client support (stdio/HTTP/SSE transports)
- **Session Persistence**: SQLite + file storage with smart context compression
- **Multi-Model Support**: OpenAI, DeepSeek, MiniMax, GLM, Gemini, or builtin keyword planner
- **TUI Client**: Interactive terminal UI with chat interface
- **REST API**: Programmatic access with streaming support
- **Configurable SOUL**: Custom agent personality via SOUL.md
- **Skills System**: 9 built-in skills + custom skills with lazy loading

## Installation

### From Source

```bash
git clone https://github.com/zetatez/morpheus.git
cd morpheus
go build -o morpheus ./cmd/morpheus
```

### Quick Start

```bash
# Run with REPL mode (requires bun for TUI)
./morpheus repl

# Or start API server only
./morpheus serve
```

### Python Installer

```bash
python install.py --help
python install.py --install          # Install to ~/.local/bin
python install.py --install --prefix /usr/local  # Custom prefix
```

## VSCode Extension

Morpheus provides a VSCode extension for native IDE integration.

```bash
cd extensions/vscode
npm install
npm run compile
```

**Features:**
- Chat panel (`Ctrl+Shift+M` / `Cmd+Shift+M`)
- Inline code editing
- Code explanation and refactoring
- Configurable endpoint, API key, and model parameters

## Configuration

Create `config.yaml` in one of these locations (in order of priority):

1. `./config.yaml` (current directory)
2. `~/.config/morpheus/config.yaml`
3. `~/.morpheus/config.yaml`

### Example Configuration

```yaml
workspace_root: ./

logging:
  level: info
  file: ~/.local/share/morpheus/logs/morpheus.log

planner:
  provider: deepseek        # openai, deepseek, minmax, glm, gemini, builtin
  model: deepseek-chat
  temperature: 0.2
  api_key: ${DEEPSEEK_API_KEY}

subagents:
  default_mode: build      # build (full access) | plan (read-only)

server:
  listen: :8080

session:
  path: ~/.local/share/morpheus/sessions
  sqlite_path: ~/.local/share/morpheus/sessions.db
  retention: 720h

mcp:
  servers:
    - name: filesystem
      transport: stdio
      command: npx -y @modelcontextprotocol/server-filesystem .

permissions:
  confirm_above: high
  confirm_protected_paths:
    - /etc
    - /usr/bin
    - ~/.ssh
```

### Interactive Confirmation

Morpheus supports interactive confirmation for high-risk operations:

- When a risky command is detected, Morpheus will prompt for approval
- Reply `approve` to proceed or `deny` to cancel
- By default `auto_approve: true` - set to `false` for interactive mode
- `confirm_above` controls threshold: `low`, `medium`, `high`, `critical`

### Agent Modes

| Mode | Description |
|------|-------------|
| `build` | Full access - can execute commands and write files |
| `plan` | Read-only - generates plans without executing |

### Custom Subagents

Custom subagents are loaded lazily from markdown files in:

- `~/.config/morpheus/subagents/<name>.md`

They are only loaded when the user explicitly references the subagent name in the conversation.

Example `~/.config/morpheus/subagents/release-notes.md`:

```markdown
---
name: release-notes
description: Draft release notes from git history.
tools:
  - fs.read
  - git.*
---
Write release notes in Markdown. Focus on user-facing changes and breaking changes first.
```

### Multi-Agent Coordination

Morpheus automatically coordinates multiple specialized subagents (up to 9) for complex tasks:

| Subagent | Description |
|-------|-------------|
| `implementer` | Delivers concrete code changes |
| `explorer` | Investigates codebase details |
| `reviewer` | Reviews changes for risks |
| `architect` | Designs high-level approach |
| `tester` | Writes and verifies tests |
| `devops` | Handles deployment and CI/CD |
| `data` | Works with data pipelines |
| `security` | Reviews security vulnerabilities |
| `docs` | Creates documentation |
| `shell-python-operator` | Handles shell pipelines, automation, and short Python scripts |

**DAG Scheduling**: Tasks can have dependencies (`depends_on`). Morpheus automatically performs topological sort and executes tasks in the correct order.

### Environment Variables

| Variable | Description |
|----------|-------------|
| `BRUTECODE_API_KEY` | Fallback API key |
| `OPENAI_API_KEY` | OpenAI provider |
| `DEEPSEEK_API_KEY` | DeepSeek provider |
| `MINMAX_API_KEY` | MiniMax provider |
| `GLM_API_KEY` | GLM provider |
| `GEMINI_API_KEY` | Gemini provider |

## CLI Commands

### REPL Mode

```bash
morpheus repl                      # Start with TUI frontend
morpheus repl --session my-session # Resume specific session
morpheus repl --prompt "task"      # Run initial prompt
morpheus repl --url http://host:8080  # Connect to remote server
morpheus repl --model gpt-4o       # Specify model
morpheus repl --plan               # Plan mode (read-only)
```

### Server Mode

```bash
morpheus serve                     # Start HTTP API server
morpheus serve --config path/to/config.yaml
```

## REST API

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/global/health` | Health check |
| GET | `/global/event` | SSE event stream |
| GET | `/global/sync-event` | SSE sync event stream |
| GET | `/global/config` | Get configuration |
| PATCH | `/global/config` | Update configuration |
| GET | `/doc` | OpenAPI documentation |
| POST | `/chat` | Chat with agent |
| POST | `/plan` | Generate plan |
| POST | `/execute` | Execute plan |
| POST | `/tasks/` | Create task |
| GET | `/tasks/{id}` | Get task status |
| DELETE | `/tasks/{id}` | Cancel task |
| GET | `/session/` | List sessions |
| POST | `/session/` | Create session |
| GET | `/session/status` | Get session status |
| GET | `/session/{id}` | Get session |
| PATCH | `/session/{id}` | Update session |
| DELETE | `/session/{id}` | Delete session |
| POST | `/session/{id}/fork` | Fork session |
| POST | `/session/{id}/share` | Share session |
| DELETE | `/session/{id}/share` | Unshare session |
| POST | `/session/{id}/summarize` | Summarize session |
| POST | `/session/{id}/abort` | Abort session |
| POST | `/session/{id}/revert` | Revert message |
| POST | `/session/{id}/unrevert` | Unrevert message |
| GET | `/session/{id}/children` | Get child sessions |
| GET | `/session/{id}/todo` | Get session todos |
| POST | `/session/{id}/init` | Initialize session |
| GET | `/session/{id}/message` | Get session messages |
| POST | `/session/{id}/message` | Send message |
| GET | `/session/{id}/message/{messageID}` | Get single message |
| DELETE | `/session/{id}/message/{messageID}` | Delete message |
| PATCH | `/session/{id}/message/{messageID}/part/{partID}` | Update message part |
| DELETE | `/session/{id}/message/{messageID}/part/{partID}` | Delete message part |
| GET | `/session/{id}/diff` | Get message diff |
| POST | `/session/{id}/command` | Send command |
| POST | `/session/{id}/shell` | Run shell command |
| GET | `/skill` | List skills |
| POST | `/skill/{name}` | Load skill |
| GET | `/models/` | List models |
| POST | `/models/select` | Switch model |
| GET | `/runs/` | List runs |
| GET | `/runs/{id}` | Get run |
| GET | `/metrics` | Server metrics |
| GET | `/provider/` | List AI providers |
| GET | `/provider/auth` | Get auth methods |
| GET | `/permission/` | List pending permissions |
| POST | `/permission/{id}/reply` | Reply to permission |
| GET | `/question/` | List pending questions |
| POST | `/question/{id}/reply` | Reply to question |
| POST | `/question/{id}/reject` | Reject question |
| GET | `/mcp/` | List MCP servers |
| POST | `/mcp/` | Add MCP server |
| POST | `/mcp/{name}/connect` | Connect MCP |
| POST | `/mcp/{name}/disconnect` | Disconnect MCP |
| GET | `/find` | Text search |
| GET | `/find/file` | File search |
| GET | `/find/symbol` | Symbol search |
| GET | `/file` | List files |
| GET | `/file/content` | Read file content |
| GET | `/file/status` | Git status |
| GET | `/project/` | List projects |
| GET | `/project/{id}` | Get project |
| PATCH | `/project/{id}` | Update project |
| GET | `/project/current` | Get current project |
| POST | `/project/git/init` | Initialize git |
| POST | `/shell` | Execute shell command |
| GET | `/vim` | Read remote file |
| POST | `/vim` | Write remote file |
| GET | `/ssh` | SSH info |
| GET | `/ws` | WebSocket endpoint |
| POST | `/repl` | REPL endpoint |
| POST | `/repl/stream` | Streaming REPL |

### Chat Example

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"session": "default", "input": "Hello"}'
```

### Streaming Example

```bash
curl -X POST http://localhost:8080/repl/stream \
  -H "Content-Type: application/json" \
  -d '{"session": "default", "input": "List files"}' \
  -N
```

## Tools

Morpheus provides these built-in tools:

| Tool | Description |
|------|-------------|
| `fs.read` | Read file contents |
| `fs.write` | Write file contents |
| `fs.edit` | Edit file with replacement |
| `fs.glob` | Glob pattern matching |
| `fs.grep` | Text search in files |
| `cmd.exec` | Execute shell commands |
| `lsp.query` | LSP operations (definition, references, etc.) |
| `git.*` | Git operations |
| `web.fetch` | Fetch web pages |
| `conversation.ask` | Ask user questions |
| `skill.invoke` | Invoke skills |
| `mcp.query` | Manage MCP servers |
| `mcp.<server>.<tool>` | MCP proxy tools |
| `agent.run` | Run sub-agent |

## MCP Protocol

Morpheus supports MCP (Model Context Protocol) for external tool integration.

### Configure MCP Servers

```yaml
mcp:
  servers:
    # stdio transport (local process)
    - name: filesystem
      transport: stdio
      command: npx -y @modelcontextprotocol/server-filesystem .

    # HTTP transport (remote)
    - name: remote
      transport: http
      url: https://example.com/mcp
      sse_url: https://example.com/mcp/sse
      auth_token: ${MCP_TOKEN}
```

### MCP Tool Usage

```bash
# Connect to server
mcp.query({"action": "connect", "name": "filesystem", "command": "npx -y @modelcontextprotocol/server-filesystem ."})

# List tools
mcp.query({"action": "tools", "name": "filesystem"})

# Call MCP tool
mcp.filesystem.read_file({"path": "/path/to/file"})
```

## SOUL

Morpheus loads SOUL (system prompt) from:

1. `~/.config/morpheus/SOUL.md` (user-level)
2. Built-in default SOUL

The built-in SOUL defines Morpheus's core personality:

## Skills

Morpheus provides built-in skills that can be invoked with `@skill`:

```bash
@review Review code changes
@test Recommend tests
@docs Draft documentation
@refactor Suggest improvements
@debug Help diagnose issues
@security Review for vulnerabilities
@git Git workflow guidance
@explain Explain code or concepts
@optimize Performance optimization
```

### Custom Skills

Create custom skills in one of these locations:

- `~/.config/morpheus/skills/` (user-level)
- `.morpheus/skills/` (project-level)

```
~/.config/morpheus/skills/
└── my-skill/
    ├── skill.yaml
    └── prompt.md
```

**skill.yaml Example**:

```yaml
name: my-skill
description: Custom skill description
capabilities:
  - custom
expected_token_cost: 1000
```

**prompt.md Example**:

```
Please help with: {{input}}
```

Skills use lazy loading - custom skills are loaded on demand when first invoked.

## Session Management

Sessions are stored with two backends:

- **SQLite**: `~/.local/share/morpheus/sessions.db` (default)
- **File**: `~/.local/share/morpheus/sessions/`

```
session:
  path: ~/.local/share/morpheus/sessions
  sqlite_path: ~/.local/share/morpheus/sessions.db
  retention: 720h
```

Morpheus automatically compresses context when tokens exceed 60,000.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      CLI / TUI / REST API                        │
├─────────────────────────────────────────────────────────────────┤
│                          Runtime                                 │
│  ┌──────────────┐  ┌─────────────┐  ┌─────────────────────┐   │
│  │ Conversation │  │   Planner   │  │      Coordinator   │   │
│  │   Manager    │  │   (LLM)     │  │ (Multi-Agent Exec) │   │
│  └──────────────┘  └─────────────┘  └─────────────────────┘   │
│  ┌──────────────┐  ┌─────────────┐  ┌─────────────────────┐   │
│  │   Skills     │  │   Plugin    │  │   Agent Registry   │   │
│  │   (Lazy)     │  │   Registry  │  │  (Custom Agents)  │   │
│  └──────────────┘  └─────────────┘  └─────────────────────┘   │
│         │                 │                    │               │
│         └─────────────────┴────────────────────┘               │
│                              │                                 │
│              ┌───────────────┴───────────────┐               │
│              │       Tool Registry            │               │
│              │  (fs/cmd/agent/mcp/skill/...)  │               │
│              └───────────────────────────────┘               │
│                              │                                 │
│              ┌───────────────┴───────────────┐               │
│              │      Policy Engine            │               │
│              │   (Risk Assessment)           │               │
│              └───────────────────────────────┘               │
├─────────────────────────────────────────────────────────────────┤
│                      VSCode Extension                            │
│  (Chat Panel / Inline Edit / Code Explanation)                  │
└─────────────────────────────────────────────────────────────────┘
```

See [docs/architecture.md](docs/architecture.md) for details.

## Development

```bash
# Run from source
go run ./cmd/morpheus

# Run tests
go test ./...

# Build
go build -o morpheus ./cmd/morpheus
```

## CI/CD

GitHub Actions workflow runs on push to `main` and pull requests:

- **CI**: Lint, build, and test
- See [.github/workflows/ci.yml](.github/workflows/ci.yml)

## Tech Stack

- **Backend**: Go 1.25
- **CLI**: Cobra + OpenTUI (Solid.js/Bun)
- **Frontend**: TypeScript + Bun
- **LLM**: OpenAI API compatible (OpenAI, DeepSeek, MiniMax, GLM, Gemini)

## License

MIT
