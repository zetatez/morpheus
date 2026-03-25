# Morpheus CLI (TypeScript)

TypeScript TUI client (Bun runtime) that talks to the Go REST API.

## Quick start

```bash
cd cli
bun install
bun run dev -- --url http://localhost:8080
```

## Options

- `--url` API base URL (default: `http://localhost:8080`)
- `--session` custom session ID
- `--prompt` initial prompt to submit

## Keybindings

- `Enter` send
- `Shift+Enter` newline
- `Ctrl+N` new session
- `Ctrl+Y` copy selection
- `Ctrl+C` exit

## Commands

- `/sessions` list latest 32 sessions (type to filter, Enter to load)
- `/new` create a new session
- `/skills` browse skills
- `/models` browse/select models
- `/connect <url>` switch API base URL

## Streaming

The CLI uses `/repl/stream` when available to render assistant output incrementally.
If streaming is unavailable, it falls back to `/repl`.
