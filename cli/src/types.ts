export type Entry = {
  id: string
  role: "user" | "assistant" | "tool" | "error" | "system" | "queue" | "todo"
  title?: string
  content: string
  kind?: "thinking" | "summary" | "queued" | "default"
}
