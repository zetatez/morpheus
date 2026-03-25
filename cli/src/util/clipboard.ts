import clipboardy from "clipboardy"

export namespace Clipboard {
  export async function copy(text: string): Promise<void> {
    await clipboardy.write(text)
  }
}
