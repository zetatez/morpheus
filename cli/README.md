# Morpheus CLI (TypeScript)

TypeScript TUI client (Bun runtime) that talks to the Go REST API.

## Quick start

```bash
cd cli
bun install
bun run dev -- --url http://localhost:8080
```

Or run the built binary:

```bash
./morpheus repl
```

## Options

- `--url` API base URL (default: `http://localhost:8080`)
- `--session` custom session ID
- `--prompt` initial prompt to submit
- `--model` specify model (e.g., gpt-4o, deepseek-chat)
- `--plan` plan mode (read-only)
- `--config` path to config file

## Keybindings

- `Enter` send message
- `Shift+Enter` newline
- `Ctrl+N` new session
- `Ctrl+Y` copy selection
- `Ctrl+C` exit
- `Ctrl+L` clear screen

## Commands

- `/sessions` list latest 32 sessions (type to filter, Enter to load)
- `/new` create a new session
- `/skills` browse skills
- `/models` browse/select models
- `/connect <url>` switch API base URL
- `/clear` clear session

## Streaming

The CLI uses `/repl/stream` when available to render assistant output incrementally.
If streaming is unavailable, it falls back to `/repl`.
