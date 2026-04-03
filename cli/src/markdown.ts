import { TextAttributes } from "@opentui/core"

export type MarkdownLine = {
  text: string
  fg?: string
  attributes?: number
}

type CodeBlockInfo = {
  lang: string
  content: string[]
}

type TableInfo = {
  rows: string[][]
  colWidths: number[]
  colCount: number
}

export function renderMarkdownLines(
  content: string,
  colors: {
    text: string
    muted: string
    code: string
    success?: string
    output?: string
    file?: string
    accent?: string
    error?: string
    thinking?: string
  },
): MarkdownLine[] {
  const lines = (content ?? "").split("\n")
  const out: MarkdownLine[] = []

  let inCode = false
  let codeLang = ""
  let codeBuffer: string[] = []
  let inThinking = false
  let tableState: TableInfo | null = null

  const flushTable = () => {
    if (tableState) {
      out.push(...renderTable(tableState, colors))
      tableState = null
    }
  }

  for (const raw of lines) {
    const line = raw ?? ""
    const trimmed = line.trim()

    if (trimmed.startsWith("```")) {
      flushTable()
      if (inCode) {
        flushCodeBlock(out, { lang: codeLang, content: codeBuffer }, colors)
        inCode = false
        codeLang = ""
        codeBuffer = []
      } else {
        inCode = true
        codeLang = trimmed.slice(3).trim().toLowerCase()
        codeBuffer = []
      }
      continue
    }

    if (inCode) {
      codeBuffer.push(line)
      continue
    }

    const thinkingLine = normalizeThinkingLine(trimmed)
    if (thinkingLine) {
      flushTable()
      inThinking = true
      const rest = thinkingLine.slice("Thinking:".length).trimStart()
      const label = "Thinking:"
      out.push({ text: label, fg: colors.accent ?? colors.code, attributes: TextAttributes.BOLD | TextAttributes.ITALIC })
      if (rest) {
        out.push({ text: rest, fg: colors.thinking ?? colors.muted, attributes: TextAttributes.ITALIC })
      }
      continue
    }

    const summaryLine = normalizeSummaryLine(trimmed)
    if (summaryLine) {
      flushTable()
      const rest = summaryLine.slice("Summary:".length).trimStart()
      const label = "Summary:"
      out.push({ text: label, fg: colors.accent ?? colors.code, attributes: TextAttributes.BOLD })
      if (rest) {
        out.push({ text: rest, fg: colors.text })
      }
      continue
    }

    if (inThinking) {
      if (trimmed === "") {
        inThinking = false
        out.push({ text: " " })
        continue
      }
      if (isThinkingCommand(trimmed)) {
        out.push({ text: `  ${trimmed}`, fg: colors.code })
        continue
      }
      out.push({ text: line, fg: colors.thinking ?? colors.muted })
      continue
    }

    if (trimmed === "") {
      flushTable()
      out.push({ text: " " })
      continue
    }

    const checkbox = /^[-*]\s+\[([ xX~\-])\]\s+(.*)$/.exec(trimmed)
    if (checkbox) {
      flushTable()
      const status = checkbox[1]
      const label = checkbox[2] ?? ""
      const normalized = `[${status}] ${label}`.trimEnd()
      if (status === "x" || status === "X") {
        out.push({ text: normalized, fg: colors.muted })
        continue
      }
      if (status === "-" || status === "~") {
        out.push({ text: normalized, fg: colors.accent ?? colors.code, attributes: TextAttributes.BOLD })
        continue
      }
      out.push({ text: normalized, fg: colors.muted })
      continue
    }

    if (/^---+$/.test(trimmed)) {
      continue
    }

    const list = /^[-*]\s+(.*)$/.exec(trimmed)
    if (list) {
      flushTable()
      out.push({ text: `- ${list[1]}`, fg: colors.text })
      continue
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(trimmed)
    if (heading) {
      flushTable()
      const level = heading[1].length
      const text = heading[2] || ""
      const lowered = text.toLowerCase().trim()
      const fg =
        lowered === "error" && colors.error
          ? colors.error
          : lowered === "output"
            ? colors.accent ?? colors.code
            : colors.text
      out.push({
        text,
        fg,
        attributes: level <= 2 ? TextAttributes.BOLD : undefined,
      })
      continue
    }

    if (trimmed.startsWith("> ")) {
      flushTable()
      out.push({ text: trimmed.slice(2), fg: colors.muted })
      continue
    }

    if (trimmed.startsWith("|")) {
      const cells = trimmed.split("|").filter(c => c !== "").map(c => c.trim())
      if (cells.length >= 2) {
        if (!tableState) {
          tableState = { rows: [], colWidths: [], colCount: cells.length }
        }
        tableState.rows.push(cells)
        for (let i = 0; i < cells.length; i++) {
          if (i >= tableState.colWidths.length) {
            tableState.colWidths.push(0)
          }
          tableState.colWidths[i] = Math.max(tableState.colWidths[i], cells[i].length)
        }
        continue
      }
    }

    flushTable()
    out.push({ text: line, fg: colors.text })
  }

  flushTable()
  return out
}

function flushCodeBlock(
  out: MarkdownLine[],
  block: CodeBlockInfo,
  colors: {
    text: string
    muted: string
    code: string
    success?: string
    output?: string
    file?: string
    accent?: string
    error?: string
  },
): void {
  const lang = block.lang
  const lines = block.content
  if (lang === "bash") {
    for (const line of lines) {
      const trimmed = line.trimStart()
      const isCommand = trimmed.startsWith("$")
      out.push({ text: `  ${line}`, fg: isCommand ? colors.success ?? colors.code : colors.output ?? colors.success ?? colors.code })
    }
    return
  }

  if (lang === "read" || lang === "write" || lang === "patch" || lang === "diff") {
    for (const line of lines) {
      out.push({ text: `  ${line}`, fg: colors.file ?? colors.accent ?? colors.code })
    }
    return
  }

  const fg = lang === "stderr" && colors.error ? colors.error : colors.code
  for (const line of lines) {
    out.push({ text: `  ${line}`, fg })
  }
}

function normalizeThinkingLine(trimmed: string): string | null {
  if (trimmed.startsWith("Thinking:")) return trimmed
  if (trimmed.toLowerCase().startsWith("thinking:")) {
    const rest = trimmed.slice("thinking:".length).trimStart()
    return rest ? `Thinking: ${rest}` : "Thinking:"
  }
  return null
}

function normalizeSummaryLine(trimmed: string): string | null {
  if (trimmed.startsWith("Summary:")) return trimmed
  if (trimmed.toLowerCase().startsWith("summary:")) {
    const rest = trimmed.slice("summary:".length).trimStart()
    return rest ? `Summary: ${rest}` : "Summary:"
  }
  return null
}

function isThinkingCommand(trimmed: string): boolean {
  if (trimmed.startsWith("$ ") || trimmed.startsWith("$")) return true
  if (trimmed.startsWith("* ") || trimmed.startsWith("- ")) return true
  if (trimmed.startsWith("-> ")) return true
  return false
}

function renderTable(
  table: TableInfo,
  colors: { text: string; muted: string; accent?: string },
): MarkdownLine[] {
  if (table.rows.length === 0) return []

  const out: MarkdownLine[] = []
  const totalWidth = table.colWidths.reduce((a, b) => a + b, 0) + table.colCount * 3 + 1
  const divider = "─".repeat(totalWidth - 2)

  const padRow = (cells: string[], isHeader: boolean) => {
    const padded = cells.map((cell, i) => {
      const width = table.colWidths[i] ?? cell.length
      return cell.padEnd(width)
    })
    return "│ " + padded.join(" │ ") + " │"
  }

  out.push({ text: "┌" + divider + "┐", fg: colors.accent ?? colors.muted })

  const header = table.rows[0]
  out.push({ text: padRow(header, true), fg: colors.accent ?? colors.text, attributes: TextAttributes.BOLD })

  if (table.rows.length > 1) {
    out.push({ text: "├" + divider + "┤", fg: colors.accent ?? colors.muted })
  }

  for (let i = 1; i < table.rows.length; i++) {
    out.push({ text: padRow(table.rows[i], false), fg: colors.text })
  }

  out.push({ text: "└" + divider + "┘", fg: colors.accent ?? colors.muted })

  return out
}
