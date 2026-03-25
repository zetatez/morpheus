import { createSignal, onCleanup, For as SolidFor, Show as SolidShow, createMemo } from "solid-js"

export const TextAttributes = {
  NONE: 0,
  BOLD: 1,
  ITALIC: 3,
  UNDERLINE: 4,
  STRIKETHROUGH: 9,
} as const

export type TextAttribute = typeof TextAttributes[keyof typeof TextAttributes]

export interface TerminalDimensions {
  width: number
  height: number
}

export interface KeyEvent {
  name: string
  ctrl: boolean
  shift: boolean
  alt: boolean
  meta: boolean
  option: boolean
  preventDefault: () => void
}

export interface PasteEvent {
  text: string
  preventDefault: () => void
}

export interface ConsoleOptions {
  keyBindings?: Array<{
    name: string
    ctrl?: boolean
    shift?: boolean
    alt?: boolean
    meta?: boolean
    action?: string
  }>
  onCopySelection?: (text: string) => void
}

export interface InputRenderable {
  value: string
  focused: boolean
  focus: () => void
}

export interface TextareaRenderable {
  plainText: string
  cursorOffset: number
  focused: boolean
  setText: (text: string) => void
  clear: () => void
  focus: () => void
}

export interface ScrollBoxRenderable {
  scrollBy: (amount: number) => void
}

export interface TextareaInput {
  plainText: string
  setText: (text: string) => void
  insertText: (text: string) => void
  deleteBackward: () => void
  moveCursor: (dir: "left" | "right") => void
  getCursorLine: () => number
  getCursorCol: () => number
  setCursorLineCol: (line: number, col: number) => void
  clear: () => void
  focus: () => void
}

export function createTextareaInput(initialText = ""): TextareaInput {
  let lines = initialText.split("\n")
  let cursorLine = 0
  let cursorCol = 0
  
  return {
    get plainText() {
      return lines.join("\n")
    },
    setText(text: string) {
      lines = text.split("\n")
      cursorLine = Math.min(cursorLine, lines.length - 1)
      cursorCol = Math.min(cursorCol, lines[cursorLine].length)
    },
    insertText(text: string) {
      const currentLine = lines[cursorLine]
      const before = currentLine.slice(0, cursorCol)
      const after = currentLine.slice(cursorCol)
      if (text.includes("\n")) {
        const parts = text.split("\n")
        lines[cursorLine] = before + parts[0]
        const newLines = parts.slice(1).map((p, i) => i === parts.length - 2 ? p + after : p)
        lines.splice(cursorLine + 1, 0, ...newLines.filter(p => p.length > 0 || i < parts.length - 2))
        cursorLine++
        cursorCol = parts[parts.length - 1].length
      } else {
        lines[cursorLine] = before + text + after
        cursorCol += text.length
      }
    },
    deleteBackward() {
      if (cursorCol > 0) {
        const currentLine = lines[cursorLine]
        lines[cursorLine] = currentLine.slice(0, cursorCol - 1) + currentLine.slice(cursorCol)
        cursorCol--
      } else if (cursorLine > 0) {
        const currentLine = lines[cursorLine]
        const prevLine = lines[cursorLine - 1]
        cursorCol = prevLine.length
        lines[cursorLine - 1] = prevLine + currentLine
        lines.splice(cursorLine, 1)
        cursorLine--
      }
    },
    moveCursor(dir: "left" | "right") {
      if (dir === "left") {
        if (cursorCol > 0) {
          cursorCol--
        } else if (cursorLine > 0) {
          cursorLine--
          cursorCol = lines[cursorLine].length
        }
      } else {
        if (cursorCol < lines[cursorLine].length) {
          cursorCol++
        } else if (cursorLine < lines.length - 1) {
          cursorLine++
          cursorCol = 0
        }
      }
    },
    getCursorLine() { return cursorLine },
    getCursorCol() { return cursorCol },
    setCursorLineCol(line: number, col: number) {
      cursorLine = Math.max(0, Math.min(line, lines.length - 1))
      cursorCol = Math.max(0, Math.min(col, lines[cursorLine].length))
    },
    clear() {
      lines = [""]
      cursorLine = 0
      cursorCol = 0
    },
    focus() {
      // no-op for now, focus is handled by the renderer
    }
  }
}

let globalRenderer: TerminalRenderer | null = null

