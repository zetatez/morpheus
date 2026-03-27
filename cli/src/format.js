const MAX_PREVIEW = 800;
const MAX_MATCHES = 8;
function stringify(value) {
    if (value == null)
        return "";
    if (typeof value === "string")
        return value;
    try {
        return JSON.stringify(value, null, 2);
    }
    catch {
        return String(value);
    }
}
export function formatToolOutput(step, result) {
    const inputs = (step.inputs ?? {});
    const note = formatToolNote(step.description);
    const thinking = formatThinking(step.description);
    const args = formatToolArgsPresentation(step.tool, inputs);
    const header = formatToolLead(step, args.inline, result);
    const hasFileLine = args.lines.some((line) => line.startsWith("File: "));
    if (!result) {
        return joinLines(header, note, thinking, ...args.lines);
    }
    if (!result.success && result.error) {
        return joinLines(header, note, thinking, ...args.lines, `Error: ${result.error}`);
    }
    const data = result.data ?? {};
    if (step.tool === "fs.patch") {
        const patch = stringify(data.patch || inputs.patch);
        const path = stringify(data.path || inputs.path || inputs.filePath);
        if (patch) {
            const patchedLine = path ? `# Patched ${path}` : "# Patched";
            const lang = isDiffPatch(patch) ? "diff" : inferCodeLanguage(path);
            return joinLines(header, note, thinking, ...args.lines, patchedLine, wrapCodeBlock(patch, lang));
        }
        if (hasFileLine)
            return joinLines(header, note, thinking, ...args.lines, "Patched");
        return joinLines(header, note, thinking, ...args.lines, path ? `Patched ${path}` : "Patched");
    }
    if (step.tool === "fs.write" || step.tool === "fs.create" || step.tool === "fs.edit") {
        const path = stringify(data.path || inputs.path || inputs.filePath);
        const size = stringify(data.size);
        const inputContent = inputs.content;
        const dataContent = data.content;
        const content = inputContent ?? dataContent ?? "";
        const isEdit = step.tool === "fs.edit";
        const createdLine = isEdit
            ? path
                ? `# Edited ${path}`
                : "# Edited"
            : path
                ? `# Created ${path}`
                : "# Created";
        if (content) {
            const preview = truncateLines(content, 300);
            const lang = inferCodeLanguage(path);
            return joinLines(header, note, thinking, ...args.lines, createdLine, wrapCodeBlock(preview, lang));
        }
        if (size) {
            if (hasFileLine)
                return joinLines(header, note, thinking, ...args.lines, `Wrote (${size} bytes)`);
            if (path)
                return joinLines(header, note, thinking, ...args.lines, `Wrote ${path} (${size} bytes)`);
            return joinLines(header, note, thinking, ...args.lines, `Wrote (${size} bytes)`);
        }
        if (hasFileLine)
            return joinLines(header, note, thinking, ...args.lines, "Wrote");
        if (path)
            return joinLines(header, note, thinking, ...args.lines, `Wrote ${path}`);
        return joinLines(header, note, thinking, ...args.lines, "Wrote");
    }
    if (step.tool === "fs.read") {
        const path = stringify(data.path);
        const total = stringify(data.total_lines);
        const readLine = path ? `Read ${path}` : "Read file";
        return joinLines(header, note, thinking, ...args.lines, total ? `${readLine} (${total} lines)` : readLine);
    }
    if (step.tool === "cmd.exec") {
        const commandLine = formatShellCommand(stringify(inputs.command));
        const stdout = truncate(stringify(data.stdout), MAX_PREVIEW);
        const stderr = truncate(stringify(data.stderr), MAX_PREVIEW);
        if (stdout || stderr) {
            return joinLines(header, note, thinking, ...args.lines, wrapCodeBlock(commandLine + (stdout ? `\n\n${stdout}` : ""), "bash"), stderr ? wrapCodeBlock(stderr, "stderr") : "");
        }
        return joinLines(header, note, thinking, ...args.lines, wrapCodeBlock(commandLine, "bash"));
    }
    if (step.tool === "web.fetch") {
        const body = stringify(data.body || data.text);
        const status = stringify(data.status);
        const contentType = stringify(data.content_type);
        const summary = body ? formatWebFetchSummary(String(inputs.url || ""), body) : [];
        const concise = formatWebFetchConciseBody(body, contentType);
        if (summary.length || concise || status || contentType) {
            const meta = [];
            if (status)
                meta.push(`Status: ${status}`);
            if (contentType)
                meta.push(`Type: ${contentType}`);
            return joinLines(header, note, thinking, ...args.lines, meta.join(" · "), ...(summary.length ? summary : concise ? [concise] : []));
        }
        if (body) {
            const pretty = formatMaybeJSON(body);
            const lang = pretty.isJSON ? "json" : "";
            const preview = truncate(pretty.text, 240);
            return joinLines(header, note, thinking, ...args.lines, status ? `Status: ${status}` : "", contentType ? `Type: ${contentType}` : "", wrapCodeBlock(preview, lang));
        }
    }
    if (Array.isArray(data.matches)) {
        const matches = data.matches;
        return joinLines(header, note, thinking, ...args.lines, `${matches.length} results`);
    }
    const preferred = stringify(data.stdout) ||
        stringify(data.content) ||
        stringify(data.body) ||
        stringify(data.tree) ||
        stringify(data.text) ||
        stringify(data.patch);
    if (preferred) {
        const preview = truncate(preferred, MAX_PREVIEW);
        return joinLines(header, note, thinking, ...args.lines, wrapCodeBlock(preview));
    }
    return joinLines(header, note, thinking, ...args.lines, wrapCodeBlock(truncate(stringify(data), MAX_PREVIEW)));
}
function formatToolArgsPresentation(tool, inputs) {
    const keys = Object.keys(inputs ?? {});
    if (keys.length === 0)
        return { inline: "", lines: [] };
    const get = (name) => inputs[name];
    const kv = (name) => `${name}=${inlineValue(get(name))}`;
    const rawPath = (get("path") ?? get("filePath"));
    switch (tool) {
        case "fs.read":
            {
                const p = inlineValue(rawPath);
                return { inline: p, lines: [] };
            }
        case "fs.create":
        case "fs.write":
        case "fs.edit":
            {
                const p = inlineValue(rawPath);
                const lines = [];
                if (p)
                    lines.push(`File: ${p}`);
                return { inline: "", lines };
            }
        case "fs.patch": {
            const p = inlineValue(rawPath);
            const lines = [];
            if (p)
                lines.push(`File: ${p}`);
            return { inline: "", lines };
        }
        case "fs.glob": {
            const pat = inlineValue(get("pattern"));
            const p = inlineValue(get("path"));
            if (pat && p)
                return { inline: `${pat} ${kv("path")}`, lines: [] };
            return { inline: pat || kv("path"), lines: [] };
        }
        case "fs.grep": {
            const q = get("query") !== undefined ? kv("query") : "";
            const pat = get("pattern") !== undefined ? kv("pattern") : "";
            const p = get("path") !== undefined ? kv("path") : "";
            const inc = get("include") !== undefined ? kv("include") : "";
            return { inline: [q, pat, inc, p].filter(Boolean).join(" "), lines: [] };
        }
        case "cmd.exec": {
            const wd = get("workdir") !== undefined ? kv("workdir") : "";
            const t = get("timeout") !== undefined ? kv("timeout") : "";
            return { inline: [wd, t].filter(Boolean).join(" "), lines: [] };
        }
        case "web.fetch": {
            const url = get("url") !== undefined ? kv("url") : "";
            const fmt = get("format") !== undefined ? kv("format") : "";
            const t = get("timeout") !== undefined ? kv("timeout") : "";
            return { inline: [url, fmt, t].filter(Boolean).join(" "), lines: [] };
        }
        default:
            return {
                inline: keys
                    .sort()
                    .slice(0, 6)
                    .map((k) => `${k}=${inlineValue(inputs[k])}`)
                    .join(" "),
                lines: [],
            };
    }
}
function inlineValue(value) {
    if (value == null)
        return "";
    if (typeof value === "string") {
        const trimmed = value.replace(/\s+/g, " ").trim();
        if (trimmed.length === 0)
            return "";
        const short = trimmed.length > 200 ? `${trimmed.slice(0, 200)}...` : trimmed;
        // keep file paths / short tokens readable without key= quoting
        if (/^[A-Za-z0-9._\-/~]+$/.test(short))
            return short;
        return JSON.stringify(short);
    }
    try {
        const text = JSON.stringify(value);
        return text.length > 200 ? `${text.slice(0, 200)}...` : text;
    }
    catch {
        return String(value);
    }
}
function joinInline(a, b) {
    const left = (a ?? "").trim();
    const right = (b ?? "").trim();
    if (!left)
        return right;
    if (!right)
        return left;
    return `${left} ${right}`;
}
function formatToolLead(step, inline, result) {
    const tool = step.tool || "";
    const input = step.inputs ?? {};
    const label = categorizeToolLabel(tool);
    if (tool === "fs.grep") {
        const pattern = inlineValue(input["pattern"] ?? input["query"]);
        const path = inlineValue(input["path"]);
        const count = Array.isArray(result?.data?.matches)
            ? (result?.data).matches.length
            : 0;
        const where = path ? ` in ${path}` : "";
        const suffix = count > 0 ? ` (${count} matches)` : "";
        return `✱ ${label} ${pattern}${where}${suffix}`.trim();
    }
    if (tool === "fs.glob") {
        const pattern = inlineValue(input["pattern"]);
        const path = inlineValue(input["path"]);
        const count = Array.isArray(result?.data?.matches)
            ? (result?.data).matches.length
            : 0;
        const where = path ? ` in ${path}` : "";
        const suffix = count > 0 ? ` (${count} matches)` : "";
        return `✱ ${label} ${pattern}${where}${suffix}`.trim();
    }
    if (tool === "fs.read") {
        const path = inlineValue(input["path"] ?? input["filePath"]);
        return path ? `✱ ${label} ${path}` : `✱ ${label}`;
    }
    if (tool === "fs.write" || tool === "fs.create" || tool === "fs.edit") {
        const path = inlineValue(input["path"] ?? input["filePath"]);
        return path ? `✱ ${label} ${path}` : `✱ ${label}`;
    }
    if (tool === "fs.patch") {
        const path = inlineValue(input["path"] ?? input["filePath"]);
        return path ? `✱ ${label} ${path}` : `✱ ${label}`;
    }
    if (tool === "cmd.exec") {
        const command = inlineValue(input["command"]);
        return command ? `✱ ${label} ${command}` : `✱ ${label}`;
    }
    if (tool === "web.fetch") {
        const url = inlineValue(input["url"]);
        return url ? `✱ ${label} ${url}` : `✱ ${label}`;
    }
    if (inline) {
        return `✱ ${label} ${inline}`.trim();
    }
    return tool ? `✱ ${label}` : "✱ Tool";
}
function categorizeToolLabel(tool) {
    if (tool === "fs.read" || tool === "fs.grep" || tool === "fs.glob" || tool === "web.fetch")
        return "Check:";
    if (tool === "fs.write" || tool === "fs.create" || tool === "fs.edit" || tool === "fs.patch")
        return "Edit:";
    if (tool === "cmd.exec")
        return "Run:";
    return "Tool:";
}
function formatThinking(description) {
    if (!description)
        return "";
    const trimmed = description.trim();
    if (!trimmed.toLowerCase().startsWith("thinking:"))
        return "";
    return trimmed;
}
function formatToolNote(description) {
    if (!description)
        return "";
    const trimmed = description.trim();
    const lower = trimmed.toLowerCase();
    if (lower.startsWith("tool call:") || lower.startsWith("thinking:"))
        return "";
    return `# ${trimmed}`;
}
function extractContent(inputs) {
    const content = inputs.content;
    if (typeof content === "string")
        return content;
    return "";
}
function extractWriteContent(inputs, data) {
    for (const key of ["content", "text", "body", "file_content", "new_content", "written_content", "value", "code"]) {
        if (typeof (inputs[key]) === "string" && inputs[key].length > 0) {
            return inputs[key];
        }
    }
    for (const key of Object.keys(inputs)) {
        const val = inputs[key];
        if (typeof val === "string" && val.length > 10) {
            return val;
        }
    }
    for (const key of Object.keys(data)) {
        const val = data[key];
        if (typeof val === "string" && val.length > 0) {
            return val;
        }
    }
    return "";
}
function formatShellCommand(command) {
    const lines = command
        .split("\n")
        .map((line) => line.trim())
        .filter((line) => line.length > 0);
    if (lines.length === 0)
        return "";
    if (lines.length === 1)
        return `$ ${lines[0]}`;
    return lines.map((line) => `$ ${line}`).join("\n");
}
function formatMaybeJSON(text) {
    const trimmed = text.trim();
    if (trimmed === "")
        return { text, isJSON: false };
    if (!(trimmed.startsWith("{") || trimmed.startsWith("["))) {
        return { text, isJSON: false };
    }
    try {
        const parsed = JSON.parse(trimmed);
        return { text: JSON.stringify(parsed, null, 2), isJSON: true };
    }
    catch {
        return { text, isJSON: false };
    }
}
function formatWebFetchSummary(url, body) {
    return formatWeatherSummary(body) || formatFxSummary(url, body) || formatNewsSummary(body) || [];
}
function formatWebFetchConciseBody(body, contentType) {
    const trimmed = body.trim();
    if (!trimmed)
        return "";
    const lowerType = (contentType || "").toLowerCase();
    if (lowerType.includes("xml") || trimmed.startsWith("<?xml") || trimmed.startsWith("<rss") || trimmed.startsWith("<feed")) {
        return "Fetched structured feed data.";
    }
    if (trimmed.startsWith("<html") || trimmed.startsWith("<!doctype html")) {
        return "Fetched webpage content.";
    }
    if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
        return "Fetched structured data.";
    }
    return "Fetched text content.";
}
function formatWeatherSummary(body) {
    try {
        const parsed = JSON.parse(body);
        const current = parsed.current_weather || parsed.current_condition?.[0];
        if (!current)
            return null;
        const temp = current.temperature ?? current.temp_C ?? current.FeelsLikeC;
        const wind = current.windspeed ?? current.windspeedKmph;
        const desc = current.weatherDesc?.[0]?.value || weatherCodeLabel(current.weathercode) || "";
        const lines = ["Weather summary:"];
        if (temp !== undefined)
            lines.push(`- Temperature: ${temp}${current.temperature !== undefined ? "°C" : "°C"}`);
        if (desc)
            lines.push(`- Condition: ${desc}`);
        if (wind !== undefined)
            lines.push(`- Wind: ${wind} km/h`);
        return lines.length > 1 ? lines : null;
    }
    catch {
        return null;
    }
}
function formatFxSummary(url, body) {
    const source = `${url}\n${body}`;
    const match = source.match(/(?:USD\/?CNY|美元兑人民币|人民币\/?美元|AAPL|price|quote)[^\n]{0,80}?([0-9]+(?:\.[0-9]+)?)/i);
    if (!match)
        return null;
    return ["Market summary:", `- Latest value: ${match[1]}`];
}
function formatNewsSummary(body) {
    const titles = Array.from(body.matchAll(/<title>([^<]+)<\/title>|"title"\s*:\s*"([^"]+)"/gi))
        .map((m) => (m[1] || m[2] || "").trim())
        .filter(Boolean)
        .slice(0, 3);
    if (titles.length === 0)
        return null;
    return ["Headline preview:", ...titles.map((title) => `- ${title}`)];
}
function weatherCodeLabel(code) {
    const value = Number(code);
    if (Number.isNaN(value))
        return "";
    if (value === 0)
        return "Clear";
    if (value <= 3)
        return "Partly cloudy";
    if (value <= 48)
        return "Fog";
    if (value <= 67)
        return "Rain";
    if (value <= 77)
        return "Snow";
    if (value <= 99)
        return "Storm";
    return "";
}
function truncateLines(content, maxLines) {
    const lines = content.replace(/\r\n/g, "\n").split("\n");
    if (lines.length <= maxLines)
        return lines.join("\n");
    return [...lines.slice(0, maxLines), "..."].join("\n");
}
function isDiffPatch(patch) {
    const trimmed = patch.trim();
    if (trimmed.startsWith("diff --git"))
        return true;
    if (trimmed.startsWith("@@"))
        return true;
    return /\n\+\+\+|\n---/.test(trimmed);
}
function inferCodeLanguage(path) {
    const lower = (path ?? "").toLowerCase();
    const idx = lower.lastIndexOf(".");
    if (idx === -1)
        return "";
    const ext = lower.slice(idx + 1);
    switch (ext) {
        case "ts":
            return "ts";
        case "tsx":
            return "tsx";
        case "js":
            return "js";
        case "jsx":
            return "jsx";
        case "go":
            return "go";
        case "py":
            return "py";
        case "md":
            return "md";
        case "yaml":
        case "yml":
            return "yaml";
        case "json":
            return "json";
        case "sh":
        case "bash":
        case "zsh":
            return "bash";
        case "sql":
            return "sql";
        case "html":
            return "html";
        case "css":
            return "css";
        case "scss":
            return "scss";
        case "java":
            return "java";
        case "rb":
            return "rb";
        case "rs":
            return "rs";
        case "c":
        case "h":
            return "c";
        case "cpp":
        case "cc":
        case "hpp":
            return "cpp";
        case "cs":
            return "csharp";
        case "kt":
            return "kotlin";
        case "swift":
            return "swift";
        case "php":
            return "php";
        default:
            return "";
    }
}
function wrapCodeBlock(content, lang = "") {
    const trimmed = content.trimEnd();
    if (!trimmed)
        return "";
    const info = lang ? lang : "";
    return `\`\`\`${info}\n${trimmed}\n\`\`\`\n`;
}
function truncate(value, max) {
    if (value.length <= max)
        return value;
    return `${value.slice(0, max)}\n...`;
}
function joinLines(...parts) {
    return parts.filter((part) => part && part.trim().length > 0).join("\n");
}
