import type { PlanStep, ToolResult } from "./api"

const MAX_PREVIEW = 800
const MAX_MATCHES = 8

function stringify(value: unknown): string {
  if (value == null) return ""
  if (typeof value === "string") return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function formatToolOutput(step: PlanStep, result: ToolResult | null): string {
  const inputs = (step.inputs ?? {}) as Record<string, unknown>
  const thinking = step.description ? `Thinking: ${step.description}` : ""
  const args = formatToolArgsPresentation(step.tool, inputs)
  const header = joinInline(thinking, args.inline)
  const hasFileLine = args.lines.some((line) => line.startsWith("File: "))

  if (!result) {
    return joinLines(header || step.description || "Tool executed", ...args.lines)
  }
  if (!result.success && result.error) {
    return joinLines(header, ...args.lines, `Error: ${result.error}`)
  }
  const data = result.data ?? {}

  if (step.tool === "fs.patch") {
    const patch = stringify(data.patch)
    if (patch) return joinLines(header, ...args.lines, patch)
    const path = stringify(data.path)
    if (hasFileLine) return joinLines(header, ...args.lines, "Patched")
    return joinLines(header, ...args.lines, path ? `Patched ${path}` : "Patched")
  }

  if (step.tool === "fs.write") {
    const path = stringify(data.path)
    const size = stringify(data.size)
    if (size) {
      if (hasFileLine) return joinLines(header, ...args.lines, `Wrote (${size} bytes)`)
      if (path) return joinLines(header, ...args.lines, `Wrote ${path} (${size} bytes)`)
      return joinLines(header, ...args.lines, `Wrote (${size} bytes)`)
    }
    if (hasFileLine) return joinLines(header, ...args.lines, "Wrote")
    if (path) return joinLines(header, ...args.lines, `Wrote ${path}`)
    return joinLines(header, ...args.lines, "Wrote")
  }

  if (step.tool === "fs.read") {
    const path = stringify(data.path)
    const content = stringify(data.content)
    if (!content) return joinLines(header, ...args.lines, path ? `Read ${path}` : "Read file")
    const preview = truncate(content, MAX_PREVIEW)
    return joinLines(header, ...args.lines, path ? `Read ${path}` : "Read file", preview)
  }

  if (step.tool === "cmd.exec") {
    const commandLine = formatCommandLine(stringify(inputs.command))
    const stdout = truncate(stringify(data.stdout), MAX_PREVIEW)
    const stderr = truncate(stringify(data.stderr), MAX_PREVIEW)
    if (stdout) return joinLines(header, ...args.lines, commandLine, stdout)
    if (stderr) return joinLines(header, ...args.lines, commandLine, stderr)
    const exitCode = stringify(data.exit_code)
    return joinLines(
      header,
      ...args.lines,
      commandLine,
      exitCode ? `Command finished (exit ${exitCode})` : "Command finished",
    )
  }

  if (Array.isArray((data as { matches?: unknown }).matches)) {
    const matches = (data as { matches: unknown[] }).matches
    const list = matches.slice(0, MAX_MATCHES).map((item) => stringify(item))
    const suffix = matches.length > MAX_MATCHES ? " …" : ""
    return joinLines(header, ...args.lines, `Matches (${matches.length}): ${list.join(", ")}${suffix}`)
  }

  const preferred =
    stringify(data.stdout) ||
    stringify(data.content) ||
    stringify(data.body) ||
    stringify(data.tree) ||
    stringify(data.text) ||
    stringify(data.patch)
  if (preferred) return joinLines(header, ...args.lines, truncate(preferred, MAX_PREVIEW))
  return joinLines(header, ...args.lines, truncate(stringify(data), MAX_PREVIEW))
}

function formatToolArgsPresentation(
  tool: string,
  inputs: Record<string, unknown>,
): { inline: string; lines: string[] } {
  const keys = Object.keys(inputs ?? {})
  if (keys.length === 0) return { inline: "", lines: [] }

  const get = (name: string) => inputs[name]
  const kv = (name: string) => `${name}=${inlineValue(get(name))}`

  const rawPath = (get("path") ?? get("filePath")) as unknown

  switch (tool) {
    case "fs.read":
      {
        const p = inlineValue(rawPath)
        return { inline: p, lines: [] }
      }
    case "fs.create":
    case "fs.write":
      {
        const p = inlineValue(rawPath)
        const content = get("content")
        const lines: string[] = []
        if (p) lines.push(`File: ${p}`)
        if (content !== undefined) lines.push(omitValue("Content", content))
        return { inline: "", lines }
      }
    case "fs.patch": {
      const p = inlineValue(rawPath)
      const patch = get("patch")
      const lines: string[] = []
      if (p) lines.push(`File: ${p}`)
      if (patch !== undefined) lines.push(omitValue("Patch", patch))
      return { inline: "", lines }
    }
    case "fs.glob": {
      const pat = inlineValue(get("pattern"))
      const p = inlineValue(get("path"))
      if (pat && p) return { inline: `${pat} ${kv("path")}`, lines: [] }
      return { inline: pat || kv("path"), lines: [] }
    }
    case "fs.grep": {
      const q = get("query") !== undefined ? kv("query") : ""
      const pat = get("pattern") !== undefined ? kv("pattern") : ""
      const p = get("path") !== undefined ? kv("path") : ""
      const inc = get("include") !== undefined ? kv("include") : ""
      return { inline: [q, pat, inc, p].filter(Boolean).join(" "), lines: [] }
    }
    case "cmd.exec": {
      const cmd = get("command") !== undefined ? kv("command") : ""
      const wd = get("workdir") !== undefined ? kv("workdir") : ""
      const t = get("timeout") !== undefined ? kv("timeout") : ""
      return { inline: [cmd, wd, t].filter(Boolean).join(" "), lines: [] }
    }
    case "web.fetch": {
      const url = get("url") !== undefined ? kv("url") : ""
      const fmt = get("format") !== undefined ? kv("format") : ""
      const t = get("timeout") !== undefined ? kv("timeout") : ""
      return { inline: [url, fmt, t].filter(Boolean).join(" "), lines: [] }
    }
    default:
      return {
        inline: keys
          .sort()
          .slice(0, 6)
          .map((k) => `${k}=${inlineValue(inputs[k])}`)
          .join(" "),
        lines: [],
      }
  }
}

function omitValue(label: string, value: unknown): string {
  if (typeof value === "string") {
    const bytes = new TextEncoder().encode(value).length
    return `${label}: ... (${bytes} bytes)`
  }
  if (value == null) return `${label}: ...`
  try {
    const text = JSON.stringify(value)
    const bytes = new TextEncoder().encode(text).length
    return `${label}: ... (${bytes} bytes)`
  } catch {
    return `${label}: ...`
  }
}

function inlineValue(value: unknown): string {
  if (value == null) return ""
  if (typeof value === "string") {
    const trimmed = value.replace(/\s+/g, " ").trim()
    if (trimmed.length === 0) return ""
    const short = trimmed.length > 200 ? `${trimmed.slice(0, 200)}...` : trimmed
    // keep file paths / short tokens readable without key= quoting
    if (/^[A-Za-z0-9._\-/~]+$/.test(short)) return short
    return JSON.stringify(short)
  }
  try {
    const text = JSON.stringify(value)
    return text.length > 200 ? `${text.slice(0, 200)}...` : text
  } catch {
    return String(value)
  }
}

function joinInline(a: string, b: string): string {
  const left = (a ?? "").trim()
  const right = (b ?? "").trim()
  if (!left) return right
  if (!right) return left
  return `${left} ${right}`
}

function truncate(value: string, max: number): string {
  if (value.length <= max) return value
  return `${value.slice(0, max)}\n...`
}

function joinLines(...parts: string[]): string {
  return parts.filter((part) => part && part.trim().length > 0).join("\n")
}

function formatCommandLine(command: string): string {
  if (!command) return ""
  const lines = command
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
  if (lines.length === 0) return ""
  if (lines.length === 1) return `Command: ${lines[0]}`
  return `Command:\n${lines.map((line) => `  ${line}`).join("\n")}`
}