function getTerminalSize(): TerminalDimensions {
  const defaultWidth = 80
  const defaultHeight = 24
  
  try {
    if (process.stdout.columns && process.stdout.rows) {
      return { width: process.stdout.columns, height: process.stdout.rows }
    }
  } catch (e) {}
  
  try {
    const { execSync } = require("child_process")
    const cols = parseInt(execSync("tput cols 2>/dev/null || echo 80", { encoding: "utf8" }).trim())
    const rows = parseInt(execSync("tput lines 2>/dev/null || echo 24", { encoding: "utf8" }).trim())
    if (cols > 0 && rows > 0) {
      return { width: cols, height: rows }
    }
  } catch (e) {}
  
  return { width: defaultWidth, height: defaultHeight }
}

function moveCursor(row: number, col: number) {
  process.stdout.write(`\x1b[${row};${col}H`)
}

function clearScreen() {
  process.stdout.write("\x1b[2J\x1b[H")
}

function hideCursor() {
  process.stdout.write("\x1b[?25l")
}

function showCursor() {
  process.stdout.write("\x1b[?25h")
}

function eraseLine() {
  process.stdout.write("\x1b[2K")
}

function resetAttributes() {
  process.stdout.write("\x1b[0m")
}

function setForeground(color: string | number | undefined) {
  if (color === undefined) return
  if (typeof color === "number") {
    process.stdout.write(`\x1b[38;5;${color}m`)
  } else if (color.startsWith("#")) {
    const r = parseInt(color.slice(1, 3), 16)
    const g = parseInt(color.slice(3, 5), 16)
    const b = parseInt(color.slice(5, 7), 16)
    process.stdout.write(`\x1b[38;2;${r};${g};${b}m`)
  } else {
    process.stdout.write(`\x1b[38;5;${color}m`)
  }
}

function setBackground(color: string | number | undefined) {
  if (color === undefined) return
  if (typeof color === "number") {
    process.stdout.write(`\x1b[48;5;${color}m`)
  } else if (color.startsWith("#")) {
    const r = parseInt(color.slice(1, 3), 16)
    const g = parseInt(color.slice(3, 5), 16)
    const b = parseInt(color.slice(5, 7), 16)
    process.stdout.write(`\x1b[48;2;${r};${g};${b}m`)
  } else {
    process.stdout.write(`\x1b[48;5;${color}m`)
  }
}

function setBold(on: boolean) {
  process.stdout.write(on ? "\x1b[1m" : "\x1b[22m")
}

function setItalic(on: boolean) {
  process.stdout.write(on ? "\x1b[3m" : "\x1b[23m")
}

function setUnderline(on: boolean) {
  process.stdout.write(on ? "\x1b[4m" : "\x1b[24m")
}

export class TerminalRenderer {
  private eventListeners: Map<string, Set<(...args: unknown[]) => void>> = new Map()
  private renderCallback: (() => void) | null = null
  private running = false
  private suspended = false
  private pendingRender = false
  private exitOnCtrlC = true
  private consoleOptions: ConsoleOptions = {}
  private textareaCounter = 0
  private textareaInputs: Map<number, TextareaInput> = new Map()
  private focusedTextarea: TextareaInput | null = null
  private focusedTextareaOnChange: (() => void) | null = null

  constructor(options?: { exitOnCtrlC?: boolean; consoleOptions?: ConsoleOptions }) {
    this.exitOnCtrlC = options?.exitOnCtrlC ?? true
    this.consoleOptions = options?.consoleOptions ?? {}
  }

  createTextareaInput(initialText = ""): { id: number; input: TextareaInput } {
    const id = ++this.textareaCounter
    const input = createTextareaInput(initialText)
    this.textareaInputs.set(id, input)
    return { id, input }
  }

  setFocusedTextarea(id: number | null, onChange: (() => void) | null = null) {
    if (id === null) {
      this.focusedTextarea = null
      this.focusedTextareaOnChange = null
    } else {
      this.focusedTextarea = this.textareaInputs.get(id) || null
      this.focusedTextareaOnChange = onChange
    }
  }

  getFocusedTextarea(): TextareaInput | null {
    return this.focusedTextarea
  }

  requestRender() {
    if (this.suspended) return
    this.pendingRender = true
    if (this.renderCallback && !this.running) {
      this.running = true
      queueMicrotask(() => {
        this.running = false
        if (this.pendingRender && this.renderCallback) {
          this.pendingRender = false
          this.renderCallback()
        }
      })
    }
  }

