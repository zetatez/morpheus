import { Clipboard } from "./clipboard"

type Renderer = {
  getSelection: () => { getSelectedText: () => string } | null
  clearSelection: () => void
}

export namespace Selection {
  export async function copy(renderer: Renderer): Promise<boolean> {
    const text = renderer.getSelection()?.getSelectedText()
    if (!text) return false
    await Clipboard.copy(text)
    renderer.clearSelection()
    return true
  }
}
