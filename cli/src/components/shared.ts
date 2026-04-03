import type { Entry } from "../types"
import type { TodoItem } from "../api"

export type ModalKind = "sessions" | "runs" | "timeline" | "skills" | "models" | "connect" | "modelToken" | "help" | "confirm"

export type ModalItem = {
  id: string
  title: string
  subtitle?: string
  meta?: string
  status?: string
  provider?: string
  model?: string
  hasToken?: boolean
}

export type AttachmentInput = {
  path?: string
  url?: string
  name?: string
  kind?: string
  mime?: string
}

export function formatToken(count: number): string {
  if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`
  if (count >= 1000) return `${(count / 1000).toFixed(1)}K`
  return count.toString()
}

export function formatTodoBlock(todos: TodoItem[]): string {
  if (todos.length === 0) return ""
  return `${todos.map((todo) => {
    const mark = todo.status === "completed" ? "[x]" : todo.status === "in_progress" ? "[•]" : todo.status === "failed" ? "[!]" : todo.status === "cancelled" ? "[-]" : "[ ]"
    const suffix = todo.tool ? ` (${todo.tool})` : ""
    const note = todo.note ? ` - ${todo.note}` : ""
    return `${mark} ${todo.content}${suffix}${note}`
  }).join("\n")}`
}

export function formatSessionID(): string {
  const now = new Date()
  const pad = (value: number, size = 2) => String(value).padStart(size, "0")
  const date = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}`
  const time = `${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`
  const ms = pad(now.getMilliseconds(), 3)
  return `${date}-${time}-${ms}`
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

export function todoLineColor(todo: TodoItem, pulse: boolean, theme: Record<string, string>): string {
  if (todo.status === "completed") return theme.todoDone ?? theme.success
  if (todo.status === "failed") return theme.todoFailed ?? theme.error
  if (todo.status === "cancelled") return theme.todoCancelled ?? theme.muted
  if (todo.status === "in_progress") return pulse ? theme.primary ?? theme.text : theme.todoActive ?? theme.text
  return theme.todoPending ?? theme.muted
}