  suspend() {
    this.suspended = true
    showCursor()
  }

  resume() {
    this.suspended = false
    hideCursor()
    this.requestRender()
  }

  on(event: string, cb: (...args: unknown[]) => void) {
    if (!this.eventListeners.has(event)) {
      this.eventListeners.set(event, new Set())
    }
    this.eventListeners.get(event)!.add(cb)
  }

  off(event: string, cb: (...args: unknown[]) => void) {
    const listeners = this.eventListeners.get(event)
    if (listeners) {
      listeners.delete(cb)
    }
  }

  private emit(event: string, ...args: unknown[]) {
    const listeners = this.eventListeners.get(event)
    if (listeners) {
      listeners.forEach((cb) => cb(...args))
    }
  }

  setRenderCallback(cb: () => void) {
    this.renderCallback = cb
  }

  getConsoleOptions() {
    return this.consoleOptions
  }

  cleanup() {
    showCursor()
    process.stdout.write("\x1b[0m")
    clearScreen()
    moveCursor(1, 1)
  }

  render(app: () => unknown) {
    hideCursor()
    clearScreen()
    
    this.setupInputHandlers()
    this.setupSignalHandlers()

    let needsRender = true

    const frameLoop = () => {
      if (!this.suspended && needsRender && this.renderCallback) {
        needsRender = false
        const content = app()
        if (content) {
          this.renderContent(content as RenderableElement)
        }
      }
      setTimeout(frameLoop, 16)
    }

    const originalRequestRender = this.requestRender.bind(this)
    this.requestRender = () => {
      if (this.suspended) return
      needsRender = true
    }

    this.setRenderCallback(() => {
      needsRender = true
    })

    frameLoop()
  }

  private setupInputHandlers() {
    let rawMode = false
    
    if (typeof (process.stdin as any).setRawMode === 'function') {
      try {
        ;(process.stdin as any).setRawMode(true)
        rawMode = true
      } catch (e) {}
    }
    
    if (rawMode) {
      this.setupRawModeInput(process.stdin)
    } else {
      this.setupSttyRawMode()
    }
  }
  
  private async setupSttyRawMode() {
    try {
      const { execSync } = await import("child_process")
      execSync("stty raw -echo 2>/dev/null", { stdio: "pipe" })
      this.setupRawModeInput(process.stdin)
    } catch (e) {
      this.setupReadlineMode()
    }
  }
  
  private setupRawModeInput(rs: any) {
    let buffer = ""
    
    const handleKey = (name: string, char?: string) => {
      const textarea = this.focusedTextarea
      if (textarea) {
        if (name === "backspace") {
          textarea.deleteBackward()
          if (this.focusedTextareaOnChange) this.focusedTextareaOnChange()
          this.requestRender()
          return
        }
        if (name === "left") {
          textarea.moveCursor("left")
          this.requestRender()
          return
        }
        if (name === "right") {
          textarea.moveCursor("right")
          this.requestRender()
          return
        }
        if (char && char >= " " && char <= "~") {
          textarea.insertText(char)
          if (this.focusedTextareaOnChange) this.focusedTextareaOnChange()
          this.requestRender()
          return
        }
      }
      
      const evt: KeyEvent = {
        name: name,
        ctrl: false,
        shift: char ? (char >= "A" && char <= "Z") : false,
        alt: false,
        meta: false,
        option: false,
        preventDefault: () => {},
        char: char,
      }
      this.emit("key", evt)
      this.emit(`key:${name}`, evt)
    }
    
    rs.on("keypress", (chunk: string, info: any) => {
      if (info === undefined) {
        handleKey("unknown", chunk)
        return
      }
      
      if (info.ctrl && info.name === "c") {
        this.cleanup()
        process.exit(0)
        return
      }
      
      handleKey(info.name || "unknown", chunk)
    })
    
    rs.on("data", (chunk: string) => {
      buffer += chunk
    })
  }
  
