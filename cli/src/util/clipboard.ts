import clipboardy from "clipboardy"

function writeOsc52(text: string): void {
  if (!process.stdout.isTTY) return
  const base64 = Buffer.from(text).toString("base64")
  const osc52 = `\x1b]52;c;${base64}\x07`
  const passthrough = process.env.TMUX || process.env.STY
  const sequence = passthrough ? `\x1bPtmux;\x1b${osc52}\x1b\\` : osc52
  process.stdout.write(sequence)
}

export namespace Clipboard {
  export async function copy(text: string): Promise<void> {
    writeOsc52(text)
    await clipboardy.write(text)
  }
}
