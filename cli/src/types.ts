export type Entry = {
  id: string
  role: "user" | "assistant" | "tool" | "error" | "system"
  title?: string
  content: string
  kind?: "thinking" | "summary" | "default"
}