  private setupReadlineMode() {
    import("readline").then(({ createInterface }) => {
      const rl = createInterface({
        input: process.stdin,
        output: process.stdout,
        terminal: true,
        escapeCodeTimeout: 50,
      } as Parameters<typeof createInterface>[0])

      rl.on("line", (line: string) => {
        const textarea = this.focusedTextarea
        if (textarea) {
          textarea.setText(line)
          if (this.focusedTextareaOnChange) this.focusedTextareaOnChange()
          this.requestRender()
        }
      })

      rl.on("keypress", (_key: string, info: { ctrl: boolean; shift: boolean; meta: boolean; name: string }) => {
        if (this.exitOnCtrlC && info.ctrl && info.name === "c") {
          this.cleanup()
          process.exit(0)
          return
        }

        if (info.name === "return" || info.name === "enter") {
          return
        }

        const textarea = this.focusedTextarea
        if (textarea && _key) {
          if (info.name === "backspace") {
            textarea.deleteBackward()
            if (this.focusedTextareaOnChange) this.focusedTextareaOnChange()
            this.requestRender()
            return
          }
          if (_key.length === 1) {
            textarea.insertText(_key)
            if (this.focusedTextareaOnChange) this.focusedTextareaOnChange()
            this.requestRender()
            return
          }
        }

        const evt: KeyEvent = {
          name: info.name || "unknown",
          ctrl: info.ctrl,
          shift: info.shift,
          alt: info.meta,
          meta: info.meta,
          option: info.meta,
          preventDefault: () => {},
        }
        this.emit("key", evt)
        this.emit(`key:${info.name}`, evt)
      })
    })
  }

  private setupSignalHandlers() {
    process.on("SIGWINCH", () => {
      this.emit("resize", getTerminalSize())
      this.requestRender()
    })
  }

  renderContent(root: RenderableElement) {
    const dims = getTerminalSize()
    moveCursor(1, 1)
    process.stdout.write("\x1b[J")
    
    this.renderElement(root, 0, 0, dims.width, dims.height)
    
    process.stdout.write("\x1b[0m")
  }

  private renderElement(el: unknown, x: number, y: number, width: number, height: number): number {
    if (!el) return 0
    
    const node = el as RenderableElement
    
    switch (node.type) {
      case "box":
        return this.renderBox(node as BoxElement, x, y, width, height)
      case "text":
        return this.renderText(node as TextElement, x, y)
      case "scrollbox":
        return this.renderScrollbox(node as ScrollboxElement, x, y, width, height)
      case "textarea":
        return this.renderTextarea(node as TextareaElement, x, y, width, height)
      case "input":
        return this.renderInput(node as InputElement, x, y, width)
      case "fragment":
        return this.renderFragment(node as FragmentElement, x, y, width, height)
      default:
        return 0
    }
  }

