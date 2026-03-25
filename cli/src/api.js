export function createClient(baseUrl) {
    const base = baseUrl.replace(/\/$/, "");
    return {
        async shell(command, workdir) {
            let resp;
            try {
                resp = await fetch(`${base}/shell`, {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ command, workdir }),
                });
            }
            catch (err) {
                throw new Error(`API request failed (/shell): ${err instanceof Error ? err.message : String(err)}`);
            }
            const body = await safeReadBody(resp);
            if (!resp.ok) {
                throw new Error(formatAPIError(resp, body));
            }
            return JSON.parse(body);
        },
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
            const url = new URL(`${base}/session/`);
            if (query)
                url.searchParams.set("q", query);
            const data = await fetchJSON(url.toString());
            return (data.sessions ?? []);
        },
        async loadSession(id) {
            await fetchJSON(`${base}/session/${encodeURIComponent(id)}/load`, {
                method: "POST",
            });
        },
        async getSession(id) {
            const data = await fetchJSON(`${base}/session/${encodeURIComponent(id)}`);
            const raw = data;
            return {
                id: String(raw.id ?? id),
                conversation: String(raw.conversation ?? ""),
            };
        },
        async skills(query) {
            const url = new URL(`${base}/skill`);
            if (query)
                url.searchParams.set("q", query);
            const data = await fetchJSON(url.toString());
            return data;
        },
        async loadSkill(name) {
            const data = await fetchJSON(`${base}/skill/${encodeURIComponent(name)}/load`, {
                method: "POST",
            });
            return data;
        },
        async models() {
            const data = await fetchJSON(`${base}/models/`);
            return data;
        },
        async selectModel(req) {
            await fetchJSON(`${base}/models/select`, {
                method: "POST",
                body: JSON.stringify(req),
            });
        },
        async metrics() {
            const data = await fetchJSON(`${base}/metrics`);
            return data;
        },
        async getRemoteFile(path) {
            const url = new URL(`${base}/vim`);
            url.searchParams.set("path", path);
            const data = await fetchJSON(url.toString());
            return data;
        },
        async putRemoteFile(path, content, expectedHash) {
            const data = await fetchJSON(`${base}/vim`, {
                method: "POST",
                body: JSON.stringify({ path, content, expected_hash: expectedHash }),
            });
            return data;
        },
        async sshInfo() {
            const data = await fetchJSON(`${base}/ssh`);
            return data;
        },
        async plan(req) {
            let resp;
            try {
                resp = await fetch(`${base}/plan`, {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify(req),
                });
            }
            catch (err) {
                throw new Error(`API request failed (/plan): ${err instanceof Error ? err.message : String(err)}`);
            }
            const body = await safeReadBody(resp);
            if (!resp.ok) {
                throw new Error(formatAPIError(resp, body));
            }
            return JSON.parse(body);
        },
    };
}
export async function streamRunEvents(baseUrl, runID, afterSeq, onEvent, signal, limit = 200) {
    const base = baseUrl.replace(/\/$/, "");
    const controller = signal ? null : new AbortController();
    const requestSignal = signal ?? controller?.signal;
    const timeout = controller ? setTimeout(() => controller.abort(), 15000) : null;
    const resp = await fetch(`${base}/runs/${runID}/events?after_seq=${afterSeq}&limit=${limit}`, {
        method: "GET",
        headers: { Accept: "text/event-stream" },
        signal: requestSignal,
    });
    if (timeout)
        clearTimeout(timeout);
    if (!resp.ok || !resp.body) {
        throw new Error(`Failed to stream run events for ${runID}`);
    }
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let lastSeq = afterSeq;
    while (true) {
        const { value, done } = await reader.read();
        if (done)
            return { lastSeq };
        buffer += decoder.decode(value, { stream: true });
        while (true) {
            const idx = buffer.indexOf("\n\n");
            if (idx < 0)
                break;
            const frame = buffer.slice(0, idx);
            buffer = buffer.slice(idx + 2);
            for (const line of frame.split("\n")) {
                const trimmed = line.trim();
                if (!trimmed.startsWith("data:"))
                    continue;
                const payload = trimmed.slice(5).trim();
                if (!payload)
                    continue;
                try {
                    const evt = JSON.parse(payload);
                    if (evt.event === "run_event") {
                        lastSeq = Math.max(lastSeq, evt.data.seq ?? lastSeq);
                    }
                    onEvent(evt);
                }
                catch {
                    // ignore malformed frames
                }
            }
        }
    }
}
export async function getRun(baseUrl, runID) {
    const base = baseUrl.replace(/\/$/, "");
    return fetchJSON(`${base}/runs/${runID}`);
}
export async function createRun(baseUrl, req) {
    const base = baseUrl.replace(/\/$/, "");
    const result = await fetchJSON(`${base}/runs`, {
        method: "POST",
        body: JSON.stringify(req),
    });
    return result;
}
export async function listRuns(baseUrl, sessionID, status = "", cursor = "") {
    const base = baseUrl.replace(/\/$/, "");
    const suffix = status ? `&status=${encodeURIComponent(status)}` : "";
    const cursorPart = cursor ? `&cursor=${encodeURIComponent(cursor)}` : "";
    const data = await fetchJSON(`${base}/runs?session=${encodeURIComponent(sessionID)}&limit=20${suffix}${cursorPart}`);
    return { runs: data.runs ?? [], next_cursor: data.next_cursor };
}
export async function cancelRun(baseUrl, runID) {
    const base = baseUrl.replace(/\/$/, "");
    await fetchJSON(`${base}/runs/${runID}?action=cancel`, { method: "POST" });
}
export async function replStream(baseUrl, req, onEvent, signal) {
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
            signal,
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
    let sawEvent = false;
    let lastActivity = Date.now();
    let timeoutId = null;
    const shouldStop = (event) => event === "done" || event === "error";
    const clearTimeoutId = () => {
        if (timeoutId !== null) {
            clearTimeout(timeoutId);
            timeoutId = null;
        }
    };
    const cancelReader = async () => {
        try {
            await reader.cancel();
        }
        catch {
            // ignore
        }
    };
    const cleanup = async () => {
        clearTimeoutId();
        await cancelReader();
    };
    while (true) {
        let timedOut = false;
        timeoutId = setTimeout(() => {
            if (Date.now() - lastActivity >= 15000) {
                timedOut = true;
            }
        }, 5000);
        let readResult;
        try {
            readResult = await Promise.race([
                reader.read(),
                new Promise((_, reject) => {
                    const timer = setTimeout(() => {
                        if (Date.now() - lastActivity >= 15000) {
                            reject(new Error("Stream timeout waiting for response completion"));
                        }
                    }, 15000);
                    signal?.addEventListener("abort", () => clearTimeout(timer), { once: true });
                }),
            ]);
        }
        catch (err) {
            clearTimeoutId();
            await cleanup();
            throw err;
        }
        clearTimeoutId();
        if (timedOut) {
            await cleanup();
            throw new Error("Stream timeout waiting for response completion");
        }
        const { value, done } = readResult;
        if (done)
            break;
        lastActivity = Date.now();
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
                    sawEvent = true;
                    lastActivity = Date.now();
                    if (event === "tool_result") {
                        const step = normalizeStep(raw?.data?.step ?? {});
                        const result = normalizeToolResult(raw?.data?.result ?? {});
                        const callID = raw?.data?.call_id;
                        onEvent({ event: "tool_result", data: { call_id: typeof callID === "string" ? callID : undefined, step, result } });
                        continue;
                    }
                    if (event === "confirmation") {
                        const confirmation = normalizeConfirmation(raw?.data ?? {});
                        onEvent({ event: "confirmation", data: confirmation });
                        continue;
                    }
                    if (event === "done") {
                        onEvent({ event: "done", data: normalizeResponse(raw?.data ?? {}) });
                        await cleanup();
                        return;
                    }
                    onEvent(raw);
                    if (shouldStop(event)) {
                        await cleanup();
                        return;
                    }
                }
                catch {
                    // ignore malformed frames
                }
            }
        }
    }
    await cleanup();
    if (sawEvent)
        return;
    throw new Error("Stream ended without terminal event");
}
function normalizeStep(raw) {
    const inputs = (raw.inputs ?? raw.Inputs ?? {});
    if (raw.content !== undefined && !inputs.content) {
        inputs.content = raw.content;
    }
    return {
        id: String(raw.id ?? raw.ID ?? ""),
        description: String(raw.description ?? raw.Description ?? ""),
        tool: String(raw.tool ?? raw.Tool ?? ""),
        inputs,
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
function normalizeConfirmation(raw) {
    return {
        tool: String(raw.tool ?? raw.Tool ?? ""),
        inputs: (raw.inputs ?? raw.Inputs ?? {}),
        decision: (raw.decision ?? raw.Decision ?? undefined),
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
    const rawConfirmation = (raw.confirmation ?? raw.Confirmation ?? null);
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
    const normalizedConfirmation = rawConfirmation ? normalizeConfirmation(rawConfirmation) : undefined;
    return {
        plan: normalizedPlan,
        results: normalizedResults,
        reply,
        confirmation: normalizedConfirmation,
    };
}
