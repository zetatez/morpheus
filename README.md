# Morpheus

Morpheus is a local AI agent runtime with a configurable SOUL prompt, tool execution, session persistence, and a TUI client.

## Highlights

- External `SOUL.md` support with built-in fallback
- Workspace-scoped file and command tools
- Session storage and model configuration
- Go backend with optional TypeScript CLI

## Configuration

Morpheus reads config from your config file and applies these defaults:

- `workspace_root`: defaults to the directory where Morpheus starts
- `SOUL.md`: Morpheus first looks for `~/.config/morph/SOUL.md`
- logs: default to `~/.local/share/morph/logs/morph.log`
- sessions: default to `~/.local/share/morph/sessions`
- knowledge base: default to `~/.config/morph/knowledge_base`
- if external `SOUL.md` is missing, Morpheus falls back to the built-in default SOUL

This means:

- launching Morpheus inside a project makes that directory the default workspace root
- if you want a custom agent soul, create `~/.config/morph/SOUL.md`
- if you do nothing, Morpheus uses the internal default SOUL

## SOUL Priority

Morpheus loads SOUL in this order:

1. `~/.config/morph/SOUL.md`
2. built-in default SOUL embedded in the binary

If the external SOUL exists, the built-in SOUL is not used.

## Run

```bash
go run .
```

Run from the project directory you want Morpheus to treat as the workspace root, unless you explicitly set `workspace_root` in config.

## CLI

The TUI client lives in `cli/`.

```bash
cd cli
bun install
bun run dev -- --url http://localhost:8080
```

See `cli/README.md` for client usage.
