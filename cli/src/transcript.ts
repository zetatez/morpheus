import type { Entry } from "./types"

type TranscriptMsg = { role: string; content: string }

export function parseTranscriptToEntries(raw: string): Entry[] {
  const messages = parseTranscript(raw)
  const entries: Entry[] = []

  for (const msg of messages) {
    const role = msg.role.toLowerCase().trim()
    if (role === "user") {
      entries.push({ id: crypto.randomUUID(), role: "user", content: msg.content.trimEnd() })
      continue
    }

    if (role === "tool") {
      entries.push({ id: crypto.randomUUID(), role: "tool", title: "Tool", content: msg.content.trimEnd() })
      continue
    }

    // Default to assistant.
    const assistantContent = msg.content.trimEnd()
    const toolBlocks = splitToolBlocks(assistantContent)
    if (toolBlocks.length === 0) {
      entries.push({ id: crypto.randomUUID(), role: "assistant", content: assistantContent })
      continue
    }

    for (const block of toolBlocks) {
      if (block.kind === "assistant") {
        if (block.content.trim() !== "") {
          entries.push({ id: crypto.randomUUID(), role: "assistant", content: block.content.trimEnd() })
        }
      } else {
        entries.push({
          id: crypto.randomUUID(),
          role: "tool",
          title: block.title,
          content: block.content.trimEnd(),
        })
      }
    }
  }

  return entries
}

function parseTranscript(raw: string): TranscriptMsg[] {
  const text = (raw ?? "").replace(/\r\n/g, "\n")
  const lines = text.split("\n")

  const out: TranscriptMsg[] = []
  let currentRole: string | null = null
  let buf: string[] = []

  const flush = () => {
    if (!currentRole) return
    const content = stripTrailingSeparators(buf.join("\n"))
    out.push({ role: currentRole, content })
    currentRole = null
    buf = []
  }

  for (const line of lines) {
    const header = /^##\s+([^|]+)\|/.exec(line)
    if (header) {
      flush()
      currentRole = header[1].trim()
      continue
    }
    if (line.trim() === "---") {
      flush()
      continue
    }
    if (currentRole) buf.push(line)
  }
  flush()
  return out
}

function stripTrailingSeparators(text: string): string {
  return text.replace(/\n+$/g, "")
}

function splitToolBlocks(content: string): Array<
  | { kind: "assistant"; content: string }
  | { kind: "tool"; title: string; content: string }
> {
  const lines = (content ?? "").split("\n")
  const blocks: Array<{ kind: "assistant" | "tool"; title?: string; lines: string[] }> = []

  const push = (kind: "assistant" | "tool", title?: string) => {
    blocks.push({ kind, title, lines: [] })
  }

  push("assistant")

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const m = /^###\s+Tool:\s+(.+)$/.exec(line.trim())
    if (m) {
      const full = m[1]
      const name = full.split("(")[0].trim()
      push("tool", name ? `Tool ${name}` : "Tool")
      continue
    }
    blocks[blocks.length - 1].lines.push(line)
  }

  return blocks
    .map((b) => {
      if (b.kind === "assistant") return { kind: "assistant" as const, content: b.lines.join("\n") }
      return { kind: "tool" as const, title: b.title ?? "Tool", content: b.lines.join("\n") }
    })
    .filter((b) => b.kind === "tool" || b.content.trim() !== "")
}
