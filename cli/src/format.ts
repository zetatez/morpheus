import type { PlanStep, ToolResult } from "./api"

const MAX_PREVIEW = 800

function stringify(value: unknown): string {
  if (value == null) return ""
  if (typeof value === "string") return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function inlineValue(value: unknown): string {
  if (value == null) return ""
  if (typeof value === "string") {
    const trimmed = value.replace(/\s+/g, " ").trim()
    if (trimmed.length === 0) return ""
    const short = trimmed.length > 200 ? `${trimmed.slice(0, 200)}...` : trimmed
    if (/^[A-Za-z0-9._\-/~]+$/.test(short)) return short
    return short
  }
  try {
    const text = JSON.stringify(value)
    return text.length > 200 ? `${text.slice(0, 200)}...` : text
  } catch {
    return String(value)
  }
}

function joinLines(...parts: (string | undefined)[]): string {
  return parts.filter(Boolean).join("\n")
}

function formatToolArgsPresentation(
  tool: string,
  inputs: Record<string, unknown>,
): { inline: string } {
  const keys = Object.keys(inputs ?? {})
  if (keys.length === 0) return { inline: "" }

  const get = (name: string) => {
    if (inputs[name] !== undefined) return inputs[name]
    return undefined
  }
  const t = tool.toLowerCase()

  switch (t) {
    case "fs.read":
    case "read": {
      const p = inlineValue(inputs.path ?? inputs.filePath ?? inputs.file_path)
      return { inline: p || keys.map(k => `${k}=${inlineValue(inputs[k])}`).join(" ") }
    }
    case "fs.glob":
    case "glob": {
      const pat = inlineValue(get("pattern"))
      const p = inlineValue(get("path"))
      return { inline: pat || p || keys.map(k => `${k}=${inlineValue(inputs[k])}`).join(" ") }
    }
    case "fs.grep":
    case "grep": {
      const pat = inlineValue(get("pattern") ?? get("query"))
      const p = inlineValue(get("path"))
      return { inline: pat || p || keys.map(k => `${k}=${inlineValue(inputs[k])}`).join(" ") }
    }
    case "cmd.exec":
    case "exec":
    case "bash":
    case "shell": {
      const cmd = inlineValue(get("command"))
      return { inline: cmd || keys.map(k => `${k}=${inlineValue(inputs[k])}`).join(" ") }
    }
    case "web.fetch":
    case "fetch": {
      const url = inlineValue(get("url"))
      return { inline: url || keys.map(k => `${k}=${inlineValue(inputs[k])}`).join(" ") }
    }
    case "fs.write":
    case "write":
    case "fs.create":
    case "create":
    case "fs.edit":
    case "edit": {
      const p = inlineValue(inputs.path ?? inputs.filePath ?? inputs.file_path)
      return { inline: p || keys.map(k => `${k}=${inlineValue(inputs[k])}`).join(" ") }
    }
    case "fs.patch":
    case "patch": {
      const p = inlineValue(inputs.path ?? inputs.filePath ?? inputs.file_path)
      return { inline: p || keys.map(k => `${k}=${inlineValue(inputs[k])}`).join(" ") }
    }
    default: {
      return {
        inline: keys
          .slice(0, 6)
          .map((k) => `${k}=${inlineValue(inputs[k])}`)
          .join(" "),
      }
    }
  }
}

function getToolIcon(tool: string): string {
  const t = tool.toLowerCase()
  if (t === "cmd.exec" || t === "exec" || t === "bash" || t === "shell") return "$"
  if (t === "fs.read" || t === "read") return "→"
  if (t === "fs.write" || t === "write" || t === "fs.create" || t === "create" || t === "fs.edit" || t === "edit" || t === "fs.patch" || t === "patch") return "←"
  if (t === "fs.glob" || t === "glob" || t === "fs.grep" || t === "grep" || t === "web.fetch" || t === "fetch") return "✱"
  return "⚙"
}

function formatToolName(tool: string): string {
  const t = tool.toLowerCase()
  if (t === "cmd.exec" || t === "exec" || t === "bash" || t === "shell") return "bash"
  if (t === "fs.read" || t === "read") return "read"
  if (t === "fs.glob" || t === "glob") return "glob"
  if (t === "fs.grep" || t === "grep") return "grep"
  if (t === "fs.write" || t === "write") return "write"
  if (t === "fs.create" || t === "create") return "create"
  if (t === "fs.edit" || t === "edit") return "edit"
  if (t === "fs.patch" || t === "patch") return "patch"
  if (t === "web.fetch" || t === "fetch") return "fetch"
  return tool
}

export function formatToolOutput(step: PlanStep, result: ToolResult | null): string {
  const inputs = (step.inputs ?? {}) as Record<string, unknown>
  const tool = step.tool || ""
  const args = formatToolArgsPresentation(tool, inputs)
  const icon = getToolIcon(tool)
  const name = formatToolName(tool)

  if (!result) {
    const argsDisplay = args.inline ? ` ${args.inline}` : ""
    const displayName = name || tool
    if (!displayName) return ""
    return `${icon} ${displayName}${argsDisplay}`.trim()
  }

  if (!result.success && result.error) {
    const displayName = args.inline ? name : (tool || name)
    if (!displayName && !args.inline) return `Error: ${result.error}`
    return `${icon} ${displayName} ${args.inline}\nError: ${result.error}`.trim()
  }

  const data = result.data ?? {}
  const t = tool.toLowerCase()

  if (t === "cmd.exec" || t === "exec" || t === "bash" || t === "shell") {
    const command = formatShellCommand(stringify(inputs.command))
    const stdout = stringify(data.stdout)
    const stderr = stringify(data.stderr)
    const exitCode = stringify(data.exit_code)
    const exitInfo = exitCode && exitCode !== "0" ? ` [exit ${exitCode}]` : ""
    const cmdLine = `$ ${command}${exitInfo}`
    if (stdout || stderr) {
      return joinLines(cmdLine, stdout, stderr ? `[stderr] ${stderr}` : undefined)
    }
    return cmdLine
  }

  if (t === "fs.patch" || t === "patch") {
    const patch = stringify(data.patch || inputs.patch)
    return joinLines(`← ${name} ${args.inline}`, patch)
  }

  if (t === "fs.write" || t === "write" || t === "fs.create" || t === "create" || t === "fs.edit" || t === "edit") {
    const path = stringify(data.path || inputs.path || inputs.filePath)
    const content = inputs.content as string | undefined
    return joinLines(`← ${name} ${path}`, content ? content.slice(0, 500) : undefined)
  }

  if (t === "fs.read" || t === "read") {
    const path = stringify(data.path)
    const total = stringify(data.total_lines)
    const suffix = total ? ` (${total} lines)` : ""
    return `→ ${name} ${path}${suffix}`
  }

  if (t === "fs.glob" || t === "glob") {
    const count = Array.isArray((data as { matches?: unknown }).matches)
      ? (data as { matches: unknown[] }).matches.length
      : 0
    const suffix = count > 0 ? ` (${count} matches)` : ""
    return `✱ ${name} ${args.inline}${suffix}`
  }

  if (t === "fs.grep" || t === "grep") {
    const count = Array.isArray((data as { matches?: unknown }).matches)
      ? (data as { matches: unknown[] }).matches.length
      : 0
    const suffix = count > 0 ? ` (${count} matches)` : ""
    return `✱ ${name} ${args.inline}${suffix}`
  }

  if (t === "web.fetch" || t === "fetch") {
    const status = stringify(data.status)
    const body = stringify(data.body || data.text)
    const suffix = status ? ` [${status}]` : ""
    return `✱ ${name} ${args.inline}${suffix}${body ? `\n${body.slice(0, 500)}` : ""}`
  }

  if (!args.inline && !tool) return ""
  return `${icon} ${name} ${args.inline}`
}

function formatShellCommand(cmd: string): string {
  if (cmd.length > 120) {
    return cmd.slice(0, 120) + "..."
  }
  return cmd
}