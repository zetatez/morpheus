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
  plan: {
    steps: PlanStep[]
  }
  results: ToolResult[]
  reply: string
}

export type ReplRequest = {
  session: string
  input: string
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

export function createClient(baseUrl: string) {
  const base = baseUrl.replace(/\/$/, "")
  return {
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
      const url = new URL(`${base}/api/v1/sessions`)
      if (query) url.searchParams.set("q", query)
      const data = await fetchJSON(url.toString())
      return ((data as Record<string, unknown>).sessions ?? []) as SessionInfo[]
    },
    async loadSession(id: string): Promise<void> {
      await fetchJSON(`${base}/api/v1/sessions/${encodeURIComponent(id)}/load`, {
        method: "POST",
      })
    },
    async getSession(id: string): Promise<SessionDump> {
      const data = await fetchJSON(`${base}/api/v1/sessions/${encodeURIComponent(id)}`)
      const raw = data as Record<string, unknown>
      return {
        id: String(raw.id ?? id),
        conversation: String(raw.conversation ?? ""),
      }
    },
    async skills(query?: string): Promise<SkillListResponse> {
      const url = new URL(`${base}/api/v1/skills`)
      if (query) url.searchParams.set("q", query)
      const data = await fetchJSON(url.toString())
      return data as SkillListResponse
    },
    async loadSkill(name: string): Promise<SkillMetadata> {
      const data = await fetchJSON(`${base}/api/v1/skills/${encodeURIComponent(name)}/load`, {
        method: "POST",
      })
      return data as SkillMetadata
    },
    async models(): Promise<ModelsResponse> {
      const data = await fetchJSON(`${base}/api/v1/models`)
      return data as ModelsResponse
    },
    async selectModel(req: ModelSelectRequest): Promise<void> {
      await fetchJSON(`${base}/api/v1/models/select`, {
        method: "POST",
        body: JSON.stringify(req),
      })
    },
  }
}

export type StreamEvent =
  | { event: "assistant_delta"; data: { text: string } }
  | { event: "tool_pending"; data: { tool: string; input?: Record<string, unknown>; call_id?: string } }
  | { event: "tool_result"; data: { call_id?: string; step: PlanStep; result: ToolResult } }
  | { event: "error"; data: { error: string } }
  | { event: "done"; data: ApiResponse }

export async function replStream(
  baseUrl: string,
  req: ReplRequest,
  onEvent: (evt: StreamEvent) => void,
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

  const shouldStop = (event: string) => event === "done" || event === "error"

  while (true) {
    const { value, done } = await reader.read()
    if (done) break
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
          if (event === "tool_result") {
            const step = normalizeStep(raw?.data?.step ?? {})
            const result = normalizeToolResult(raw?.data?.result ?? {})
            const callID = raw?.data?.call_id
            onEvent({ event: "tool_result", data: { call_id: typeof callID === "string" ? callID : undefined, step, result } })
            continue
          }
          if (event === "done") {
            onEvent({ event: "done", data: normalizeResponse(raw?.data ?? {}) })

			// The server should close the stream, but don't rely on it.
			try {
				await reader.cancel()
			} catch {
				// ignore
			}
			return
          }

          onEvent(raw as StreamEvent)

          if (shouldStop(event)) {
            try {
              await reader.cancel()
            } catch {
              // ignore
            }
            return
          }
        } catch {
          // ignore malformed frames
        }
      }
    }
  }
}

function normalizeStep(raw: Record<string, unknown>): PlanStep {
  return {
    id: String(raw.id ?? raw.ID ?? ""),
    description: String(raw.description ?? raw.Description ?? ""),
    tool: String(raw.tool ?? raw.Tool ?? ""),
    inputs: (raw.inputs ?? raw.Inputs ?? {}) as Record<string, unknown>,
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

  return {
    plan: normalizedPlan,
    results: normalizedResults,
    reply,
  }
}
