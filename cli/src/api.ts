export type PlanStep = {
  id: string
  description: string
  tool: string
  inputs: Record<string, unknown>
  status: string
}

export type ToolResult = {
  step_id?: string
  success: boolean
  data?: Record<string, unknown>
  error?: string
}

export type ApiResponse = {
  run_id?: string
  run_status?: string
  plan: {
    steps: PlanStep[]
  }
  results: ToolResult[]
  reply: string
  confirmation?: ConfirmationPayload
  todos?: TodoItem[]
}

export type TodoItem = {
  id: string
  content: string
  status: string
  priority?: string
  active?: boolean
  tool?: string
  note?: string
}

export type ConfirmationDecision = {
  reason?: string
  rule_name?: string
  risk_level?: string
  risk_score?: number
  alternatives?: string[]
  suggestions?: string[]
}

export type ConfirmationPayload = {
  tool: string
  inputs?: Record<string, unknown>
  decision?: ConfirmationDecision
}

export type ReplRequest = {
  session: string
  input: string
  mode?: string
  attachments?: AttachmentInput[]
}

export type PlanRequest = {
  session: string
  input: string
  attachments?: AttachmentInput[]
}

export type PlanResponse = {
  summary: string
  steps: PlanStep[]
}

export type AttachmentInput = {
  path?: string
  url?: string
  name?: string
  kind?: string
  mime?: string
}

export type SessionInfo = {
  id: string
  created_at: string
}

export type SessionDump = {
  id: string
  conversation: string
}

export type SkillMetadata = {
  name: string
  description: string
  capabilities?: string[]
}

export type SkillListResponse = {
  skills: SkillMetadata[]
  active?: string
}

export type ModelProviderInfo = {
  name: string
  models: string[]
  has_token: boolean
}

export type ModelsResponse = {
  providers: ModelProviderInfo[]
  current?: { provider: string; model: string }
}

export type ModelSelectRequest = {
  provider: string
  model: string
  api_key?: string
  endpoint?: string
}

export type MetricsResponse = {
  uptime_seconds: number
  active_requests: number
  processed_requests: number
  error_requests: number
  input_tokens: number
  output_tokens: number
  latency_ms?: { p50: number; p95: number; p99: number }
  memory?: {
    heap_alloc_mb?: number
    heap_sys_mb?: number
    sys_mb?: number
    num_gc?: number
  }
  runtime?: {
    goroutines?: number
  }
  resource?: {
    cpu_percent?: number
    mem_percent?: number
    mem_used_mb?: number
    mem_total_mb?: number
  }
}

export type RemoteFileResponse = {
  path: string
  content: string
  hash: string
  size: number
}

export type SSHInfoResponse = {
  host: string
  user: string
  workspace: string
}

