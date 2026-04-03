import { createEffect, createMemo, createSignal, For, onCleanup, onMount } from "solid-js"
import { useKeyboard, useRenderer, useTerminalDimensions } from "@opentui/solid"
import { InputRenderable, PasteEvent, ScrollBoxRenderable, TextareaRenderable } from "@opentui/core"
import { sha256Hex, formatToken, type AttachmentInput } from "./util/helpers"
import { Selection } from "./util/selection"
import { Clipboard } from "./util/clipboard"
import { mkdtemp, readFile, rm, writeFile } from "fs/promises"
import { appendFileSync } from "fs"
import { tmpdir } from "os"
import { basename, join } from "path"
import { spawn } from "child_process"
import {
  createClient,
  cancelRun,
  getRun,
  getLatestRun,
  listRuns,
  replStream,
  streamRunEvents,
  type ApiResponse,
  type ConfirmationPayload,
  type StreamEvent,
  type TeamTaskEvent,
  type TodoItem,
  type ToolResult,
  type PlanStep,
  type SessionInfo,
  type SkillMetadata,
  type ModelsResponse,
  type MetricsResponse,
  type PlanResponse,
  type RemoteFileResponse,
  type SSHInfoResponse,
} from "./api"
import { formatToolOutput } from "./format"
import type { Entry } from "./types"
import { parseTranscriptToEntries } from "./transcript"
import { theme } from "./theme"
import { StatusBar, ChatEntries } from "./components/ChatComponents"
import { TodoList } from "./components/TodoComponents"
import { TeamTaskPanel } from "./components/TeamTaskComponents"
import { Modal } from "./components/ModalComponent"

type AppProps = {
  apiUrl: string
  sessionID: string
  initialPrompt?: string
  debugStream?: boolean
}

type ModalKind = "sessions" | "runs" | "timeline" | "skills" | "models" | "connect" | "modelToken" | "help" | "confirm"

type ModalItem = {
  id: string
  title: string
  subtitle?: string
  meta?: string
  status?: string
  provider?: string
  model?: string
  hasToken?: boolean
}

