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



export function todoLineColor(todo: TodoItem, pulse: boolean, theme: Record<string, string>): string {
  if (todo.status === "completed") return theme.todoDone ?? theme.success
  if (todo.status === "failed") return theme.todoFailed ?? theme.error
  if (todo.status === "cancelled") return theme.todoCancelled ?? theme.muted
  if (todo.status === "in_progress") return pulse ? theme.primary ?? theme.text : theme.todoActive ?? theme.text
  return theme.todoPending ?? theme.muted
}
