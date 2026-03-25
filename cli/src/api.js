export function createClient(baseUrl) {
    const base = baseUrl.replace(/\/$/, "");
    return {
        async repl(req) {
            let resp;
            try {
                resp = await fetch(`${base}/repl`, {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify(req),
                });
            }
            catch (err) {
                throw new Error(`API request failed (/repl): ${err instanceof Error ? err.message : String(err)}`);
            }
            const body = await safeReadBody(resp);
            if (!resp.ok) {
                throw new Error(formatAPIError(resp, body));
            }
            const raw = JSON.parse(body);
            return normalizeResponse(raw);
        },
        async sessions(query) {
            const url = new URL(`${base}/api/v1/sessions`);
            if (query)
                url.searchParams.set("q", query);
            const data = await fetchJSON(url.toString());
            return (data.sessions ?? []);
        },
        async loadSession(id) {
            await fetchJSON(`${base}/api/v1/sessions/${encodeURIComponent(id)}/load`, {
                method: "POST",
            });
        },
        async getSession(id) {
            const data = await fetchJSON(`${base}/api/v1/sessions/${encodeURIComponent(id)}`);
            const raw = data;
            return {
                id: String(raw.id ?? id),
                conversation: String(raw.conversation ?? ""),
            };
        },
        async skills(query) {
            const url = new URL(`${base}/api/v1/skills`);
            if (query)
                url.searchParams.set("q", query);
            const data = await fetchJSON(url.toString());
            return data;
        },
        async loadSkill(name) {
            const data = await fetchJSON(`${base}/api/v1/skills/${encodeURIComponent(name)}/load`, {
                method: "POST",
            });
            return data;
        },
        async models() {
            const data = await fetchJSON(`${base}/api/v1/models`);
            return data;
        },
        async selectModel(req) {
            await fetchJSON(`${base}/api/v1/models/select`, {
                method: "POST",
                body: JSON.stringify(req),
            });
        },
    };
}
export async function replStream(baseUrl, req, onEvent) {
    const base = baseUrl.replace(/\/$/, "");
    let resp;
    try {
        resp = await fetch(`${base}/repl/stream`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                Accept: "text/event-stream",
            },
            body: JSON.stringify(req),
        });
    }
    catch (err) {
        throw new Error(`API request failed (/repl/stream): ${err instanceof Error ? err.message : String(err)}`);
    }
    const body = resp.body;
    if (!resp.ok) {
        const text = await safeReadBody(resp);
        throw new Error(formatAPIError(resp, text));
    }
    if (!body) {
        throw new Error("API error: empty stream body");
    }
    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    const shouldStop = (event) => event === "done" || event === "error";
    while (true) {
        const { value, done } = await reader.read();
        if (done)
            break;
        buffer += decoder.decode(value, { stream: true });
        while (true) {
            const idx = buffer.indexOf("\n\n");
            if (idx < 0)
                break;
            const frame = buffer.slice(0, idx);
            buffer = buffer.slice(idx + 2);
            const lines = frame.split("\n");
            for (const line of lines) {
                const trimmed = line.trim();
                if (!trimmed.startsWith("data:"))
                    continue;
                const payload = trimmed.slice("data:".length).trim();
                if (!payload)
                    continue;
                try {
                    const raw = JSON.parse(payload);
                    const event = String(raw?.event ?? "");
                    if (event === "tool_result") {
                        const step = normalizeStep(raw?.data?.step ?? {});
                        const result = normalizeToolResult(raw?.data?.result ?? {});
                        const callID = raw?.data?.call_id;
                        onEvent({ event: "tool_result", data: { call_id: typeof callID === "string" ? callID : undefined, step, result } });
                        continue;
                    }
                    if (event === "done") {
                        onEvent({ event: "done", data: normalizeResponse(raw?.data ?? {}) });
                        // The server should close the stream, but don't rely on it.
                        try {
                            await reader.cancel();
                        }
                        catch {
                            // ignore
                        }
                        return;
                    }
                    onEvent(raw);
                    if (shouldStop(event)) {
                        try {
                            await reader.cancel();
                        }
                        catch {
                            // ignore
                        }
                        return;
                    }
                }
                catch {
                    // ignore malformed frames
                }
            }
        }
    }
}
function normalizeStep(raw) {
    return {
        id: String(raw.id ?? raw.ID ?? ""),
        description: String(raw.description ?? raw.Description ?? ""),
        tool: String(raw.tool ?? raw.Tool ?? ""),
        inputs: (raw.inputs ?? raw.Inputs ?? {}),
        status: String(raw.status ?? raw.Status ?? ""),
    };
}
function normalizeToolResult(raw) {
    return {
        step_id: (raw.step_id ?? raw.StepID ?? raw.stepID ?? raw.StepId),
        success: Boolean(raw.success ?? raw.Success),
        data: (raw.data ?? raw.Data),
        error: (raw.error ?? raw.Error),
    };
}
async function fetchJSON(url, init) {
    let resp;
    try {
        resp = await fetch(url, {
            ...init,
            headers: {
                "Content-Type": "application/json",
                ...(init?.headers ?? {}),
            },
        });
    }
    catch (err) {
        throw new Error(`API request failed (${url}): ${err instanceof Error ? err.message : String(err)}`);
    }
    const body = await safeReadBody(resp);
    if (!resp.ok) {
        throw new Error(formatAPIError(resp, body));
    }
    return body ? JSON.parse(body) : {};
}
async function safeReadBody(resp) {
    try {
        return await resp.text();
    }
    catch {
        return "";
    }
}
function formatAPIError(resp, body) {
    const trimmed = body.trim();
    const details = trimmed || resp.statusText || "No response body";
    const path = (() => {
        try {
            return new URL(resp.url).pathname;
        }
        catch {
            return resp.url || "(unknown url)";
        }
    })();
    return `API error (${resp.status}) ${path}: ${details}`;
}
function normalizeResponse(raw) {
    const plan = (raw.plan ?? raw.Plan ?? null);
    const rawResults = (raw.results ?? raw.Results ?? []);
    const reply = (raw.reply ?? raw.Reply ?? "");
    const steps = (plan?.steps ?? plan?.Steps ?? []);
    const normalizedPlan = {
        steps: steps.map((step) => ({
            id: String(step.id ?? step.ID ?? ""),
            description: String(step.description ?? step.Description ?? ""),
            tool: String(step.tool ?? step.Tool ?? ""),
            inputs: (step.inputs ?? step.Inputs ?? {}),
            status: String(step.status ?? step.Status ?? ""),
        })),
    };
    const normalizedResults = rawResults.map((result) => ({
        step_id: (result.step_id ?? result.StepID),
        success: Boolean(result.success ?? result.Success),
        data: (result.data ?? result.Data),
        error: (result.error ?? result.Error),
    }));
    return {
        plan: normalizedPlan,
        results: normalizedResults,
        reply,
    };
}