  private renderBox(el: BoxElement, x: number, y: number, width: number, height: number): number {
    const props = el.props || {}
    const {
      flexDirection = "row",
      backgroundColor,
      paddingLeft = 0,
      paddingRight = 0,
      paddingTop = 0,
      paddingBottom = 0,
    } = props

    if (backgroundColor != null) {
      setBackground(backgroundColor as string | number)
      moveCursor(y + 1, x + 1)
      for (let i = 0; i < height; i++) {
        process.stdout.write(" ".repeat(width))
        if (i < height - 1) moveCursor(y + i + 2, x + 1)
      }
      resetAttributes()
    }

    const contentWidth = Math.max(1, width - (paddingLeft as number) - (paddingRight as number))
    const contentHeight = Math.max(1, height - (paddingTop as number) - (paddingBottom as number))
    let offsetX = x + (paddingLeft as number)
    let offsetY = y + (paddingTop as number)

    const children = (el as any).children || props.children
    
    if (children) {
      const childArray = Array.isArray(children) ? children : [children]
      
      if (flexDirection === "row") {
        let renderedWidth = 0
        
        for (const child of childArray) {
          if (!child) continue
          if (typeof child === "string" || typeof child === "number") {
            const len = String(child).length
            moveCursor(offsetY + 1, offsetX + 1)
            process.stdout.write(String(child))
            offsetX += len
            renderedWidth += len
            continue
          }
          const childNode = child as RenderableElement
          const childProps = childNode.props || {}
          
          if (childProps.flexGrow) continue
          
          let childWidth: number
          if (childNode.type === "text") {
            childWidth = this.extractText(childNode).length
          } else if (childNode.type === "box") {
            const boxProps = childNode.props || {}
            const boxPaddingLeft = (boxProps.paddingLeft as number) || 0
            const boxPaddingRight = (boxProps.paddingRight as number) || 0
            const boxChildren = (childNode as any).children || boxProps.children
            if (!boxChildren) {
              childWidth = boxPaddingLeft + boxPaddingRight
            } else {
              childWidth = this.getNodeWidth(childNode, contentWidth)
            }
          } else {
            childWidth = this.getNodeWidth(childNode, contentWidth)
          }
          this.renderElement(childNode, offsetX, offsetY, childWidth, contentHeight)
          offsetX += childWidth
          renderedWidth += childWidth
        }
        
        const flexGrowChildren = childArray.filter(c => {
          if (!c || typeof c === "string" || typeof c === "number") return false
          const childProps = (c as RenderableElement).props || {}
          return childProps.flexGrow
        })
        
        if (flexGrowChildren.length > 0) {
          const remainingWidth = Math.max(0, contentWidth - renderedWidth)
          const flexGrowWidth = Math.floor(remainingWidth / flexGrowChildren.length)
          if (y === 1) process.stderr.write(`[ROW1] flexGrow: contentWidth=${contentWidth} renderedWidth=${renderedWidth} remaining=${remainingWidth} flexGrowWidth=${flexGrowWidth}\n`)
          
          for (const child of flexGrowChildren) {
            const childNode = child as RenderableElement
            this.renderElement(childNode, offsetX, offsetY, flexGrowWidth, contentHeight)
            offsetX += flexGrowWidth
            renderedWidth += flexGrowWidth
          }
        }
      } else {
        let contentOffsetY = offsetY
        for (const child of childArray) {
          if (!child) continue
          if (typeof child === "string" || typeof child === "number") {
            moveCursor(contentOffsetY + 1, offsetX + 1)
            process.stdout.write(String(child))
            contentOffsetY += 1
            continue
          }
          const childNode = child as RenderableElement
          const childProps = childNode.props || {}
          let childHeight: number
          if (childProps.flexGrow) {
            childHeight = contentHeight
          } else {
            childHeight = this.getNodeHeight(childNode, 1)
          }
          this.renderElement(childNode, offsetX, contentOffsetY, contentWidth, childHeight)
          contentOffsetY += childHeight
        }
      }
    }

    if (flexDirection === "row") {
      return height
    } else {
      let totalHeight = 0
      const children = (el as any).children || props.children
      if (children) {
        const childArray = Array.isArray(children) ? children : [children]
        for (const child of childArray) {
          if (!child || typeof child === "string" || typeof child === "number") {
            totalHeight += 1
            continue
          }
          const childNode = child as RenderableElement
          if (childNode.type === "box") {
            totalHeight += this.getNodeHeight(childNode, 1)
          } else {
            totalHeight += 1
          }
        }
      }
      return Math.max(1, totalHeight)
    }
  }

  private renderText(el: TextElement, x: number, y: number): number {
    const props = el.props || {}
    const fg = props.fg as string | number | undefined
    const attributes = props.attributes as TextAttribute | undefined
    const text = this.extractText(el)

    if (!text) return 0

    moveCursor(y + 1, x + 1)
    
    if (fg != null) setForeground(fg)
    if (attributes === TextAttributes.BOLD) setBold(true)
    else if (attributes === TextAttributes.ITALIC) setItalic(true)
    else if (attributes === TextAttributes.UNDERLINE) setUnderline(true)

    process.stdout.write(text)
    resetAttributes()

    return 1
  }

  private renderScrollbox(el: ScrollboxElement, x: number, y: number, width: number, height: number): number {
    const props = el.props || {}
    const paddingLeft = (props.paddingLeft as number) || 0
    const paddingRight = (props.paddingRight as number) || 0
    const paddingTop = (props.paddingTop as number) || 0
    
    const contentWidth = Math.max(1, width - paddingLeft - paddingRight)
    const contentHeight = Math.max(1, height - paddingTop)
    let offsetY = y + paddingTop

    const children = (el as any).children || props.children
    if (children) {
      const childArray = Array.isArray(children) ? children : [children]
      for (const child of childArray) {
        if (!child || typeof child === "string" || typeof child === "number") continue
        const childHeight = this.getNodeHeight(child as RenderableElement, contentHeight)
        if (offsetY + childHeight <= y + height) {
          this.renderElement(child, x + paddingLeft, offsetY, contentWidth, childHeight)
          offsetY += childHeight
        }
      }
    }

    return contentHeight
  }

