export type MarkdownLine = {
  text?: string
  fg?: string
  attributes?: number
}

export type RenderColors = {
  text: string
  muted: string
  code: string
  success?: string
  output?: string
  file?: string
  accent?: string
  error?: string
  thinking?: string
}

export declare function renderMarkdownLines(
  content: string,
  colors: RenderColors
): MarkdownLine[]