export function createClient(baseUrl: string) {
  const base = baseUrl.replace(/\/$/, "")
  return {
    async shell(command: string, workdir?: string): Promise<{ stdout: string; stderr: string; exit_code: number; success: boolean; error: string }> {
      let resp: Response
      try {
        resp = await fetch(`${base}/shell`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ command, workdir }),
        })
      } catch (err) {
        throw new Error(`API request failed (/shell): ${err instanceof Error ? err.message : String(err)}`)
      }
      const body = await safeReadBody(resp)
      if (!resp.ok) {
        throw new Error(formatAPIError(resp, body))
      }
      return JSON.parse(body)
    },
    async repl(req: ReplRequest): Promise<ApiResponse> {
      let resp: Response
      try {
        resp = await fetch(`${base}/repl`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(req),
        })
      } catch (err) {
        throw new Error(`API request failed (/repl): ${err instanceof Error ? err.message : String(err)}`)
      }
      const body = await safeReadBody(resp)
      if (!resp.ok) {
        throw new Error(formatAPIError(resp, body))
      }
      const raw = JSON.parse(body) as Record<string, unknown>
      return normalizeResponse(raw)
    },
    async sessions(query?: string): Promise<SessionInfo[]> {
      const url = new URL(`${base}/session/`)
      if (query) url.searchParams.set("q", query)
      const data = await fetchJSON(url.toString())
      return ((data as Record<string, unknown>).sessions ?? []) as SessionInfo[]
    },
    async loadSession(id: string): Promise<void> {
      await fetchJSON(`${base}/session/${encodeURIComponent(id)}/load`, {
        method: "POST",
      })
    },
    async getSession(id: string): Promise<SessionDump> {
      const data = await fetchJSON(`${base}/session/${encodeURIComponent(id)}`)
      const raw = data as Record<string, unknown>
      return {
        id: String(raw.id ?? id),
        conversation: String(raw.conversation ?? ""),
      }
    },
    async skills(query?: string): Promise<SkillListResponse> {
      const url = new URL(`${base}/skill`)
      if (query) url.searchParams.set("q", query)
      const data = await fetchJSON(url.toString())
      return data as SkillListResponse
    },
    async loadSkill(name: string): Promise<SkillMetadata> {
      const data = await fetchJSON(`${base}/skill/${encodeURIComponent(name)}/load`, {
        method: "POST",
      })
      return data as SkillMetadata
    },
    async models(): Promise<ModelsResponse> {
      const data = await fetchJSON(`${base}/models/`)
      return data as ModelsResponse
    },
    async selectModel(req: ModelSelectRequest): Promise<void> {
      await fetchJSON(`${base}/models/select`, {
        method: "POST",
        body: JSON.stringify(req),
      })
    },
    async metrics(): Promise<MetricsResponse> {
      const data = await fetchJSON(`${base}/metrics`)
      return data as MetricsResponse
    },
    async getRemoteFile(path: string): Promise<RemoteFileResponse> {
      const url = new URL(`${base}/vim`)
      url.searchParams.set("path", path)
      const data = await fetchJSON(url.toString())
      return data as RemoteFileResponse
    },
    async putRemoteFile(path: string, content: string, expectedHash: string): Promise<RemoteFileResponse> {
      const data = await fetchJSON(`${base}/vim`, {
        method: "POST",
        body: JSON.stringify({ path, content, expected_hash: expectedHash }),
      })
      return data as RemoteFileResponse
    },
    async sshInfo(): Promise<SSHInfoResponse> {
      const data = await fetchJSON(`${base}/ssh`)
      return data as SSHInfoResponse
    },
    async plan(req: PlanRequest): Promise<PlanResponse> {
      let resp: Response
      try {
        resp = await fetch(`${base}/plan`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(req),
        })
      } catch (err) {
        throw new Error(`API request failed (/plan): ${err instanceof Error ? err.message : String(err)}`)
      }
      const body = await safeReadBody(resp)
      if (!resp.ok) {
        throw new Error(formatAPIError(resp, body))
      }
      return JSON.parse(body) as PlanResponse
    },
  }
}

export type TeamTaskStatus = "queued" | "running" | "completed" | "failed" | "cancelled"

export type TeamTaskEvent = {
  id: string
  role: string
  prompt: string
  status: TeamTaskStatus
  summary?: string
  error?: string
}

export type StreamEvent =
  | { event: "run_event"; data: { run_id: string; seq: number; type: string; data?: Record<string, unknown> } }
  | { event: "assistant_delta"; data: { text: string } }
  | { event: "tool_pending"; data: { tool: string; input?: Record<string, unknown>; call_id?: string } }
  | { event: "tool_result"; data: { call_id?: string; step: PlanStep; result: ToolResult } }
  | { event: "confirmation"; data: ConfirmationPayload }
  | { event: "error"; data: { error: string } }
  | { event: "done"; data: ApiResponse }
  | { event: "team_plan"; data: { summary: string; tasks: TeamTaskEvent[] } }
  | { event: "team_task_started"; data: TeamTaskEvent }
  | { event: "team_task_finished"; data: TeamTaskEvent }
  | { event: "team_task_error"; data: TeamTaskEvent }

export async function streamRunEvents(
  baseUrl: string,
  runID: string,
  afterSeq: number,
  onEvent: (evt: StreamEvent) => void,
  signal?: AbortSignal,
  limit = 200,
): Promise<{ lastSeq: number }> {
  const base = baseUrl.replace(/\/$/, "")
  const controller = signal ? null : new AbortController()
  const requestSignal = signal ?? controller?.signal
  const timeout = controller ? setTimeout(() => controller.abort(), 15000) : null
  const resp = await fetch(`${base}/runs/${runID}/events?after_seq=${afterSeq}&limit=${limit}`, {
    method: "GET",
    headers: { Accept: "text/event-stream" },
    signal: requestSignal,
  })
  if (timeout) clearTimeout(timeout)
  if (!resp.ok || !resp.body) {
    throw new Error(`Failed to stream run events for ${runID}`)
  }
  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""
  let lastSeq = afterSeq
  while (true) {
    const { value, done } = await reader.read()
    if (done) return { lastSeq }
    buffer += decoder.decode(value, { stream: true })
    while (true) {
      const idx = buffer.indexOf("\n\n")
      if (idx < 0) break
      const frame = buffer.slice(0, idx)
      buffer = buffer.slice(idx + 2)
      for (const line of frame.split("\n")) {
        const trimmed = line.trim()
        if (!trimmed.startsWith("data:")) continue
        const payload = trimmed.slice(5).trim()
        if (!payload) continue
        try {
          const evt = JSON.parse(payload) as StreamEvent
          if (evt.event === "run_event") {
            lastSeq = Math.max(lastSeq, evt.data.seq ?? lastSeq)
          }
          onEvent(evt)
        } catch {
          // ignore malformed frames
        }
      }
    }
  }
}

