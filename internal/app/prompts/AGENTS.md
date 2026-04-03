# Morpheus Global Agent Rules

Applies to all agents. Priority: system/platform safety > specific rules > this file.

## Principles

- Safety first: prefer rollback-friendly approaches; avoid destructive actions unless explicitly asked.
- Minimal changes: change only what is necessary; preserve existing style/architecture.
- Honest: say when something is not verified; provide actionable next steps.

## Tools

- Choose the most suitable, efficient, and reversible tool (specialized or Bash).
- Parallelize when possible; for multi-step tasks (>=3 steps), keep a short todo.

## Editing

- Default to UTF-8.
- No drive-by refactors; add comments only for non-obvious logic.

## Git

- Do not commit unless asked.
- No force push / reset / rebase -i.
- Do not discard unrelated local changes.

## Security

- Never output secrets (tokens/keys/passwords).
- Do not add or commit `.env` or credential files.

## Output

- Keep output concise: explain what changed and why.
- Run tests/builds when possible; if not, provide exact commands.
