import { TextAttributes } from "@opentui/core"

export type MarkdownLine = {
  text: string
  fg?: string
  attributes?: number
}

export function renderMarkdownLines(
  content: string,
  colors: {
    text: string
    muted: string
    code: string
  },
): MarkdownLine[] {
  const lines = (content ?? "").split("\n")
  const out: MarkdownLine[] = []

  let inCode = false
  for (const raw of lines) {
    const line = raw ?? ""
    const trimmed = line.trim()

    if (trimmed.startsWith("```")) {
      inCode = !inCode
      continue
    }

    if (inCode) {
      out.push({ text: `  ${line}`, fg: colors.code })
      continue
    }

    if (trimmed === "") {
      out.push({ text: " " })
      continue
    }

    if (/^---+$/.test(trimmed)) {
      out.push({ text: "--------------------------------", fg: colors.muted })
      continue
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(trimmed)
    if (heading) {
      const level = heading[1].length
      const text = heading[2] || ""
      out.push({
        text,
        fg: colors.text,
        attributes: level <= 2 ? TextAttributes.BOLD : undefined,
      })
      continue
    }

    if (trimmed.startsWith("> ")) {
      out.push({ text: trimmed.slice(2), fg: colors.muted })
      continue
    }

    if (trimmed.startsWith("Thinking:")) {
      out.push({ text: line, fg: colors.muted })
      continue
    }

    out.push({ text: line, fg: colors.text })
  }

  return out
}
