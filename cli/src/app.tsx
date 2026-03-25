import { createEffect, createMemo, createSignal, For, onCleanup, onMount } from "solid-js"
import { useKeyboard, useRenderer, useTerminalDimensions } from "@opentui/solid"
import { InputRenderable, ScrollBoxRenderable, TextAttributes, TextareaRenderable } from "@opentui/core"
import {
  createClient,
  replStream,
  type ApiResponse,
  type StreamEvent,
  type ToolResult,
  type PlanStep,
  type SessionInfo,
  type SkillMetadata,
  type ModelsResponse,
} from "./api"
import { formatToolOutput } from "./format"
import { renderMarkdownLines } from "./markdown"
import type { Entry } from "./types"
import { parseTranscriptToEntries } from "./transcript"
import { theme } from "./theme"

type AppProps = {
  apiUrl: string
  sessionID: string
  initialPrompt?: string
}

type ModalKind = "sessions" | "skills" | "models" | "connect" | "modelToken" | "help"

type ModalItem = {
  id: string
  title: string
  subtitle?: string
  provider?: string
  model?: string
  hasToken?: boolean
}

export function App(props: AppProps) {
  const renderer = useRenderer()
  const terminal = useTerminalDimensions()
  const [apiUrl, setApiUrl] = createSignal(props.apiUrl)
  const client = createMemo(() => createClient(apiUrl()))
  const [entries, setEntries] = createSignal<Entry[]>([])
  const [input, setInput] = createSignal("")
  const [busy, setBusy] = createSignal(false)
  const [sessionID, setSessionID] = createSignal(props.sessionID)
  const [inputHistory, setInputHistory] = createSignal<string[]>([])
  const [historyIndex, setHistoryIndex] = createSignal<number | null>(null)
  const [historyDraft, setHistoryDraft] = createSignal("")
  const [modal, setModal] = createSignal<ModalKind | null>(null)
  const [modalTitle, setModalTitle] = createSignal("")
  const [modalHint, setModalHint] = createSignal("")
  const [modalQuery, setModalQuery] = createSignal("")
  const [modalItems, setModalItems] = createSignal<ModalItem[]>([])
  const [modalError, setModalError] = createSignal("")
  const [pendingModel, setPendingModel] = createSignal<{ provider: string; model: string } | null>(null)
  const [modalSelected, setModalSelected] = createSignal(0)
  const [modalInput, setModalInput] = createSignal("")
  let modalInputRef: InputRenderable | undefined
  let modalSearchTimer: ReturnType<typeof setTimeout> | undefined
  let scroll: ScrollBoxRenderable | undefined
  let textarea: TextareaRenderable | undefined
  let suppressHistoryChange = false
  const toolEntryByCallID = new Map<string, string>()

  const setTextareaValue = (value: string) => {
    suppressHistoryChange = true
    setInput(value)
    textarea?.setText(value)
    if (textarea) textarea.cursorOffset = textarea.plainText.length
    suppressHistoryChange = false
  }

  const rememberInput = (value: string) => {
    const normalized = value
    if (!normalized.trim()) return
    setInputHistory((prev) => {
      const next = prev.length > 0 && prev[prev.length - 1] === normalized ? prev : [...prev, normalized]
      if (next.length > 200) return next.slice(next.length - 200)
      return next
    })
  }

  const historyPrev = () => {
    const list = inputHistory()
    if (list.length === 0) return
    let idx = historyIndex()
    if (idx === null) {
      setHistoryDraft(textarea?.plainText ?? input())
      idx = list.length
    }
    const nextIdx = Math.max(0, idx - 1)
    setHistoryIndex(nextIdx)
    setTextareaValue(list[nextIdx])
  }

  const historyNext = () => {
    const list = inputHistory()
    const idx = historyIndex()
    if (idx === null) return
    const nextIdx = idx + 1
    if (nextIdx >= list.length) {
      setHistoryIndex(null)
      setTextareaValue(historyDraft())
      return
    }
    setHistoryIndex(nextIdx)
    setTextareaValue(list[nextIdx])
  }

  const focusActiveInput = () => {
    queueMicrotask(() => {
      if (modal()) {
        modalInputRef?.focus()
        return
      }
      textarea?.focus()
    })
  }

  const formatSessionID = () => {
    const now = new Date()
    const pad = (value: number, size = 2) => String(value).padStart(size, "0")
    const date = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}`
    const time = `${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`
    const ms = pad(now.getMilliseconds(), 3)
    return `${date}-${time}-${ms}`
  }

  const appendEntry = (entry: Entry) => {
    setEntries((prev) => [...prev, entry])
    queueMicrotask(() => {
      if (scroll) scroll.scrollBy(100000)
      renderer.requestRender()
    })
  }

  const updateEntryContent = (id: string, content: string) => {
    setEntries((prev) => prev.map((e) => (e.id === id ? { ...e, content } : e)))
    queueMicrotask(() => renderer.requestRender())
  }

  const submit = async (text: string) => {
    const trimmed = text.trim()
    if (!trimmed) return

    // Record input before we clear it.
    if (!modal()) {
      rememberInput(text)
      setHistoryIndex(null)
      setHistoryDraft("")
    }

    setInput("")
    textarea?.clear()
    if (modal()) {
      handleModalSubmit(trimmed)
      return
    }
    if (trimmed.startsWith("/")) {
      await handleCommand(trimmed)
      return
    }

    // Allow slash-commands (like /exit) even while busy,
    // but avoid sending a new prompt while a request is in-flight.
    if (busy()) return
    setBusy(true)
    appendEntry({ id: crypto.randomUUID(), role: "user", content: trimmed })

    const assistantID = crypto.randomUUID()
    appendEntry({ id: assistantID, role: "assistant", content: "" })
    let assistantText = ""
    let finalReply: string | null = null

    const onStreamEvent = (evt: StreamEvent) => {
      if (evt.event === "assistant_delta") {
        assistantText += evt.data.text ?? ""
        updateEntryContent(assistantID, assistantText)
        return
      }
      if (evt.event === "tool_pending") {
        const step: PlanStep = {
          id: evt.data.call_id ?? crypto.randomUUID(),
          description: `Tool call: ${evt.data.tool}`,
          tool: evt.data.tool,
          inputs: (evt.data.input ?? {}) as Record<string, unknown>,
          status: "running",
        }
        const entryID = crypto.randomUUID()
        if (evt.data.call_id) toolEntryByCallID.set(evt.data.call_id, entryID)
        appendEntry({ id: entryID, role: "tool", title: `Tool ${evt.data.tool}`, content: formatToolOutput(step, null) })
        return
      }
      if (evt.event === "tool_result") {
        const callID = evt.data.call_id
        const existing = callID ? toolEntryByCallID.get(callID) : undefined
        if (existing) {
          updateEntryContent(existing, formatToolOutput(evt.data.step, evt.data.result))
        } else {
          appendEntry({
            id: crypto.randomUUID(),
            role: "tool",
            title: evt.data.step.tool ? `Tool ${evt.data.step.tool}` : "Tool",
            content: formatToolOutput(evt.data.step, evt.data.result),
          })
        }
        return
      }
      if (evt.event === "error") {
        appendEntry({ id: crypto.randomUUID(), role: "error", content: evt.data.error })
        return
      }
      if (evt.event === "done") {
        const payload = evt.data as unknown as { reply?: string; Reply?: string }
        finalReply = payload.reply ?? payload.Reply ?? null
      }
    }

    try {
      await replStream(apiUrl(), { session: sessionID(), input: trimmed }, onStreamEvent)
    } catch (err) {
      // Fallback to non-streaming endpoint.
      try {
        const response = await client().repl({ session: sessionID(), input: trimmed })
        appendToolEntries(response.plan?.steps ?? [], response.results ?? [])
        if (response.reply) updateEntryContent(assistantID, response.reply)
      } catch (err2) {
        appendEntry({
          id: crypto.randomUUID(),
          role: "error",
          content: err2 instanceof Error ? err2.message : String(err2),
        })
        setBusy(false)
        return
      }
      setBusy(false)
      return
    }

    // If the stream ended but we didn't get deltas, fall back to the final reply.
    if (assistantText.trim() === "" && finalReply) {
      updateEntryContent(assistantID, finalReply)
    }

    setBusy(false)
  }

  const modalFilteredItems = createMemo(() => {
    const kind = modal()
    if (kind === "sessions" || kind === "skills") {
      return modalItems()
    }
    const query = modalQuery().toLowerCase().trim()
    if (!query) return modalItems()
    return modalItems().filter((item) =>
      `${item.title} ${item.subtitle ?? ""}`.toLowerCase().includes(query),
    )
  })

  const modalWindow = createMemo(() => {
    const items = modalFilteredItems()
    if (items.length <= 8) {
      return { start: 0, items }
    }
    const current = modalSelected()
    const start = Math.min(Math.max(0, current - 3), items.length - 8)
    return { start, items: items.slice(start, start + 8) }
  })

  const modalCountLabel = createMemo(() => {
    const count = modalFilteredItems().length
    return count > 0 ? `${modalTitle()} (${count})` : modalTitle()
  })

  const modalListHeight = createMemo(() => {
    const count = modalWindow().items.length
    if (count <= 0) return 1
    return Math.min(8, count)
  })

  const resetModal = () => {
    setModal(null)
    setModalTitle("")
    setModalHint("")
    setModalQuery("")
    setModalItems([])
    setModalError("")
    setPendingModel(null)
    setModalSelected(0)
    setModalInput("")
  }

  const startModal = (kind: ModalKind, title: string, hint: string, items: ModalItem[] = []) => {
    setModal(kind)
    setModalTitle(title)
    setModalHint(hint)
    setModalItems(items)
    setModalQuery("")
    setModalError("")
    setModalSelected(0)
    setModalInput("")
    queueMicrotask(() => {
      modalInputRef?.focus()
    })
  }

  const moveModalSelection = (delta: number) => {
    const items = modalFilteredItems()
    if (items.length === 0) return
    let next = modalSelected() + delta
    if (next < 0) next = items.length - 1
    if (next >= items.length) next = 0
    setModalSelected(next)
  }

  const updateModalQuery = (value: string) => {
    const kind = modal()
    setModalInput(value)
    if (!kind || kind === "connect" || kind === "modelToken") {
      return
    }
    const query = value.trim()
    setModalQuery(query)
    if (kind === "models") {
      setModalSelected(0)
      return
    }
    if (modalSearchTimer) clearTimeout(modalSearchTimer)
    modalSearchTimer = setTimeout(async () => {
      try {
        if (kind === "sessions") {
          const sessions = await client().sessions(query)
          const items = sessions
            .slice()
            .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
            .slice(0, 32)
            .map((s: SessionInfo) => ({
              id: s.id,
              title: s.id,
              subtitle: s.created_at,
            }))
          setModalItems(items)
          const currentIndex = items.findIndex((item) => item.id === sessionID())
          setModalSelected(currentIndex >= 0 ? currentIndex : 0)
          return
        }
        if (kind === "skills") {
          const res = await client().skills(query)
          const items = res.skills.map((s: SkillMetadata) => ({
            id: s.name,
            title: s.name,
            subtitle: s.description,
          }))
          setModalItems(items)
          setModalSelected(0)
        }
      } catch (err) {
        setModalError(err instanceof Error ? err.message : String(err))
      }
    }, 120)
  }

  const selectModalItem = async (item: ModalItem | undefined) => {
    if (!item) return
    const kind = modal()
    if (!kind) return
    if (kind === "sessions") {
      try {
        await client().loadSession(item.id)
        setSessionID(item.id)
        const dump = await client().getSession(item.id)
        setEntries(parseTranscriptToEntries(dump.conversation))
        resetModal()
        focusActiveInput()
      } catch (err) {
        setModalError(err instanceof Error ? err.message : String(err))
      }
      return
    }
    if (kind === "skills") {
      try {
        await client().loadSkill(item.id)
        appendEntry({ id: crypto.randomUUID(), role: "assistant", content: `Active skill: ${item.title}` })
        resetModal()
      } catch (err) {
        setModalError(err instanceof Error ? err.message : String(err))
      }
      return
    }
    if (kind === "models") {
      const provider = item.provider ?? ""
      const model = item.model ?? ""
      if (!provider || !model) {
        setModalError("Invalid model selection")
        return
      }
      if (item.hasToken) {
        try {
          await client().selectModel({ provider, model })
          appendEntry({ id: crypto.randomUUID(), role: "assistant", content: `Model updated: ${provider} / ${model}` })
          resetModal()
        } catch (err) {
          setModalError(err instanceof Error ? err.message : String(err))
        }
        return
      }
      setPendingModel({ provider, model })
      startModal("modelToken", `API key for ${provider}`, "Paste API key (empty to cancel)")
      return
    }
  }

  const handleModalSubmit = async (value: string) => {
    const kind = modal()
    if (!kind) return
    if (kind === "help") {
      resetModal()
      return
    }
    if (kind === "connect") {
      if (!value) {
        appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "Connect cancelled." })
        resetModal()
        return
      }
      setApiUrl(value)
      appendEntry({ id: crypto.randomUUID(), role: "assistant", content: `Connected to ${value}` })
      resetModal()
      return
    }
    if (kind === "modelToken") {
      const model = pendingModel()
      if (!model) {
        resetModal()
        return
      }
      if (!value) {
        appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "Cancelled." })
        resetModal()
        return
      }
      try {
        await client().selectModel({ provider: model.provider, model: model.model, api_key: value })
        appendEntry({
          id: crypto.randomUUID(),
          role: "assistant",
          content: `Model updated: ${model.provider} / ${model.model}`,
        })
        resetModal()
      } catch (err) {
        setModalError(err instanceof Error ? err.message : String(err))
      }
      return
    }

    const item = modalFilteredItems()[modalSelected()]
    await selectModalItem(item)
  }

  const newSession = () => {
    const next = formatSessionID()
    setSessionID(next)
    setEntries([])
    setInput("")
    textarea?.clear()
    return next
  }

  const handleCommand = async (command: string) => {
    const [name, ...args] = command.split(" ")
    switch (name) {
      case "/exit":
        process.exit(0)
        return
      case "/help":
        appendEntry({
          id: crypto.randomUUID(),
          role: "assistant",
          content:
            "Commands:\n  /new\n  /sessions\n  /skills\n  /models\n  /connect <url>\n  /help\n  /exit",
        })
        return
      case "/new": {
        const next = newSession()
        appendEntry({ id: crypto.randomUUID(), role: "assistant", content: `New session ${next}` })
        return
      }
      case "/connect": {
        const url = args.join(" ").trim()
        if (url) {
          setApiUrl(url)
          appendEntry({ id: crypto.randomUUID(), role: "assistant", content: `Connected to ${url}` })
          return
        }
        startModal("connect", "Connect", "Enter API base URL")
        return
      }
      case "/sessions": {
        try {
          const sessions = await client().sessions()
          if (sessions.length === 0) {
            appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "No sessions found." })
            return
          }
          const items = sessions
            .slice()
            .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
            .slice(0, 32)
            .map((s: SessionInfo) => ({
              id: s.id,
              title: s.id,
              subtitle: s.created_at,
            }))
          startModal("sessions", "Sessions", "Type to search, Enter to select", items)
          const currentIndex = items.findIndex((item) => item.id === sessionID())
          if (currentIndex >= 0) setModalSelected(currentIndex)
          setModalInput("")
          queueMicrotask(() => modalInputRef?.focus())
        } catch (err) {
          appendEntry({
            id: crypto.randomUUID(),
            role: "error",
            content: err instanceof Error ? err.message : String(err),
          })
        }
        return
      }
      case "/skills": {
        try {
          const res = await client().skills()
          if (!res.skills || res.skills.length === 0) {
            appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "No skills found." })
            return
          }
          const items = res.skills.map((s: SkillMetadata) => ({
            id: s.name,
            title: s.name,
            subtitle: s.description,
          }))
          startModal("skills", "Skills", "Type to search, Enter to select", items)
          setModalInput("")
          queueMicrotask(() => modalInputRef?.focus())
        } catch (err) {
          appendEntry({
            id: crypto.randomUUID(),
            role: "error",
            content: err instanceof Error ? err.message : String(err),
          })
        }
        return
      }
      case "/models": {
        try {
          const res = await client().models()
          const items = flattenModels(res)
          if (items.length === 0) {
            appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "No models available." })
            return
          }
          setModalItems(items)
          startModal("models", "Models", "Type to search, Enter to select", items)
          setModalInput("")
          queueMicrotask(() => modalInputRef?.focus())
        } catch (err) {
          appendEntry({
            id: crypto.randomUUID(),
            role: "error",
            content: err instanceof Error ? err.message : String(err),
          })
        }
        return
      }
      default:
        appendEntry({ id: crypto.randomUUID(), role: "error", content: `Unknown command: ${name}` })
    }
  }

  const flattenModels = (res: ModelsResponse): ModalItem[] => {
    const items: ModalItem[] = []
    for (const provider of res.providers ?? []) {
      for (const model of provider.models ?? []) {
        items.push({
          id: `${provider.name}:${model}`,
          title: `${provider.name} / ${model}`,
          provider: provider.name,
          model,
          hasToken: provider.has_token,
        })
      }
    }
    return items
  }

  const appendToolEntries = (steps: PlanStep[], results: ToolResult[]) => {
    const byStep = new Map<string, ToolResult>()
    for (const result of results) {
      byStep.set(result.step_id ?? "", result)
    }
    for (const step of steps) {
      const result = byStep.get(step.id) ?? null
      const title = step.tool ? `Tool ${step.tool}` : "Tool"
      const content = formatToolOutput(step, result)
      appendEntry({ id: crypto.randomUUID(), role: "tool", title, content })
    }
  }

  useKeyboard((evt) => {
    if (modal()) {
      if (evt.name === "escape") {
        resetModal()
        return
      }
      if (evt.name === "up") {
        moveModalSelection(-1)
        return
      }
      if (evt.name === "down") {
        moveModalSelection(1)
        return
      }
    }

    if (!modal() && textarea?.focused) {
      if (evt.name === "up" && !evt.shift && !evt.ctrl && !evt.meta && !evt.option) {
        if (textarea.logicalCursor.row === 0) {
          evt.preventDefault()
          historyPrev()
          return
        }
      }
      if (evt.name === "down" && !evt.shift && !evt.ctrl && !evt.meta && !evt.option) {
        if (textarea.logicalCursor.row === textarea.lineCount - 1) {
          evt.preventDefault()
          historyNext()
          return
        }
      }
    }

    if (evt.name === "c" && evt.ctrl) {
      process.exit(0)
    }
    if (evt.name === "n" && evt.ctrl) {
      newSession()
      return
    }
  })

  onMount(() => {
    focusActiveInput()

    const onRendererFocus = () => focusActiveInput()
    const onSelectionChanged = (selection: unknown) => {
      const sel = selection as { isDragging?: boolean } | null
      if (sel && sel.isDragging) return
      focusActiveInput()
    }

    ;(renderer as unknown as { on: (event: string, cb: (...args: any[]) => void) => void }).on("focus", onRendererFocus)
    ;(renderer as unknown as { on: (event: string, cb: (...args: any[]) => void) => void }).on("selection", onSelectionChanged)

    onCleanup(() => {
      const off = (renderer as unknown as { off?: (event: string, cb: (...args: any[]) => void) => void }).off
      if (off) {
        off.call(renderer, "focus", onRendererFocus)
        off.call(renderer, "selection", onSelectionChanged)
      }
    })

    if (props.initialPrompt) {
      setInput(props.initialPrompt)
      textarea?.setText(props.initialPrompt)
      submit(props.initialPrompt)
    }
  })

  createEffect(() => {
    entries()
    if (scroll) scroll.scrollBy(100000)
  })

  const width = createMemo(() => terminal().width)
  const height = createMemo(() => terminal().height)
  const contentHeight = createMemo(() => Math.max(5, height() - 6))

  return (
    <box flexDirection="column" width={width()} height={height()} backgroundColor={theme.background}>
      <box flexDirection="row" paddingLeft={2} paddingRight={2} paddingTop={1} paddingBottom={1}>
        <text fg={theme.muted}>{apiUrl()}</text>
        <box flexGrow={1} />
        <text fg={theme.muted}>session {sessionID()}</text>
      </box>
      <scrollbox
        ref={(val: ScrollBoxRenderable) => (scroll = val)}
        height={contentHeight()}
        paddingLeft={2}
        paddingRight={2}
        paddingTop={1}
      >
        <For each={entries()}>
          {(entry) => (
            <box flexDirection="column" paddingBottom={1}>
              {(entry.role === "tool" || entry.role === "error") && (
                <text
                  fg={entry.role === "error" ? theme.error : theme.tool}
                  attributes={TextAttributes.BOLD}
                >
                  {entry.role === "tool" ? entry.title ?? "Tool" : "Error"}
                </text>
              )}
              {entry.role === "assistant" ? (
                <box flexDirection="column">
                  <For each={renderMarkdownLines(entry.content, { text: theme.text, muted: theme.muted, code: theme.primary })}>
                    {(line) => (
                      <text fg={line.fg ?? theme.text} attributes={line.attributes}>
                        {line.text}
                      </text>
                    )}
                  </For>
                </box>
              ) : entry.role === "tool" ? (
                <box flexDirection="column">
                  <For each={(entry.content ?? "").split("\n")}>
                    {(line) => (
                      <text fg={line.trim().startsWith("Thinking:") ? theme.muted : theme.text}>{line === "" ? " " : line}</text>
                    )}
                  </For>
                </box>
              ) : (
                <text fg={entry.role === "user" ? theme.user : entry.role === "error" ? theme.error : theme.text}>{entry.content}</text>
              )}
            </box>
          )}
        </For>
      </scrollbox>
      {modal() && (
        <box width={width()} height={height()} justifyContent="center" alignItems="center">
          <box
            width={Math.min(80, width() - 4)}
            flexDirection="column"
            paddingLeft={2}
            paddingRight={2}
            paddingTop={1}
            paddingBottom={1}
            backgroundColor={theme.panel}
          >
            <text fg={theme.primary} attributes={TextAttributes.BOLD}>
              {modalCountLabel()}
            </text>
            <text fg={theme.muted}>{modalHint()}</text>
            <input
              ref={(val: InputRenderable) => (modalInputRef = val)}
              value={modalInput()}
              focused={true}
              placeholder={
                modal() === "connect" || modal() === "modelToken"
                  ? "Enter value"
                  : "Search"
              }
              textColor={theme.text}
              focusedTextColor={theme.text}
              cursorColor={theme.primary}
              onInput={(value) => updateModalQuery(value)}
              onSubmit={(value) => handleModalSubmit(typeof value === "string" ? value : modalInput())}
            />
            {modalQuery() && <text fg={theme.text}>query: {modalQuery()}</text>}
            <scrollbox height={modalListHeight()} paddingLeft={1} paddingRight={1}>
              <For each={modalWindow().items}>
                {(item, index) => {
                  const selected = () => modalWindow().start + index() === modalSelected()
                  const prefix = () => (selected() ? ">" : " ")
                  const current = () => modal() === "sessions" && item.id === sessionID()
                  const number = () => modalWindow().start + index() + 1
                  return (
                    <text fg={selected() ? theme.primary : theme.text}>
                      {`${prefix()} ${number()}. ${item.title}${current() ? " (current)" : ""}${item.subtitle ? ` · ${item.subtitle}` : ""}`}
                    </text>
                  )
                }}
              </For>
            </scrollbox>
            {modalError() && <text fg={theme.error}>{modalError()}</text>}
          </box>
        </box>
      )}
      <box flexDirection="column" paddingLeft={2} paddingRight={2} paddingBottom={1}>
        <textarea
          ref={(val: TextareaRenderable) => (textarea = val)}
          height={3}
          focused={!modal()}
          textColor={theme.text}
          focusedTextColor={theme.text}
          cursorColor={theme.primary}
          onContentChange={() => {
            if (suppressHistoryChange) return
            if (historyIndex() !== null) {
              setHistoryIndex(null)
            }
            setHistoryDraft(textarea?.plainText ?? "")
          }}
          keyBindings={[
            { name: "return", action: "submit" },
            { name: "return", shift: true, action: "newline" },
            { name: "enter", action: "submit" },
            { name: "enter", shift: true, action: "newline" },
            { name: "kpenter", action: "submit" },
            { name: "kpenter", shift: true, action: "newline" },
            { name: "linefeed", action: "submit" },
            { name: "linefeed", shift: true, action: "newline" },
          ]}
          onSubmit={() => {
            const value = textarea?.plainText ?? input()
            submit(value)
          }}
          placeholder={
            busy()
              ? "Waiting for response..."
              : modal()
                ? modalHint()
                : "Type a message. Enter to send, Shift+Enter for newline"
          }
        />
        <box flexDirection="row" paddingTop={1}>
          <text fg={theme.muted}>Ctrl+N new session · Ctrl+C exit · Ctrl+Y copy selection</text>
        </box>
      </box>
    </box>
  )
}