const parseAttachmentPaths = (text: string): AttachmentInput[] => {
  const paths: AttachmentInput[] = []
  const lines = text.split("\n")
  
  for (const line of lines) {
    const trimmed = line.trim()
    
    // Handle @ syntax - only for valid file paths
    if (trimmed.startsWith("@") && trimmed.length > 1) {
      const filePath = trimmed.slice(1).trim()
      if (filePath && (filePath.startsWith("/") || filePath.startsWith("~") || filePath.includes("/") || filePath.includes("\\"))) {
        const expanded = filePath.replace(/^~/, process.env.HOME || "")
        paths.push({ path: expanded })
      }
      continue
    }
  }
  
  return paths
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
  const [agentMode, setAgentMode] = createSignal<"build" | "plan">("build")
  const [inputHistory, setInputHistory] = createSignal<string[]>([])
  const [historyIndex, setHistoryIndex] = createSignal<number | null>(null)
  const [historyDraft, setHistoryDraft] = createSignal("")
  const [modal, setModal] = createSignal<ModalKind | null>(null)
  const [modalTitle, setModalTitle] = createSignal("")
  const [modalHint, setModalHint] = createSignal("")
  const [modalQuery, setModalQuery] = createSignal("")
  const [modalItems, setModalItems] = createSignal<ModalItem[]>([])
  const [runsNextCursor, setRunsNextCursor] = createSignal<string | null>(null)
  const [runsStatusFilter, setRunsStatusFilter] = createSignal("")
  const [activeRunBanner, setActiveRunBanner] = createSignal<string | null>(null)
  const [activeTodos, setActiveTodos] = createSignal<TodoItem[]>([])
  const [activeStreamToken, setActiveStreamToken] = createSignal<string | null>(null)
  const [runTimeline, setRunTimeline] = createSignal<string | null>(null)
  const [runTimelineID, setRunTimelineID] = createSignal<string | null>(null)
  const [modalError, setModalError] = createSignal("")
  const [notification, setNotification] = createSignal<string | null>(null)
  let notificationTimeout: ReturnType<typeof setTimeout> | undefined
  const [pendingModel, setPendingModel] = createSignal<{ provider: string; model: string } | null>(null)
  const [modalSelected, setModalSelected] = createSignal(0)
  const [modalInput, setModalInput] = createSignal("")
  const [confirmPayload, setConfirmPayload] = createSignal<ConfirmationPayload | null>(null)
  const [lastConfirmID, setLastConfirmID] = createSignal<string | null>(null)
  const [pendingConfirmation, setPendingConfirmation] = createSignal<ConfirmationPayload | null>(null)
  const [expandedToolEntries, setExpandedToolEntries] = createSignal<Set<string>>(new Set())
  const [queuedRequests, setQueuedRequests] = createSignal<{ id: string; text: string }[]>([])
  const [monitorActive, setMonitorActive] = createSignal(false)
  const [attachments, setAttachments] = createSignal<AttachmentInput[]>([])
  const [escapePressed, setEscapePressed] = createSignal(false)
  const [isTyping, setIsTyping] = createSignal(false)
  const [serverMetrics, setServerMetrics] = createSignal<MetricsResponse | null>(null)
  const [currentModel, setCurrentModel] = createSignal<string>("")
  const [todoPulse, setTodoPulse] = createSignal(false)
  const [activeTeamTasks, setActiveTeamTasks] = createSignal<TeamTaskEvent[]>([])
  const [teamTaskPulse, setTeamTaskPulse] = createSignal(false)
  let monitorInterval: ReturnType<typeof setInterval> | undefined
  let metricsInterval: ReturnType<typeof setInterval> | undefined
  let todoPulseInterval: ReturnType<typeof setInterval> | undefined
  let teamTaskPulseInterval: ReturnType<typeof setInterval> | undefined
  let abortController: AbortController | undefined
  let escapeTimeoutRef: ReturnType<typeof setTimeout> | undefined
  let inputQueue: { text: string; entryId: string }[] = []
  let isProcessingQueue = false
  let modalInputRef: InputRenderable | undefined
  let modalSearchTimer: ReturnType<typeof setTimeout> | undefined
  let scroll: ScrollBoxRenderable | undefined
  let textarea: TextareaRenderable | undefined
  let suppressHistoryChange = false
  const toolEntryByCallID = new Map<string, string>()
  const processNextQueuedRequest = () => {
    if (inputQueue.length === 0 || isProcessingQueue) return false
    const next = inputQueue.shift()
    if (!next) return false
    isProcessingQueue = true
    queueMicrotask(async () => {
      try {
        await submit(next.text, next.entryId)
      } finally {
        isProcessingQueue = false
        if (!busy()) processNextQueuedRequest()
      }
    })
    return true
  }

  const finishCurrentAndMaybeRunNext = () => {
    setBusy(false)
    processNextQueuedRequest()
  }

  const syncQueuedConversationEntry = () => {
    const items = queuedRequests()
    const queueID = "__queued_requests__"
    if (items.length === 0) {
      setEntries((prev) => prev.filter((entry) => entry.id !== queueID))
      return
    }
    const content = `Queued\n${items.map((item) => `- ${item.text}`).join("\n")}`
    setEntries((prev) => {
      const withoutQueue = prev.filter((entry) => entry.id !== queueID)
      return [...withoutQueue, { id: queueID, role: "queue", content }]
    })
  }

  createEffect(() => {
    queuedRequests()
    syncQueuedConversationEntry()
  })

  const debugEnabled = () => props.debugStream === true

  const debugLog = (label: string, payload?: unknown) => {
    if (!debugEnabled()) return
    const stamp = new Date().toISOString()
    const prefix = `[cli-debug ${stamp}] ${label}`
    let line = prefix
    if (payload === undefined) {
      appendFileSync("/tmp/morpheus-cli-debug.log", `${line}\n`)
      return
    }
    try {
      line = `${prefix} ${JSON.stringify(payload)}`
    } catch {
      line = `${prefix} ${String(payload)}`
    }
    appendFileSync("/tmp/morpheus-cli-debug.log", `${line}\n`)
  }

  const setTextareaValue = (value: string) => {
    suppressHistoryChange = true
    setInput(value)
    textarea?.setText(value)
    if (textarea) textarea.cursorOffset = textarea.plainText.length
    suppressHistoryChange = false
  }

  const syncInputDraft = () => {
    setInput(textarea?.plainText ?? "")
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

  type ClientType = ReturnType<typeof createClient>

  const formatMetrics = (metrics: MetricsResponse, cmd: string) => {
    const fmt = (n?: number) => n != null ? n.toFixed(1) : "N/A"
    const parts: string[] = []
    if (metrics.uptime_seconds) {
      const h = Math.floor(metrics.uptime_seconds / 3600)
      const m = Math.floor((metrics.uptime_seconds % 3600) / 60)
      parts.push(`uptime: ${h}h ${m}m`)
    }
    if (metrics.processed_requests != null) {
      parts.push(`requests: ${metrics.processed_requests}`)
    }
    if (metrics.memory?.heap_alloc_mb) {
      parts.push(`heap: ${fmt(metrics.memory.heap_alloc_mb)}MB`)
    }
    if (metrics.runtime?.goroutines) {
      parts.push(`goroutines: ${metrics.runtime.goroutines}`)
    }
    if (metrics.resource?.cpu_percent != null) {
      parts.push(`cpu: ${fmt(metrics.resource.cpu_percent)}%`)
    }
    if (metrics.resource?.mem_percent != null) {
      parts.push(`mem: ${fmt(metrics.resource.mem_percent)}%`)
    }
    return `▣ ${cmd}\n${parts.join(" · ")}`
  }

  const appendEntry = (entry: Entry) => {
    debugLog("appendEntry", { role: entry.role, kind: entry.kind, title: entry.title, preview: entry.content.slice(0, 240) })
    setEntries((prev) => {
      const queueID = "__queued_requests__"
      const queueIndex = prev.findIndex((item) => item.id === queueID)
      if (queueIndex < 0 || entry.id === queueID) return [...prev.filter((item) => item.id !== queueID), entry]
      const withoutQueue = prev.filter((item) => item.id !== queueID)
      return [...withoutQueue, entry, prev[queueIndex]]
    })
    queueMicrotask(() => {
      if (scroll && !isTyping()) scroll.scrollBy(100000)
      renderer.requestRender()
    })
  }

  const updateEntryContent = (id: string, content: string) => {
    debugLog("updateEntryContent", { id, preview: content.slice(0, 240) })
    setEntries((prev) => prev.map((e) => (e.id === id ? { ...e, content } : e)))
    queueMicrotask(() => renderer.requestRender())
  }

  const updateEntry = (id: string, updater: (entry: Entry) => Entry) => {
    setEntries((prev) => prev.map((e) => (e.id === id ? updater(e) : e)))
    queueMicrotask(() => renderer.requestRender())
  }

  const formatTodoBlock = (todos: TodoItem[]) => {
    if (todos.length === 0) return ""
    return `${todos.map((todo) => {
      const mark = todo.status === "completed" ? "[x]" : todo.status === "in_progress" ? "[•]" : todo.status === "failed" ? "[!]" : todo.status === "cancelled" ? "[-]" : "[ ]"
      const suffix = todo.tool ? ` (${todo.tool})` : ""
      const note = todo.note ? ` - ${todo.note}` : ""
      return `${mark} ${todo.content}${suffix}${note}`
    }).join("\n")}`
  }

  const todoLineColor = (todo: TodoItem, pulse: boolean) => {
    if (todo.status === "completed") return theme.todoDone
    if (todo.status === "failed") return theme.todoFailed
    if (todo.status === "cancelled") return theme.todoCancelled
    if (todo.status === "in_progress") return pulse ? theme.primary : theme.todoActive
    return theme.todoPending
  }

  const isConfirmationPrompt = (reply: string) => {
    if (!reply) return false
    if (/^\s*#\s*confirmation required/im.test(reply)) return true
    if (/\bconfirmation required\b/i.test(reply)) return true
    if (/type `approve`/i.test(reply)) return true
    if (/reply 'approve'/i.test(reply)) return true
    return false
  }


  const isToolExpanded = (id: string) => expandedToolEntries().has(id)

  const toggleToolExpanded = (id: string) => {
    setExpandedToolEntries((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const toggleAgentMode = () => {
    setAgentMode((prev) => (prev === "build" ? "plan" : "build"))
  }

  const agentModeLabel = () => (agentMode() === "build" ? "Build" : "Plan")
  const agentModeIcon = () => "◆"
  const agentModeColor = () => (agentMode() === "build" ? "#2f5fd7" : "#2f8f4e")

  const submit = async (text: string, queuedEntryId?: string) => {
    const trimmed = text.trim()
    if (!trimmed) return

    const pending = pendingConfirmation()
    if (pending) {
      const normalized = trimmed.toLowerCase()
      const isApproval = ["yes", "y", "approve", "approved", "allow", "ok", "confirm", "proceed", "continue"].includes(normalized)
      const isDenial = ["no", "n", "deny", "denied", "cancel", "stop"].includes(normalized)
      if (isApproval || isDenial) {
        appendEntry({ id: crypto.randomUUID(), role: "user", content: trimmed })
        setPendingConfirmation(null)
        const decision = isApproval ? "approve" : "deny"
        appendEntry({ id: crypto.randomUUID(), role: "assistant", content: isApproval ? "Approved. Executing..." : "Denied. Cancelled." })
        setBusy(true)
        try {
          const response = await client().repl({ session: sessionID(), input: decision, mode: agentMode() })
          if (response.reply) {
            appendEntry({ id: crypto.randomUUID(), role: "assistant", content: response.reply })
          }
          if (response.confirmation) {
            openConfirmModal(response.confirmation)
          }
        } catch (err) {
          appendEntry({
            id: crypto.randomUUID(),
            role: "error",
            content: err instanceof Error ? err.message : String(err),
          })
        }
        finishCurrentAndMaybeRunNext()
        return
      }
    }

    // Record input before we clear it.
    if (!modal()) {
      rememberInput(text)
      setHistoryIndex(null)
      setHistoryDraft("")
    }

    // Get current attachments and clear for next input
    const currentAttachments = attachments()
    setAttachments([])

    syncInputDraft()
    setInput("")
    textarea?.clear()
    if (modal()) {
      handleModalSubmit(trimmed)
      return
    }
    if (trimmed.startsWith("/")) {
      const handled = await handleCommand(trimmed)
      if (handled) return
    }

    if (trimmed.startsWith("!")) {
      const shellCmd = trimmed.slice(1).trim()
      if (!shellCmd) {
        appendEntry({ id: crypto.randomUUID(), role: "error", content: "Shell command cannot be empty" })
        return
      }
      appendEntry({ id: crypto.randomUUID(), role: "assistant", content: `$ ${shellCmd}` })
      setBusy(true)
      try {
        const result = await client().shell(shellCmd)
        if (result.stderr && !result.stdout) {
          appendEntry({ id: crypto.randomUUID(), role: "error", content: result.stderr })
        } else if (result.stdout) {
          appendEntry({ id: crypto.randomUUID(), role: "assistant", content: result.stdout })
        }
      } catch (err) {
        appendEntry({
          id: crypto.randomUUID(),
          role: "error",
          content: err instanceof Error ? err.message : String(err),
        })
      }
      finishCurrentAndMaybeRunNext()
      return
    }

    // Allow slash-commands (like /exit) even while busy,
    // but avoid sending a new prompt while a request is in-flight.
    // Exception: confirmation responses (approve/deny) should not be queued.
    if (busy() && trimmed !== "approve" && trimmed !== "deny") {
      const entryId = crypto.randomUUID()
      inputQueue.push({ text: trimmed, entryId })
      setQueuedRequests((prev) => [...prev, { id: entryId, text: trimmed }])
      return
    }
    setBusy(true)
    abortController = new AbortController()
    const entryId = queuedEntryId || crypto.randomUUID()
    if (queuedEntryId) {
      setQueuedRequests((prev) => prev.filter((item) => item.id !== queuedEntryId))
    }
    appendEntry({ id: entryId, role: "user", content: trimmed })

    let assistantReplyText = ""
    let finalReply: string | null = null
    let finalReplyRendered = false
    let finalErrorText: string | null = null
    let currentRunID: string | null = null
    let lastPhaseNote: string | null = null
    let lastLoopEvent: string | null = null
    let fallbackRendered = false
    const streamToken = crypto.randomUUID()
    setActiveStreamToken(streamToken)

    const isCurrentStream = () => activeStreamToken() === streamToken

    const formatFailureSummary = (message: string) => {
      const detail = message.trim() || "No additional error details were provided."
      return `Issue:\n- ${detail}\n\nTried:\n- Completed this reasoning pass and any needed tool calls\n- Continued or retried based on the returned results\n\nSuggestions:\n- Narrow the request and try again\n- If this depends on external data or the network, try again shortly`
    }

    const formatSuccessSummary = (text: string) => {
      const trimmed = cleanFinalAnswer(text)
      if (!trimmed) return ""
      return trimmed
    }

    const cleanFinalAnswer = (text: string) => {
      let cleaned = text.trim()
      cleaned = cleaned.replace(/^Tool call:\s*web\.fetch\s*/i, "")
      cleaned = cleaned.replace(/^Tool call:\s*cmd\.exec\s*/i, "")
      const parts = cleaned
        .split(/\n{2,}/)
        .map((part) => part.trim())
        .filter(Boolean)
      const deduped: string[] = []
      for (const part of parts) {
        if (!deduped.includes(part)) deduped.push(part)
      }
      return deduped.join("\n\n")
    }

    const appendFinalSummary = (text: string) => {
      if (!isCurrentStream()) return
      if (finalReplyRendered || !text.trim()) return
      appendEntry({ id: crypto.randomUUID(), role: "assistant", content: text, kind: "summary" })
      finalReplyRendered = true
    }

    const renderRunSnapshotIfNeeded = (run: ApiResponse) => {
      if (!isCurrentStream()) return
      if (fallbackRendered) return
      const steps = run.plan?.steps ?? []
      const results = run.results ?? []
      if (steps.length > 0) {
        appendToolEntries(steps, results)
      }
      const reply = run.reply?.trim()
      if (reply) {
        assistantReplyText = run.reply
        finalReply = run.reply
      }
      fallbackRendered = steps.length > 0 || Boolean(reply)
    }

    const appendLoopNote = (eventKey: string, text: string) => {
      if (!isCurrentStream()) return
      if (lastLoopEvent === eventKey) return
      const normalized = text.trim()
      if (!normalized || normalized === "Thinking:" || normalized === "Thinking:\n") {
        lastLoopEvent = eventKey
        return
      }
      if (text.includes("working through step") || text.includes("I know the next concrete action") || text.includes("tool finished") || text.includes("I have enough information to produce the answer")) {
        lastLoopEvent = eventKey
        return
      }
      appendEntry({ id: crypto.randomUUID(), role: "assistant", content: decorateThinkingText(text), kind: "thinking" })
      lastLoopEvent = eventKey
    }

    const startConfirmationModal = () => {
      if (!isCurrentStream()) return
      openConfirmModal(confirmPayload())
    }

    const maybePromptConfirmation = (text: string) => {
      if (isConfirmationPrompt(text)) {
        startConfirmationModal()
      }
    }

  const normalizeThinkingText = (text: string) => {
    const trimmed = text.trimStart()
    if (!trimmed) return text
    if (trimmed.toLowerCase().startsWith("thinking:")) return text
    return `Thinking: ${text}`
  }

  const decorateThinkingText = (text: string) => {
    const normalized = normalizeThinkingText(text)
    if (normalized.startsWith("Thinking:\n")) return normalized
    return normalized.replace(/^Thinking:\s*/, "Thinking:\n")
  }

    const phaseNoteForTool = (tool: string) => {
      if (tool === "web.fetch") return "Thinking: querying live information."
      if (tool.includes("read") || tool.includes("grep") || tool.includes("glob")) return "Thinking: checking the relevant code and context."
      if (tool.includes("write") || tool.includes("edit") || tool.includes("patch")) return "Thinking: applying the change directly."
      if (tool.includes("exec") || tool.includes("bash") || tool.includes("test") || tool.includes("build")) return "Thinking: verifying the change with commands."
      return "Thinking: working through the next step."
    }

    const onStreamEvent = (evt: StreamEvent) => {
      if (!isCurrentStream()) return
      debugLog("streamEvent", { event: evt.event, data: evt.data })
      if (evt.event === "run_event") {
        currentRunID = evt.data.run_id
        const runType = evt.data.type
        if (runType === "run_finished") {
          const data = (evt.data.data ?? {}) as Record<string, unknown>
          finalReply = typeof data.reply === "string" ? data.reply : finalReply
          if (typeof data.confirmation === "object" && data.confirmation) {
            setActiveRunBanner("Awaiting confirmation")
          } else {
            setActiveRunBanner(null)
          }
        }
        if (runType === "thinking_started") {
          const route = String((evt.data.data ?? {})["route"] ?? "")
          if (route !== "fresh_info") {
            const message = String((evt.data.data ?? {})["message"] ?? "").trim()
            if (message && message !== "Starting the task and checking context.") appendLoopNote("thinking_started", message)
          }
          return
        }
        if (runType === "tool_execution_started") {
          const tool = String((evt.data.data ?? {})["tool"] ?? "tool")
          setActiveRunBanner(`Executing ${tool}...`)
          return
        }
        if (runType === "tool_execution_finished") {
          const data = (evt.data.data ?? {}) as Record<string, unknown>
          const tool = String(data["tool"] ?? "tool")
          const success = Boolean(data["success"])
          setActiveRunBanner(success ? `Finished ${tool}` : `${tool} failed`)
          appendLoopNote(`tool-finish:${evt.data.seq}`, "Thinking: tool finished, continuing with the next step.")
          return
        }
        if (runType === "run_started") {
          setActiveRunBanner("Run started")
          return
        }
        if (runType === "todos_updated") {
          const todos = Array.isArray((evt.data.data ?? {})["todos"]) ? ((evt.data.data ?? {})["todos"] as TodoItem[]) : []
          setActiveTodos(todos)
          return
        }
        if (runType === "model_turn_finished") {
          const toolCalls = Number((evt.data.data ?? {})["tool_calls"] ?? 0)
          if (toolCalls === 0) {
            appendLoopNote(`model-finished:${evt.data.seq}`, "Thinking: I have enough information to produce the answer.")
          } else {
            appendLoopNote(`model-finished:${evt.data.seq}`, "Thinking: I know the next concrete action to take.")
          }
        }
        if (runType === "run_recovered") {
          setActiveRunBanner("Recovered run after restart")
          appendEntry({ id: crypto.randomUUID(), role: "system", content: "Recovered an interrupted run after restart." })
        }
        if (runType === "model_turn_started") {
          const step = String((evt.data.data ?? {})["step"] ?? "")
          appendLoopNote(`model-turn:${step}`, `Thinking: working through step ${step || "1"}.`)
        }
        if (runType === "run_failed") {
          const error = String((evt.data.data ?? {})["error"] ?? "Unknown error")
          finalErrorText = error
          appendEntry({ id: crypto.randomUUID(), role: "error", content: `Run failed: ${error}` })
        }
        if (runType === "run_loop_detected") {
          const data = (evt.data.data ?? {}) as Record<string, unknown>
          const reply = String(data["reply"] ?? "Stopped to avoid a repeated loop.")
          finalErrorText = reply
          appendEntry({ id: crypto.randomUUID(), role: "error", content: reply })
        }
        if (runType === "run_cancelled") {
          finalErrorText = "Run cancelled."
          appendEntry({ id: crypto.randomUUID(), role: "system", content: "Run cancelled." })
        }
        if (runType === "run_waiting_user") {
          appendEntry({ id: crypto.randomUUID(), role: "system", content: "Run is waiting for your confirmation or input." })
        }
        return
      }
      if (evt.event === "confirmation") {
        openConfirmModal(evt.data)
        return
      }
      if (evt.event === "assistant_delta") {
        const delta = evt.data.text ?? ""
        if (isConfirmationPrompt(delta)) {
          assistantReplyText += delta
          startConfirmationModal()
          return
        }
        assistantReplyText += delta
        maybePromptConfirmation(assistantReplyText)
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
        const phase = phaseNoteForTool(evt.data.tool)
        if (lastPhaseNote !== phase) {
          appendLoopNote(`phase:${evt.data.tool}`, phase)
          lastPhaseNote = phase
        }
        appendEntry({ id: entryID, role: "tool", content: formatToolOutput(step, null) })
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
            content: formatToolOutput(evt.data.step, evt.data.result),
          })
        }
        return
      }
      if (evt.event === "error") {
        appendEntry({ id: crypto.randomUUID(), role: "error", content: evt.data.error })
        return
      }
      if (evt.event === "team_plan") {
        const tasks = evt.data.tasks as TeamTaskEvent[]
        if (tasks && tasks.length > 0) {
          setActiveTeamTasks(tasks)
          setTeamTaskPulse(true)
        }
        return
      }
      if (evt.event === "team_task_started" || evt.event === "team_task_finished" || evt.event === "team_task_error") {
        const task = evt.data as TeamTaskEvent
        setActiveTeamTasks(prev => {
          const existing = prev.findIndex(t => t.id === task.id)
          if (existing >= 0) {
            const updated = [...prev]
            updated[existing] = task
            return updated
          }
          return [...prev, task]
        })
        if (evt.event === "team_task_finished" || evt.event === "team_task_error") {
          setActiveRunBanner(evt.event === "team_task_error" ? `Task ${task.id} failed` : `Task ${task.id} completed`)
        }
        return
      }
      if (evt.event === "done") {
        return
      }
    }

    try {
      await replStream(apiUrl(), { session: sessionID(), input: trimmed, mode: agentMode(), attachments: currentAttachments }, onStreamEvent, abortController?.signal)
      if (!isCurrentStream()) {
        setBusy(false)
        return
      }
      if (currentRunID) {
        const run = await getRun(apiUrl(), currentRunID)
        if (!isCurrentStream()) {
          setBusy(false)
          return
        }
        if (!assistantReplyText.trim() && (run.plan?.steps?.length || run.results?.length || run.reply)) {
          renderRunSnapshotIfNeeded(run)
        }
        if (run.confirmation) openConfirmModal(run.confirmation)
        finalReply = run.reply ?? finalReply
      }
      if (finalReply) {
        appendFinalSummary(formatSuccessSummary(finalReply))
        maybePromptConfirmation(finalReply)
      } else if (finalErrorText) {
        appendFinalSummary(formatFailureSummary(finalErrorText))
      }
    } catch (err) {
      if (err instanceof Error && (err.name === "AbortError" || err.message?.includes("aborted"))) {
        if (currentRunID) {
          try {
            await cancelRun(apiUrl(), currentRunID)
          } catch {
            // ignore cancel failures
          }
        }
        if (isCurrentStream()) setActiveStreamToken(null)
        finishCurrentAndMaybeRunNext()
        return
      }
      if (!abortController?.signal.aborted) {
        // Fallback to non-streaming endpoint.
        try {
          const response = await client().repl({ session: sessionID(), input: trimmed, mode: agentMode(), attachments: currentAttachments })
          if (!isCurrentStream()) {
            setBusy(false)
            return
          }
          appendToolEntries(response.plan?.steps ?? [], response.results ?? [])
          if (response.reply) {
            assistantReplyText = response.reply
            appendFinalSummary(formatSuccessSummary(assistantReplyText))
            maybePromptConfirmation(response.reply)
          }
          if (response.confirmation) {
            openConfirmModal(response.confirmation)
          }
        } catch (err2) {
          if (err2 instanceof Error && (err2.name === "AbortError" || err2.message?.includes("aborted"))) {
            finishCurrentAndMaybeRunNext()
            return
          }
          appendEntry({
            id: crypto.randomUUID(),
            role: "error",
            content: err2 instanceof Error ? err2.message : String(err2),
          })
          finishCurrentAndMaybeRunNext()
          return
        }
        finishCurrentAndMaybeRunNext()
        return
      }
    }

    if (abortController?.signal.aborted) {
      if (isCurrentStream()) setActiveStreamToken(null)
      finishCurrentAndMaybeRunNext()
      return
    }

    // If the stream ended but we didn't get deltas, fall back to the final reply.
    if (finalReply && !finalReplyRendered) {
      appendFinalSummary(formatSuccessSummary(finalReply))
    } else if (finalErrorText && !finalReplyRendered) {
      appendFinalSummary(formatFailureSummary(finalErrorText))
    }

    if (isCurrentStream()) setActiveStreamToken(null)
    finishCurrentAndMaybeRunNext()
  }

  const modalFilteredItems = createMemo(() => {
    const kind = modal()
    if (kind === "sessions" || kind === "runs" || kind === "skills" || kind === "confirm") {
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
    setConfirmPayload(null)
  }

  function startModal(kind: ModalKind, title: string, hint: string, items: ModalItem[] = []) {
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

  const formatConfirmationHint = (payload: ConfirmationPayload | null) => {
    if (!payload) return "Select an option"
    const reason = payload.decision?.reason?.trim()
    const risk = payload.decision?.risk_level?.trim()
    const tool = payload.tool?.trim()
    const command = typeof payload.inputs?.command === "string" ? payload.inputs.command.trim() : ""
    const path = typeof payload.inputs?.path === "string" ? payload.inputs.path.trim() : ""
    const detail = command ? `Command: ${command}` : path ? `Path: ${path}` : ""
    const parts = [tool ? `Tool: ${tool}` : "", risk ? `Risk: ${risk}` : "", detail, reason ?? ""].filter(Boolean)
    return parts.join(" · ") || "Select an option"
  }

  const openConfirmModal = (payload?: ConfirmationPayload | null) => {
    const nextPayload = payload ?? null
    if (nextPayload) {
      setPendingConfirmation(nextPayload)
      const hint = formatConfirmationHint(nextPayload)
      const confirmMsg = `# Confirmation Required\n\n${hint}\n\nPlease type "yes" to approve or "no" to deny.`
      appendEntry({ id: crypto.randomUUID(), role: "assistant", content: confirmMsg })
      setConfirmPayload(null)
      queueMicrotask(() => renderer.requestRender())
      return
    }
    setConfirmPayload(nextPayload)
    if (modal() === "confirm") {
      setModalTitle("Confirmation Required")
      setModalHint(formatConfirmationHint(nextPayload))
      setModalItems([
        { id: "approve", title: "Approve", subtitle: "Proceed with the action" },
        { id: "deny", title: "Deny", subtitle: "Cancel the action" },
      ])
      return
    }
    if (modal()) resetModal()
    startModal("confirm", "Confirmation Required", formatConfirmationHint(nextPayload), [
      { id: "approve", title: "Approve", subtitle: "Proceed with the action" },
      { id: "deny", title: "Deny", subtitle: "Cancel the action" },
    ])
    setModalSelected(0)
  }

  createEffect(() => {
    if (confirmPayload()) return
    const current = entries()
    const latest = [...current].reverse().find((entry) => entry.role === "assistant" && isConfirmationPrompt(entry.content))
    if (!latest) return
    if (lastConfirmID() === latest.id) return
    setLastConfirmID(latest.id)
    openConfirmModal(null)
  })

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
    if (!kind || kind === "connect" || kind === "modelToken" || kind === "confirm" || kind === "timeline") {
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
        if (kind === "runs") {
          const statusFilter = runsStatusFilter() || query
          const { runs, next_cursor } = await listRuns(apiUrl(), sessionID(), statusFilter)
          const items = runs.map((run) => ({
            id: run.run_id ?? crypto.randomUUID(),
            title: run.run_id ?? "unknown-run",
            subtitle: `${run.run_status ?? "unknown"} · ${String((run as Record<string, unknown>).created_at ?? (run as Record<string, unknown>).updated_at ?? "")}`,
            meta: `${String((run as Record<string, unknown>).last_step ?? "-")} ${run.reply ? `· ${run.reply.slice(0, 50)}` : ""} ${String((run as Record<string, unknown>).error ?? "")}`.trim(),
            status: run.run_status,
          }))
          setModalItems(items)
          setRunsNextCursor(next_cursor ?? null)
          setModalSelected(0)
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
    if (kind === "runs") {
      resetModal()
      await handleCommand(`/resume-run ${item.id}`)
      focusActiveInput()
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
    if (kind === "confirm") {
      const decision = item.id === "approve" ? "approve" : "deny"
      resetModal()
      submit(decision)
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
    if (kind === "timeline") {
      if (runTimeline()) {
        void Clipboard.copy(runTimeline() || "")
        setNotification("Timeline copied")
        if (notificationTimeout) clearTimeout(notificationTimeout)
        notificationTimeout = setTimeout(() => setNotification(null), 1500)
      }
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
    syncInputDraft()
    setInput("")
    textarea?.clear()
    return next
  }

  const openLocalVim = async (filePath: string) => {
    const editor = process.env.VISUAL || process.env.EDITOR || "vim"
    renderer.suspend()
    try {
      await new Promise<void>((resolve, reject) => {
        const child = spawn("script", ["-q", "-c", `${editor} "${filePath}"`, "/dev/null"], {
          stdio: "inherit",
          env: { ...process.env, TERM: "xterm-256color" },
        })
        child.on("error", reject)
        child.on("exit", (code) => {
          if (code === 0) resolve()
          else reject(new Error(`${editor} exited with code ${code ?? "unknown"}`))
        })
      })
    } finally {
      renderer.resume()
    }
  }

  const openSSHSession = async (info: SSHInfoResponse) => {
    const target = `${info.user}@${info.host}`
    const remoteCommand = info.workspace
      ? `cd ${JSON.stringify(info.workspace)} && exec \${SHELL:-bash} -l`
      : "exec ${SHELL:-bash} -l"
    const sshArgs = ["ssh", "-t", "-p", "22", target, remoteCommand].join(" ")
    appendEntry({
      id: crypto.randomUUID(),
      role: "assistant",
      content: formatVimStatus("Remote SSH", [
        `command: ssh ${target}`,
        `entering remote session, will return when SSH exits`,
      ]),
    })
    renderer.suspend()
    try {
      process.stdout.write("\x1b[2J\x1b[H")
      console.log(`\nExecuting SSH, press Ctrl+D or type 'exit' to return...\n`)
      const { execSync } = await import("child_process")
      execSync(sshArgs, { stdio: "inherit", env: { ...process.env, TERM: "xterm-256color" } })
    } catch (err) {
      console.error(`SSH exited with error: ${err instanceof Error ? err.message : String(err)}`)
    }
    console.log("\nReturning to morpheus...\n")
    renderer.resume()
    queueMicrotask(() => renderer.requestRender())
  }

  const shellQuote = (value: string) => `'${value.replace(/'/g, `'"'"'`)}'`

  const preserveTempCopy = async (tempFile: string, remotePath: string) => {
    const preservedPath = join(process.cwd(), `.morpheus-vim-${basename(remotePath)}-${Date.now()}`)
    await writeFile(preservedPath, await readFile(tempFile, "utf8"), "utf8")
    return preservedPath
  }

  const formatVimStatus = (title: string, steps: string[]) => {
    return [`# ${title}`, "", ...steps.map((step) => `- ${step}`)].join("\n")
  }

  const infoLikeHostForPath = (baseUrl: string, remotePath: string) => {
    try {
      const url = new URL(baseUrl)
      return `${url.hostname}:${remotePath}`
    } catch {
      return `remote:${remotePath}`
    }
  }

  const appendVimStep = (steps: string[], step: string, entryId?: string) => {
    const next = [...steps, step]
    if (entryId) {
      updateEntryContent(entryId, formatVimStatus("Remote Editing", next))
    }
    return next
  }

  const editRemoteFileInVim = async (remotePath: string) => {
    const normalizedPath = remotePath.trim()
    if (!normalizedPath) {
      appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "Usage: /vim <path>" })
      return
    }

    let remote: RemoteFileResponse
    let isNewFile = false
    try {
      remote = await client().getRemoteFile(normalizedPath)
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : String(err)
      if (errMsg.includes("404") || errMsg.includes("not found") || errMsg.includes("no such file")) {
        isNewFile = true
        remote = { path: normalizedPath, content: "", hash: sha256Hex(""), size: 0 }
      } else {
        appendEntry({
          id: crypto.randomUUID(),
          role: "error",
          content: errMsg,
        })
        return
      }
    }

    const tempDir = await mkdtemp(join(tmpdir(), "morpheus-vim-"))
    const tempFile = join(tempDir, basename(normalizedPath) || "remote-file")
    await writeFile(tempFile, remote.content, "utf8")
    const statusEntryId = crypto.randomUUID()
    let steps = [
      `command: vim ${infoLikeHostForPath(apiUrl(), normalizedPath)}`,
      `resolved remote path: ${normalizedPath}`,
      `fetched remote content to local temp file`,
      `opening local vim editor`,
    ]
    appendEntry({
      id: statusEntryId,
      role: "assistant",
      content: formatVimStatus("Remote Editing", steps),
    })

    try {
      await openLocalVim(tempFile)
      steps = appendVimStep(steps, "editor closed", statusEntryId)
      const edited = await readFile(tempFile, "utf8")
      if (sha256Hex(edited) === remote.hash) {
        appendVimStep(steps, "no changes detected; skipped remote sync", statusEntryId)
        await rm(tempDir, { recursive: true, force: true })
        return
      }
      steps = appendVimStep(steps, "detected local modifications", statusEntryId)
      try {
        await client().putRemoteFile(normalizedPath, edited, remote.hash)
        steps = appendVimStep(steps, isNewFile ? "created remote file" : "uploaded edited content", statusEntryId)
        appendVimStep(steps, "remote sync completed", statusEntryId)
        await rm(tempDir, { recursive: true, force: true })
      } catch (err) {
        const preservedPath = await preserveTempCopy(tempFile, normalizedPath)
        steps = appendVimStep(steps, `sync failed: ${err instanceof Error ? err.message : String(err)}`, statusEntryId)
        updateEntryContent(statusEntryId, formatVimStatus("Remote Editing", [...steps, `local copy preserved: ${preservedPath}`]))
      }
    } catch (err) {
      const preservedPath = await preserveTempCopy(tempFile, normalizedPath)
      steps = appendVimStep(steps, `editor failed: ${err instanceof Error ? err.message : String(err)}`, statusEntryId)
      updateEntryContent(statusEntryId, formatVimStatus("Remote Editing", [...steps, `local copy preserved: ${preservedPath}`]))
    }
  }

  const handleCommand = async (command: string): Promise<boolean> => {
    const [name, ...args] = command.split(" ")
    switch (name) {
      case "/exit":
        process.exit(0)
        return true
      case "/help":
        appendEntry({
          id: crypto.randomUUID(),
          role: "assistant",
          content:
            "Commands:\n  /new\n  /sessions\n  /skills\n  /models\n  /monitor\n  /resume\n  /plan <prompt>\n  /team\n  /team tasks\n  /team messages\n  /vim <path>\n  /ssh\n  /connect <url>\n  /help\n  /exit\n\nOther /<skill> commands will run the matching skill if available.\n\nKeys:\n  Tab toggle mode",
        })
        return true
      case "/resume": {
        const { runs } = await listRuns(apiUrl(), sessionID())
        if (runs.length === 0) {
          const latest = await getLatestRun(apiUrl(), sessionID())
          if (!latest?.run_id) {
            appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "No resumable run found." })
            return true
          }
          runs.push(latest)
        }
        const items = runs.map((run) => ({
          id: run.run_id ?? crypto.randomUUID(),
          title: run.run_id ?? "unknown-run",
          subtitle: `${run.run_status ?? "unknown"} · ${String((run as Record<string, unknown>).created_at ?? (run as Record<string, unknown>).updated_at ?? "")}`,
          meta: `${String((run as Record<string, unknown>).last_step ?? "-")} ${run.reply ? `· ${run.reply.slice(0, 50)}` : ""} ${String((run as Record<string, unknown>).error ?? "")}`.trim(),
          status: run.run_status,
        }))
        startModal("runs", "Runs", "Select a run to resume", items)
        setRunsNextCursor(null)
        setRunsStatusFilter("")
        setModalInput("")
        queueMicrotask(() => modalInputRef?.focus())
        return true
      }
      case "/resume-run": {
        const runID = args.join(" ").trim()
        if (!runID) {
          appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "No resumable run found." })
          return true
        }
        let resumedReply: string | null = null
        let replaySeq = 0
        while (true) {
          const replay = await streamRunEvents(apiUrl(), runID, replaySeq, (evt: StreamEvent) => {
          if (evt.event === "run_event" && evt.data.type === "run_finished") {
            const data = (evt.data.data ?? {}) as Record<string, unknown>
            resumedReply = typeof data.reply === "string" ? data.reply : resumedReply
          }
          })
          replaySeq = replay.lastSeq
          if (resumedReply) break
          const latest = await getRun(apiUrl(), runID)
          if (latest.run_status === "completed" || latest.run_status === "failed" || latest.run_status === "cancelled" || latest.run_status === "timed_out") {
            resumedReply = latest.reply ?? resumedReply
            break
          }
        }
        const finalRun = await getRun(apiUrl(), runID)
        appendEntry({
          id: crypto.randomUUID(),
          role: "assistant",
          content:
            finalRun.run_status === "waiting_user"
              ? `This run is awaiting confirmation or user input.\n${resumedReply ?? finalRun.reply ?? ""}`.trim()
              : resumedReply ?? finalRun.reply ?? `Resumed run ${runID} (${finalRun.run_status ?? "unknown"}).`,
        })
        return true
      }
      case "/ssh": {
        try {
          const info = await client().sshInfo()
          appendEntry({
            id: crypto.randomUUID(),
            role: "assistant",
            content: `Opening SSH session to ${info.user}@${info.host}:22`,
          })
          await openSSHSession(info)
        } catch (err) {
          appendEntry({
            id: crypto.randomUUID(),
            role: "error",
            content: err instanceof Error ? err.message : String(err),
          })
        }
        return true
      }
      case "/vim": {
        await editRemoteFileInVim(args.join(" "))
        return true
      }
      case "/monitor": {
        if (monitorActive()) {
          if (monitorInterval) {
            clearInterval(monitorInterval)
            monitorInterval = undefined
          }
          setMonitorActive(false)
          appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "Monitor stopped." })
          return true
        }
        setMonitorActive(true)
        let monitorCount = 0
        let monitorEntryID = crypto.randomUUID()
        let monitorLines: string[] = []
        appendEntry({ id: monitorEntryID, role: "assistant", content: "▣ /monitor" })
        const formatMetricsOnly = (metrics: MetricsResponse) => {
          const fmt = (n?: number) => n != null ? n.toFixed(1) : "N/A"
          const parts: string[] = []
          if (metrics.uptime_seconds) {
            const h = Math.floor(metrics.uptime_seconds / 3600)
            const m = Math.floor((metrics.uptime_seconds % 3600) / 60)
            parts.push(`uptime: ${h}h ${m}m`)
          }
          if (metrics.processed_requests != null) {
            parts.push(`requests: ${metrics.processed_requests}`)
          }
          if (metrics.memory?.heap_alloc_mb) {
            parts.push(`heap: ${fmt(metrics.memory.heap_alloc_mb)}MB`)
          }
          if (metrics.runtime?.goroutines) {
            parts.push(`goroutines: ${metrics.runtime.goroutines}`)
          }
          if (metrics.resource?.cpu_percent != null) {
            parts.push(`cpu: ${fmt(metrics.resource.cpu_percent)}%`)
          }
          if (metrics.resource?.mem_percent != null) {
            parts.push(`mem: ${fmt(metrics.resource.mem_percent)}%`)
          }
          return parts.join(" · ")
        }
        const updateMonitor = async () => {
          if (!monitorActive()) return
          monitorCount++
          if (monitorCount >= 30) {
            if (monitorInterval) {
              clearInterval(monitorInterval)
              monitorInterval = undefined
            }
            setMonitorActive(false)
            appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "Monitor stopped (timeout)." })
            return
          }
          try {
            const metrics = await client().metrics()
            monitorLines.push(formatMetricsOnly(metrics))
            if (monitorLines.length > 10) monitorLines.shift()
            updateEntryContent(monitorEntryID, `▣ /monitor\n${monitorLines.join("\n")}`)
          } catch {
            if (monitorInterval) {
              clearInterval(monitorInterval)
              monitorInterval = undefined
            }
            setMonitorActive(false)
          }
        }
        await updateMonitor()
        monitorInterval = setInterval(updateMonitor, 1000)
        return true
      }
      case "/plan": {
        const prompt = args.join(" ").trim()
        if (!prompt) {
          appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "Usage: /plan <prompt>" })
          return true
        }
        try {
          const plan = await client().plan({ session: sessionID(), input: prompt })
          let output = `Plan:\n${plan.summary}\n\nSteps:`
          for (let i = 0; i < plan.steps.length; i++) {
            const step = plan.steps[i]
            output += `\n${i + 1}. ${step.tool}: ${step.description}`
          }
          appendEntry({ id: crypto.randomUUID(), role: "assistant", content: output })
        } catch (err) {
          appendEntry({
            id: crypto.randomUUID(),
            role: "error",
            content: err instanceof Error ? err.message : String(err),
          })
        }
        return true
      }
      case "/new": {
        const next = newSession()
        appendEntry({ id: crypto.randomUUID(), role: "assistant", content: `New session ${next}` })
        return true
      }
      case "/connect": {
        const url = args.join(" ").trim()
        if (url) {
          setApiUrl(url)
          appendEntry({ id: crypto.randomUUID(), role: "assistant", content: `Connected to ${url}` })
          return true
        }
        startModal("connect", "Connect", "Enter API base URL")
        return true
      }
      case "/sessions": {
        try {
          const sessions = await client().sessions()
          if (sessions.length === 0) {
            appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "No sessions found." })
            return true
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
        return true
      }
      case "/skills": {
        try {
          const res = await client().skills()
          if (!res.skills || res.skills.length === 0) {
            appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "No skills found." })
            return true
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
        return true
      }
      case "/models": {
        try {
          const res = await client().models()
          const items = flattenModels(res)
          if (items.length === 0) {
            appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "No models available." })
            return true
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
        return true
      }
      default:
        return false
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
      const content = formatToolOutput(step, result)
      appendEntry({ id: crypto.randomUUID(), role: "tool", content })
    }
  }

  useKeyboard((evt) => {
    if (modal()) {
      if (modal() === "runs") {
        if (evt.name === "d") {
          const item = modalWindow().items[modalSelected() - modalWindow().start]
          if (item?.id) {
            void (async () => {
              let replaySeq = 0
              const lines: string[] = []
              while (true) {
                const replay = await streamRunEvents(apiUrl(), item.id, replaySeq, (event: StreamEvent) => {
                  if (event.event === "run_event") {
                    lines.push(`${event.data.seq}. ${event.data.type}`)
                  }
                })
                if (replay.lastSeq <= replaySeq) break
                replaySeq = replay.lastSeq
                if (lines.length > 200) break
              }
              setRunTimeline(lines.join("\n") || "No timeline events available.")
              setRunTimelineID(item.id)
              startModal("timeline", `Timeline ${item.id}`, "Press Enter to copy timeline, Esc to close")
            })()
          }
          return
        }
        if (evt.name === "l" && runsNextCursor()) {
          void (async () => {
            const statusFilter = runsStatusFilter() || modalQuery().trim()
            const { runs, next_cursor } = await listRuns(apiUrl(), sessionID(), statusFilter, runsNextCursor() || "")
            const items = runs.map((run) => ({
              id: run.run_id ?? crypto.randomUUID(),
              title: run.run_id ?? "unknown-run",
              subtitle: `${run.run_status ?? "unknown"} · ${String((run as Record<string, unknown>).created_at ?? (run as Record<string, unknown>).updated_at ?? "")}`,
              meta: `${String((run as Record<string, unknown>).last_step ?? "-")} ${run.reply ? `· ${run.reply.slice(0, 50)}` : ""} ${String((run as Record<string, unknown>).error ?? "")}`.trim(),
              status: run.run_status,
            }))
            setModalItems([...modalItems(), ...items])
            setRunsNextCursor(next_cursor ?? null)
            if (next_cursor) {
              const { runs: extraRuns, next_cursor: extraCursor } = await listRuns(apiUrl(), sessionID(), statusFilter, next_cursor)
              if (extraRuns.length > 0) {
                const extraItems = extraRuns.map((run) => ({
                  id: run.run_id ?? crypto.randomUUID(),
                  title: run.run_id ?? "unknown-run",
                  subtitle: `${run.run_status ?? "unknown"} · ${String((run as Record<string, unknown>).created_at ?? (run as Record<string, unknown>).updated_at ?? "")}`,
                  meta: `${String((run as Record<string, unknown>).last_step ?? "-")} ${run.reply ? `· ${run.reply.slice(0, 50)}` : ""} ${String((run as Record<string, unknown>).error ?? "")}`.trim(),
                  status: run.run_status,
                }))
                setModalItems([...modalItems(), ...items, ...extraItems])
                setRunsNextCursor(extraCursor ?? null)
              }
            }
          })()
          return
        }
        if (["1", "2", "3", "4", "5", "6"].includes(evt.name)) {
          const filters = ["", "running", "waiting_user", "failed", "timed_out", "cancelled"]
          const idx = Number(evt.name) - 1
          setRunsStatusFilter(filters[idx] ?? "")
          setModalInput(filters[idx] ?? "")
          updateModalQuery(filters[idx] ?? "")
          return
        }
      }
      if (evt.name === "escape") {
        resetModal()
        return
      }
      if (modal() === "timeline" && evt.name === "return") {
        handleModalSubmit("")
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
      if (evt.name === "tab") {
        evt.preventDefault()
        toggleAgentMode()
        return
      }
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

    if (evt.name === "escape") {
      if (busy()) {
        if (escapePressed()) {
          setEscapePressed(false)
          if (escapeTimeoutRef) clearTimeout(escapeTimeoutRef)
          escapeTimeoutRef = undefined
          abortController?.abort()
          abortController = undefined
          appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "Task cancelled." })
          return
        } else {
          setEscapePressed(true)
          escapeTimeoutRef = setTimeout(() => {
            setEscapePressed(false)
            escapeTimeoutRef = undefined
          }, 3000)
          return
        }
      }
      if (monitorActive()) {
        if (monitorInterval) {
          clearInterval(monitorInterval)
          monitorInterval = undefined
        }
        setMonitorActive(false)
        appendEntry({ id: crypto.randomUUID(), role: "assistant", content: "Monitor stopped." })
        return
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
      if (sel) {
        Selection.copy(renderer).then((success) => {
          if (success) {
            setNotification("Copied to clipboard")
            if (notificationTimeout) clearTimeout(notificationTimeout)
            notificationTimeout = setTimeout(() => setNotification(null), 1500)
          }
        })
      }
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

    metricsInterval = setInterval(async () => {
      try {
        const metrics = await client().metrics()
        setServerMetrics(metrics)
      } catch {
        // ignore
      }
    }, 3000)
    const initMetrics = async () => {
      try {
        const metrics = await client().metrics()
        setServerMetrics(metrics)
        const models = await client().models()
        if (models.current) {
          setCurrentModel(models.current.model)
        }
      } catch {
        // ignore
      }
    }
    initMetrics()
    todoPulseInterval = setInterval(() => setTodoPulse((value) => !value), 700)
    teamTaskPulseInterval = setInterval(() => setTeamTaskPulse((value) => !value), 700)

    onCleanup(() => {
      if (metricsInterval) clearInterval(metricsInterval)
      if (todoPulseInterval) clearInterval(todoPulseInterval)
      if (teamTaskPulseInterval) clearInterval(teamTaskPulseInterval)
    })
  })

  createEffect(() => {
    entries()
    if (scroll && !isTyping()) scroll.scrollBy(100000)
  })

  const width = createMemo(() => terminal().width)
  const height = createMemo(() => terminal().height)
  const contentHeight = createMemo(() => Math.max(5, height() - 6))
  const todoPanelWidth = createMemo(() => activeTodos().length > 0 ? Math.min(42, Math.max(28, Math.floor(width() * 0.28))) : 0)
  const teamTaskPanelWidth = createMemo(() => activeTeamTasks().length > 0 ? Math.min(48, Math.max(32, Math.floor(width() * 0.32))) : 0)
  const mainPanelWidth = createMemo(() => Math.max(20, width() - todoPanelWidth() - teamTaskPanelWidth()))

  return (
    <box flexDirection="column" width={width()} height={height()} backgroundColor={theme.background}>
      <StatusBar
        apiUrl={apiUrl()}
        sessionID={sessionID()}
        serverMetrics={serverMetrics()}
        agentMode={agentMode()}
      />
      <box flexDirection="row" height={contentHeight()}>
        <ChatEntries
          entries={entries()}
          isToolExpanded={isToolExpanded}
          onToggleTool={toggleToolExpanded}
          mainPanelWidth={mainPanelWidth()}
          contentHeight={contentHeight()}
          scrollRef={(val: ScrollBoxRenderable) => (scroll = val)}
        />
        {activeTodos().length > 0 && (
          <TodoList
            todos={activeTodos()}
            todoPulse={todoPulse()}
            panelWidth={todoPanelWidth()}
            contentHeight={contentHeight()}
          />
        )}
        {activeTeamTasks().length > 0 && (
          <TeamTaskPanel
            tasks={activeTeamTasks()}
            taskPulse={teamTaskPulse()}
            panelWidth={teamTaskPanelWidth()}
            contentHeight={contentHeight()}
          />
        )}
      </box>
      <Modal
        modal={modal()}
        modalTitle={modalTitle()}
        modalHint={modalHint()}
        modalQuery={modalQuery()}
        modalItems={modalItems()}
        modalSelected={modalSelected()}
        modalInput={modalInput()}
        modalInputRef={modalInputRef}
        runsStatusFilter={runsStatusFilter()}
        runTimeline={runTimeline()}
        runTimelineID={runTimelineID()}
        onQueryChange={updateModalQuery}
        onSubmit={handleModalSubmit}
        width={width()}
        height={height()}
      />
      <box flexDirection="column" paddingLeft={2} paddingRight={2} paddingBottom={1}>
        <box flexDirection="row" paddingBottom={2}>
          <text fg={agentModeColor()}>{agentModeIcon()} {agentModeLabel()}</text>
          {currentModel() && <text fg={theme.muted}> · {currentModel()}</text>}
          {activeRunBanner() && <text fg={theme.muted}> · {activeRunBanner()}</text>}
          <box flexGrow={1} />
          {notification() ? (
            <text fg={theme.success}>{notification()}</text>
          ) : (
            <text fg={theme.muted}>▣ {serverMetrics()?.resource?.cpu_percent?.toFixed(0) ?? "-"}%  ▤ {serverMetrics()?.resource?.mem_percent?.toFixed(0) ?? "-"}%</text>
          )}
        </box>
        {attachments().length > 0 && (
          <box flexDirection="row" paddingBottom={1} flexWrap="wrap">
            <For each={attachments()}>
              {(att, idx) => {
                const remove = () => {
                  const list = attachments()
                  setAttachments(list.filter((_, i) => i !== idx()))
                }
                const icon = att.kind === "image" ? "🖼️ " : att.kind === "audio" ? "🎵 " : att.kind === "video" ? "🎬 " : "📎 "
                const name = att.name || (att.path ? att.path.split("/").pop() : "file")
                return (
                  <box marginRight={1} marginBottom={1}>
                    <text fg={theme.primary}>{icon}{name}</text>
                    <text fg={theme.muted}>[x]</text>
                  </box>
                )
              }}
            </For>
          </box>
        )}
        <textarea
          ref={(val: TextareaRenderable) => (textarea = val)}
          height={3}
          focused={!modal()}
          textColor={theme.text}
          focusedTextColor={theme.text}
          cursorColor={theme.primary}
          onContentChange={() => {
            if (suppressHistoryChange) return
            setIsTyping(Boolean((textarea?.plainText ?? "").length))
            setInput(textarea?.plainText ?? "")
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
            setIsTyping(false)
            const value = textarea?.plainText ?? input()
            submit(value)
          }}
          onPaste={(evt: PasteEvent) => {
            const text = evt.text
            if (!text) return
            
            const paths = parseAttachmentPaths(text)
            if (paths.length > 0) {
              evt.preventDefault()
              const current = attachments()
              setAttachments([...current, ...paths])
              return
            }
            
            const pathMatch = text.match(/^@(.+)$/m)
            if (pathMatch) {
              const filePath = pathMatch[1].trim()
              if (filePath.startsWith("/") || filePath.startsWith("~") || filePath.includes("/") || filePath.includes("\\")) {
                evt.preventDefault()
                const expanded = filePath.replace(/^~/, process.env.HOME || "")
                const current = attachments()
                setAttachments([...current, { path: expanded }])
                return
              } else {
                evt.preventDefault()
                const current = attachments()
                setAttachments([...current, { path: filePath }])
                return
              }
            }
          }}
          placeholder={
            escapePressed()
              ? "Press ESC again to cancel task"
              : modal()
                  ? modalHint()
                  : busy()
                    ? "Task running. You can keep typing; new requests will be queued..."
                    : "Ask anything..."
          }
        />
        <box flexDirection="row" paddingTop={1}>
          <text fg={theme.muted}>Tab toggle mode · Ctrl+N new session · Ctrl+C exit · Ctrl+Y copy selection</text>
        </box>
      </box>
    </box>
  )
}