export async function getRun(baseUrl: string, runID: string): Promise<ApiResponse> {
  const base = baseUrl.replace(/\/$/, "")
  return fetchJSON(`${base}/runs/${runID}`) as Promise<ApiResponse>
}

export async function createRun(baseUrl: string, req: ReplRequest): Promise<{ run_id: string; run_status: string }> {
  const base = baseUrl.replace(/\/$/, "")
  const result = await fetchJSON(`${base}/runs`, {
    method: "POST",
    body: JSON.stringify(req),
  }) as Promise<{ run_id: string; run_status: string }>
  return result
}

export async function listRuns(baseUrl: string, sessionID: string, status = "", cursor = ""): Promise<{ runs: ApiResponse[]; next_cursor?: string }> {
  const base = baseUrl.replace(/\/$/, "")
  const suffix = status ? `&status=${encodeURIComponent(status)}` : ""
  const cursorPart = cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""
  const data = await fetchJSON(`${base}/runs?session=${encodeURIComponent(sessionID)}&limit=20${suffix}${cursorPart}`) as { runs?: ApiResponse[]; next_cursor?: string }
  return { runs: data.runs ?? [], next_cursor: data.next_cursor }
}

export async function cancelRun(baseUrl: string, runID: string): Promise<void> {
  const base = baseUrl.replace(/\/$/, "")
  await fetchJSON(`${base}/runs/${runID}?action=cancel`, { method: "POST" })
}

export async function replStream(
  baseUrl: string,
  req: ReplRequest,
  onEvent: (evt: StreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const base = baseUrl.replace(/\/$/, "")
  let resp: Response
  try {
    resp = await fetch(`${base}/repl/stream`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
      },
      body: JSON.stringify(req),
      signal,
    })
  } catch (err) {
    throw new Error(`API request failed (/repl/stream): ${err instanceof Error ? err.message : String(err)}`)
  }
  const body = resp.body
  if (!resp.ok) {
    const text = await safeReadBody(resp)
    throw new Error(formatAPIError(resp, text))
  }
  if (!body) {
    throw new Error("API error: empty stream body")
  }

  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""
  let sawEvent = false
  let lastActivity = Date.now()
  let timeoutId: ReturnType<typeof setTimeout> | null = null

  const shouldStop = (event: string) => event === "done" || event === "error"

  const clearTimeoutId = () => {
    if (timeoutId !== null) {
      clearTimeout(timeoutId)
      timeoutId = null
    }
  }

  const cancelReader = async () => {
    try {
      await reader.cancel()
    } catch {
      // ignore
    }
  }

  const cleanup = async () => {
    clearTimeoutId()
    await cancelReader()
  }

  while (true) {
    let timedOut = false
    timeoutId = setTimeout(() => {
      if (Date.now() - lastActivity >= 15000) {
        timedOut = true
      }
    }, 5000)

    let readResult: ReadableStreamReadResult<Uint8Array>
    try {
      readResult = await Promise.race([
        reader.read(),
        new Promise<ReadableStreamReadResult<Uint8Array>>((_, reject) => {
          const timer = setTimeout(() => {
            if (Date.now() - lastActivity >= 15000) {
              reject(new Error("Stream timeout waiting for response completion"))
            }
          }, 15000)
          signal?.addEventListener("abort", () => clearTimeout(timer), { once: true })
        }),
      ])
    } catch (err) {
      clearTimeoutId()
      await cleanup()
      throw err
    }

    clearTimeoutId()

    if (timedOut) {
      await cleanup()
      throw new Error("Stream timeout waiting for response completion")
    }

    const { value, done } = readResult
    if (done) break
    lastActivity = Date.now()
    buffer += decoder.decode(value, { stream: true })

    while (true) {
      const idx = buffer.indexOf("\n\n")
      if (idx < 0) break
      const frame = buffer.slice(0, idx)
      buffer = buffer.slice(idx + 2)

      const lines = frame.split("\n")
      for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed.startsWith("data:")) continue
        const payload = trimmed.slice("data:".length).trim()
        if (!payload) continue
        try {
          const raw = JSON.parse(payload) as any
          const event = String(raw?.event ?? "")
          sawEvent = true
          lastActivity = Date.now()
          if (event === "tool_result") {
            const step = normalizeStep(raw?.data?.step ?? {})
            const result = normalizeToolResult(raw?.data?.result ?? {})
            const callID = raw?.data?.call_id
            onEvent({ event: "tool_result", data: { call_id: typeof callID === "string" ? callID : undefined, step, result } })
            continue
          }
          if (event === "confirmation") {
            const confirmation = normalizeConfirmation(raw?.data ?? {})
            onEvent({ event: "confirmation", data: confirmation })
            continue
          }
          if (event === "done") {
            onEvent({ event: "done", data: normalizeResponse(raw?.data ?? {}) })
            await cleanup()
            return
          }

          onEvent(raw as StreamEvent)

          if (shouldStop(event)) {
            await cleanup()
            return
          }
        } catch {
          // ignore malformed frames
        }
      }
    }
  }

  await cleanup()
  if (sawEvent) return
  throw new Error("Stream ended without terminal event")
}

