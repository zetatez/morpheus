import { Clipboard } from "./clipboard"

type Renderer = {
  getSelection: () => { getSelectedText: () => string } | null
  clearSelection: () => void
}

function stripAnsi(text: string): string {
  return text
    .replace(/\x1b\[[0-9;]*[a-zA-Z=<?>]*/g, "")
    .replace(/\x1b\][^\x07]*\x07/g, "")
    .replace(/\x1b[()][AB012]/g, "")
}

export namespace Selection {
  export async function copy(renderer: Renderer): Promise<boolean> {
    const text = renderer.getSelection()?.getSelectedText()
    if (!text) return false
    await Clipboard.copy(stripAnsi(text))
    renderer.clearSelection()
    return true
  }
}
