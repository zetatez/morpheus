import { createHash } from "crypto"

export function sha256Hex(content: string): string {
  return createHash("sha256").update(content).digest("hex")
}

export function formatToken(count: number): string {
  if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`
  if (count >= 1000) return `${(count / 1000).toFixed(1)}K`
  return count.toString()
}