  private renderTextarea(el: TextareaElement, x: number, y: number, width: number, height: number): number {
    const props = el.props || {}
    const focused = (props.focused as boolean) ?? true
    const textColor = props.textColor as string | number | undefined
    const focusedTextColor = props.focusedTextColor as string | number | undefined
    const placeholder = props.placeholder as string | undefined
    const ref = props.ref as ((val: unknown) => void) | undefined
    const onContentChange = props.onContentChange as (() => void) | undefined
    
    let textareaId = (el as any)._textareaId
    if (!textareaId) {
      const result = this.createTextareaInput(props.value as string || "")
      textareaId = result.id
      ;(el as any)._textareaId = textareaId
    }
    
    const textareaInput = this.textareaInputs.get(textareaId)
    if (!textareaInput) {
      const result = this.createTextareaInput(props.value as string || "")
      textareaId = result.id
      ;(el as any)._textareaId = textareaId
    }
    const input = this.textareaInputs.get(textareaId)!
    
    if (focused) {
      this.setFocusedTextarea(textareaId, onContentChange || null)
    }
    
    if (ref) {
      ref(input)
    }
    
    const text = input.plainText
    const displayText = text || placeholder || ""

    const lines = displayText.split("\n")
    
    for (let i = 0; i < height; i++) {
      moveCursor(y + i + 1, x + 1)
      eraseLine()
      if (lines[i]) {
        const color = focused ? textColor : focusedTextColor
        if (color != null) setForeground(color)
        process.stdout.write(lines[i].slice(0, width))
        resetAttributes()
      }
    }
    
    if (focused) {
      const cursorLine = Math.min(input.getCursorLine(), height - 1)
      const cursorCol = Math.min(input.getCursorCol(), width - 1)
      moveCursor(y + cursorLine + 1, x + cursorCol + 1)
      process.stdout.write("\x1b[?25h")
    }

    return height
  }

  private renderInput(el: InputElement, x: number, y: number, width: number): number {
    const props = el.props || {}
    const focused = (props.focused as boolean) ?? true
    const textColor = props.textColor as string | number | undefined
    const focusedTextColor = props.focusedTextColor as string | number | undefined
    const placeholder = props.placeholder as string | undefined
    const value = props.value as string | undefined
    const text = value || ""
    const displayText = text || placeholder || ""

    moveCursor(y + 1, x + 1)
    eraseLine()

    if (displayText) {
      const color = focused ? textColor : focusedTextColor
      if (color != null) setForeground(color)
      process.stdout.write(displayText)
      resetAttributes()
    }

    return 1
  }

  private renderFragment(el: FragmentElement, x: number, y: number, width: number, height: number): number {
    const children = (el as any).children || el.props?.children
    if (!children || (Array.isArray(children) && children.length === 0)) return 0

    let maxY = y
    const childArray = Array.isArray(children) ? children : [children]
    let offsetY = y
    for (const child of childArray) {
      if (!child) {
        continue
      }
      if (typeof child === "string" || typeof child === "number") {
        moveCursor(offsetY + 1, x + 1)
        process.stdout.write(String(child))
        offsetY += 1
        continue
      }
      const childHeight = this.renderElement(child as RenderableElement, x, offsetY, width, height)
      offsetY += childHeight
      maxY = Math.max(maxY, offsetY)
    }
    return Math.max(1, maxY - y)
  }

  private extractText(el: RenderableElement): string {
    const props = el.props || {}
    const children = (el as any).children || props.children
    if (!children) return ""
    
    const childArray = Array.isArray(children) ? children : [children]
    return childArray.map((c) => {
      if (typeof c === "string") return c
      if (typeof c === "number") return String(c)
      if (c && typeof c === "object" && "props" in c) {
        return this.extractText(c as RenderableElement)
      }
      return ""
    }).join("")
  }

  private getNodeWidth(node: RenderableElement, defaultWidth: number): number {
    if (node.type === "fragment") return 0
    const props = node.props || {}
    const w = props.width
    if (typeof w === "number") return w
    
    if (node.type === "box") {
      const boxProps = props
      const boxFlexDirection = boxProps.flexDirection || "row"
      if (boxFlexDirection === "row") {
        const children = (node as any).children || boxProps.children
        if (children) {
          const childArray = Array.isArray(children) ? children : [children]
          let totalWidth = 0
          for (const child of childArray) {
            if (!child) continue
            if (typeof child === "string" || typeof child === "number") {
              totalWidth += String(child).length
            } else {
              const childNode = child as RenderableElement
              if (childNode.type === "text") {
                totalWidth += this.extractText(childNode).length
              } else {
                totalWidth += this.getNodeWidth(childNode, 0)
              }
            }
          }
          return totalWidth
        }
      }
    }
    
    return defaultWidth
  }

