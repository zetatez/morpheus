export function formatToken(count) {
    if (count >= 1000000)
        return `${(count / 1000000).toFixed(1)}M`;
    if (count >= 1000)
        return `${(count / 1000).toFixed(1)}K`;
    return count.toString();
}
export function formatTodoBlock(todos) {
    if (todos.length === 0)
        return "";
    return `${todos.map((todo) => {
        const mark = todo.status === "completed" ? "[x]" : todo.status === "in_progress" ? "[•]" : todo.status === "failed" ? "[!]" : todo.status === "cancelled" ? "[-]" : "[ ]";
        const suffix = todo.tool ? ` (${todo.tool})` : "";
        const note = todo.note ? ` - ${todo.note}` : "";
        return `${mark} ${todo.content}${suffix}${note}`;
    }).join("\n")}`;
}
export function formatSessionID() {
    const now = new Date();
    const pad = (value, size = 2) => String(value).padStart(size, "0");
    const date = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}`;
    const time = `${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
    const ms = pad(now.getMilliseconds(), 3);
    return `${date}-${time}-${ms}`;
}
export function parseAttachmentPaths(text) {
    const paths = [];
    const lines = text.split("\n");
    for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed)
            continue;
        if (trimmed.startsWith("!")) {
            const url = trimmed.slice(1).trim();
            if (url.startsWith("http://") || url.startsWith("https://")) {
                paths.push({ url, kind: "url" });
            }
        }
        else if (trimmed.startsWith("/") || trimmed.startsWith(".")) {
            paths.push({ path: trimmed, kind: "file" });
        }
    }
    return paths;
}
export function todoLineColor(todo, pulse, theme) {
    if (todo.status === "completed")
        return theme.todoDone ?? theme.success;
    if (todo.status === "failed")
        return theme.todoFailed ?? theme.error;
    if (todo.status === "cancelled")
        return theme.todoCancelled ?? theme.muted;
    if (todo.status === "in_progress")
        return pulse ? theme.primary ?? theme.text : theme.todoActive ?? theme.text;
    return theme.todoPending ?? theme.muted;
}
