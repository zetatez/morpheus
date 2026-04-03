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