function normalizeStep(raw: Record<string, unknown>): PlanStep {
  const inputs = (raw.inputs ?? raw.Inputs ?? {}) as Record<string, unknown>
  if (raw.content !== undefined && !inputs.content) {
    inputs.content = raw.content
  }
  return {
    id: String(raw.id ?? raw.ID ?? ""),
    description: String(raw.description ?? raw.Description ?? ""),
    tool: String(raw.tool ?? raw.Tool ?? ""),
    inputs,
    status: String(raw.status ?? raw.Status ?? ""),
  }
}

function normalizeToolResult(raw: Record<string, unknown>): ToolResult {
  return {
    step_id: (raw.step_id ?? raw.StepID ?? raw.stepID ?? raw.StepId) as string | undefined,
    success: Boolean(raw.success ?? raw.Success),
    data: (raw.data ?? raw.Data) as Record<string, unknown> | undefined,
    error: (raw.error ?? raw.Error) as string | undefined,
  }
}

function normalizeConfirmation(raw: Record<string, unknown>): ConfirmationPayload {
  return {
    tool: String(raw.tool ?? raw.Tool ?? ""),
    inputs: (raw.inputs ?? raw.Inputs ?? {}) as Record<string, unknown>,
    decision: (raw.decision ?? raw.Decision ?? undefined) as ConfirmationDecision | undefined,
  }
}

async function fetchJSON(url: string, init?: RequestInit): Promise<unknown> {
  let resp: Response
  try {
    resp = await fetch(url, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        ...(init?.headers ?? {}),
      },
    })
  } catch (err) {
    throw new Error(`API request failed (${url}): ${err instanceof Error ? err.message : String(err)}`)
  }
  const body = await safeReadBody(resp)
  if (!resp.ok) {
    throw new Error(formatAPIError(resp, body))
  }
  return body ? JSON.parse(body) : {}
}

async function safeReadBody(resp: Response): Promise<string> {
  try {
    return await resp.text()
  } catch {
    return ""
  }
}

function formatAPIError(resp: Response, body: string): string {
  const trimmed = body.trim()
  const details = trimmed || resp.statusText || "No response body"
  const path = (() => {
    try {
      return new URL(resp.url).pathname
    } catch {
      return resp.url || "(unknown url)"
    }
  })()
  return `API error (${resp.status}) ${path}: ${details}`
}

function normalizeResponse(raw: Record<string, unknown>): ApiResponse {
  const plan = (raw.plan ?? raw.Plan ?? null) as Record<string, unknown> | null
  const rawResults = (raw.results ?? raw.Results ?? []) as Array<Record<string, unknown>>
  const reply = (raw.reply ?? raw.Reply ?? "") as string
  const rawConfirmation = (raw.confirmation ?? raw.Confirmation ?? null) as Record<string, unknown> | null

  const steps =
    (plan?.steps ?? plan?.Steps ?? []) as Array<Record<string, unknown>>

  const normalizedPlan = {
    steps: steps.map((step) => ({
      id: String(step.id ?? step.ID ?? ""),
      description: String(step.description ?? step.Description ?? ""),
      tool: String(step.tool ?? step.Tool ?? ""),
      inputs: (step.inputs ?? step.Inputs ?? {}) as Record<string, unknown>,
      status: String(step.status ?? step.Status ?? ""),
    })),
  }

  const normalizedResults = rawResults.map((result) => ({
    step_id: (result.step_id ?? result.StepID) as string | undefined,
    success: Boolean(result.success ?? result.Success),
    data: (result.data ?? result.Data) as Record<string, unknown> | undefined,
    error: (result.error ?? result.Error) as string | undefined,
  }))

  const normalizedConfirmation = rawConfirmation ? normalizeConfirmation(rawConfirmation) : undefined

  return {
    plan: normalizedPlan,
    results: normalizedResults,
    reply,
    confirmation: normalizedConfirmation,
  }
}
