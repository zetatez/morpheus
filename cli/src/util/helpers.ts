import { createHash } from "crypto"

export function sha256Hex(content: string): string {
  return createHash("sha256").update(content).digest("hex")
}

export function formatToken(count: number): string {
  if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`
  if (count >= 1000) return `${(count / 1000).toFixed(1)}K`
  return count.toString()
}

export type AttachmentInput = {
  path?: string
  url?: string
  name?: string
  kind?: string
  mime?: string
}

export function parseAttachmentPaths(text: string): AttachmentInput[] {
  const paths: AttachmentInput[] = []
  const lines = text.split("\n")
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed) continue
    if (trimmed.startsWith("!")) {
      const url = trimmed.slice(1).trim()
      if (url.startsWith("http://") || url.startsWith("https://")) {
        paths.push({ url, kind: "url" })
      }
    } else if (trimmed.startsWith("/") || trimmed.startsWith(".")) {
      paths.push({ path: trimmed, kind: "file" })
    }
  }
  return paths
}
