export type Entry = {
  id: string
  role: "user" | "assistant" | "tool" | "error"
  title?: string
  content: string
}