  private getNodeHeight(node: RenderableElement, defaultHeight: number): number {
    if (node.type === "fragment") return 0
    const props = node.props || {}
    const h = props.height
    return typeof h === "number" ? h : defaultHeight
  }
}

export interface RenderableElement {
  type: string
  props: Record<string, unknown>
}

export interface BoxElement extends RenderableElement {
  type: "box"
}

export interface TextElement extends RenderableElement {
  type: "text"
}

export interface ScrollboxElement extends RenderableElement {
  type: "scrollbox"
}

export interface TextareaElement extends RenderableElement {
  type: "textarea"
}

export interface InputElement extends RenderableElement {
  type: "input"
}

export interface FragmentElement extends RenderableElement {
  type: "fragment"
}

export function createTerminalRenderer(options?: { exitOnCtrlC?: boolean; consoleOptions?: ConsoleOptions }) {
  return new TerminalRenderer(options)
}

export function useRenderer(): TerminalRenderer {
  if (!globalRenderer) {
    globalRenderer = new TerminalRenderer()
  }
  return globalRenderer
}

export function useTerminalDimensions(): () => TerminalDimensions {
  const [dimensions, setDimensions] = createSignal(getTerminalSize())
  
  if (globalRenderer) {
    globalRenderer.on("resize", (dims) => {
      setDimensions(dims as TerminalDimensions)
    })
  }

  return dimensions
}

export function createInputRenderable(initialValue = ""): InputRenderable {
  let value = initialValue
  let focused = true

  return {
    get value() { return value },
    get focused() { return focused },
    focus() { focused = true },
  }
}

export function createTextareaRenderable(initialText = ""): TextareaRenderable {
  let text = initialText
  let cursor = 0
  let isFocused = true

  return {
    get plainText() { return text },
    get cursorOffset() { return cursor },
    get focused() { return isFocused },
    setText(newText: string) { text = newText },
    clear() { text = ""; cursor = 0 },
    focus() { isFocused = true },
  }
}

export function createScrollBoxRenderable(): ScrollBoxRenderable {
  let scrollOffset = 0

  return {
    scrollBy(amount: number) {
      scrollOffset += amount
    },
  }
}

export function useKeyboard(handler: (evt: KeyEvent) => void): void {
  if (!globalRenderer) return

  const handlerWrapper = (evt: unknown) => {
    handler(evt as KeyEvent)
  }

  globalRenderer.on("key", handlerWrapper)

  onCleanup(() => {
    globalRenderer?.off("key", handlerWrapper)
  })
}

export function h(type: any, props: Record<string, unknown> | null, ...children: unknown[]): RenderableElement {
  if (typeof type === "function") {
    const resolvedProps = props || {}
    resolvedProps.children = children.length > 0 ? children : undefined
    return type(resolvedProps) as RenderableElement
  }
  return {
    type,
    props: props || {},
    children: children.length > 0 ? children : undefined,
  } as RenderableElement
}

export function Fragment(props: { children?: unknown[] }): RenderableElement {
  return h("fragment", null, ...(props.children || []))
}

export function For(props: { each: unknown[] | (() => unknown[]); children: (item: unknown, index: () => number) => unknown }): RenderableElement {
  const items = typeof props.each === "function" ? props.each() : props.each
  const results: unknown[] = []
  for (let i = 0; i < items.length; i++) {
    const child = props.children(items[i], () => i)
    if (Array.isArray(child)) {
      results.push(...child)
    } else if (child) {
      results.push(child)
    }
  }
  return h("fragment", null, ...results)
}

export function Show(props: { when: unknown; children: () => unknown; fallback?: () => unknown }): RenderableElement | null {
  const condition = typeof props.when === "function" ? props.when() : props.when
  if (!condition && !props.fallback) return null
  if (!condition) return props.fallback!() as RenderableElement
  const child = props.children()
  if (!child) return null
  if (Array.isArray(child)) return h("fragment", null, ...child.filter(c => c != null))
  return child as RenderableElement
}

export const jsx = h
export const jsxs = h
