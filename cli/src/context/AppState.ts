import {
  createClient,
  type ConfirmationPayload,
  type MetricsResponse,
  type ModelsResponse,
  type TodoItem,
} from "../api"
import type { Entry } from "../types"

export type AppProps = {
  apiUrl: string
  sessionID: string
  initialPrompt?: string
  debugStream?: boolean
}

export type ModalKind = "sessions" | "runs" | "timeline" | "skills" | "models" | "connect" | "modelToken" | "help" | "confirm"

export type ModalItem = {
  id: string
  title: string
  subtitle?: string
  meta?: string
  status?: string
  provider?: string
  model?: string
  hasToken?: boolean
}

export type AttachmentInput = {
  path?: string
  url?: string
  name?: string
  kind?: string
  mime?: string
}

export interface AppState {
  apiUrl: string
  setApiUrl: (url: string) => void
  client: ReturnType<typeof createClient>
  entries: Entry[]
  setEntries: (entries: Entry[] | ((prev: Entry[]) => Entry[])) => void
  input: string
  setInput: (input: string) => void
  busy: boolean
  setBusy: (busy: boolean) => void
  sessionID: string
  setSessionID: (id: string) => void
  agentMode: "build" | "plan"
  setAgentMode: (mode: "build" | "plan") => void
  inputHistory: string[]
  setInputHistory: (history: string[] | ((prev: string[]) => string[])) => void
  historyIndex: number | null
  setHistoryIndex: (idx: number | null) => void
  historyDraft: string
  setHistoryDraft: (draft: string) => void
  modal: ModalKind | null
  setModal: (modal: ModalKind | null) => void
  modalTitle: string
  setModalTitle: (title: string) => void
  modalHint: string
  setModalHint: (hint: string) => void
  modalQuery: string
  setModalQuery: (query: string) => void
  modalItems: ModalItem[]
  setModalItems: (items: ModalItem[] | ((prev: ModalItem[]) => ModalItem[])) => void
  runsNextCursor: string | null
  setRunsNextCursor: (cursor: string | null) => void
  runsStatusFilter: string
  setRunsStatusFilter: (filter: string) => void
  activeRunBanner: string | null
  setActiveRunBanner: (banner: string | null) => void
  activeTodos: TodoItem[]
  setActiveTodos: (todos: TodoItem[] | ((prev: TodoItem[]) => TodoItem[])) => void
  activeStreamToken: string | null
  setActiveStreamToken: (token: string | null) => void
  runTimeline: string | null
  setRunTimeline: (timeline: string | null) => void
  runTimelineID: string | null
  setRunTimelineID: (id: string | null) => void
  modalError: string
  setModalError: (error: string) => void
  notification: string | null
  setNotification: (notification: string | null) => void
  pendingModel: { provider: string; model: string } | null
  setPendingModel: (model: { provider: string; model: string } | null) => void
  modalSelected: number
  setModalSelected: (selected: number) => void
  modalInput: string
  setModalInput: (input: string) => void
  confirmPayload: ConfirmationPayload | null
  setConfirmPayload: (payload: ConfirmationPayload | null) => void
  lastConfirmID: string | null
  setLastConfirmID: (id: string | null) => void
  pendingConfirmation: ConfirmationPayload | null
  setPendingConfirmation: (confirmation: ConfirmationPayload | null) => void
  expandedToolEntries: Set<string>
  setExpandedToolEntries: (entries: Set<string> | ((prev: Set<string>) => Set<string>)) => void
  queuedRequests: { id: string; text: string }[]
  setQueuedRequests: (requests: { id: string; text: string }[] | ((prev: { id: string; text: string }[]) => { id: string; text: string }[])) => void
  monitorActive: boolean
  setMonitorActive: (active: boolean) => void
  attachments: AttachmentInput[]
  setAttachments: (attachments: AttachmentInput[] | ((prev: AttachmentInput[]) => AttachmentInput[])) => void
  escapePressed: boolean
  setEscapePressed: (pressed: boolean) => void
  isTyping: boolean
  setIsTyping: (typing: boolean) => void
  serverMetrics: MetricsResponse | null
  setServerMetrics: (metrics: MetricsResponse | null) => void
  currentModel: string
  setCurrentModel: (model: string) => void
  todoPulse: boolean
  setTodoPulse: (pulse: boolean) => void
  monitorInterval?: ReturnType<typeof setInterval>
  setMonitorInterval: (interval?: ReturnType<typeof setInterval>) => void
  metricsInterval?: ReturnType<typeof setInterval>
  setMetricsInterval: (interval?: ReturnType<typeof setInterval>) => void
  todoPulseInterval?: ReturnType<typeof setInterval>
  setTodoPulseInterval: (interval?: ReturnType<typeof setInterval>) => void
  abortController?: AbortController
  setAbortController: (controller?: AbortController) => void
  escapeTimeoutRef?: ReturnType<typeof setTimeout>
  setEscapeTimeoutRef: (ref?: ReturnType<typeof setTimeout>) => void
  inputQueue: { text: string; entryId: string }[]
  isProcessingQueue: boolean
  setIsProcessingQueue: (processing: boolean) => void
  modalInputRef?: import("@opentui/core").InputRenderable
  setModalInputRef: (ref?: import("@opentui/core").InputRenderable) => void
  modalSearchTimer?: ReturnType<typeof setTimeout>
  setModalSearchTimer: (timer?: ReturnType<typeof setTimeout>) => void
  scroll?: import("@opentui/core").ScrollBoxRenderable
  setScroll: (scroll?: import("@opentui/core").ScrollBoxRenderable) => void
  textarea?: import("@opentui/core").TextareaRenderable
  setTextarea: (textarea?: import("@opentui/core").TextareaRenderable) => void
  suppressHistoryChange: boolean
  setSuppressHistoryChange: (suppress: boolean) => void
  toolEntryByCallID: Map<string, string>
  notificationTimeout?: ReturnType<typeof setTimeout>
  setNotificationTimeout: (timeout?: ReturnType<typeof setTimeout>) => void
}
