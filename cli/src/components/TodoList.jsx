import { For } from "solid-js";
import { TextAttributes } from "@opentui/core";
import { theme } from "../theme";
export function todoLineColor(todo, pulse) {
    if (todo.status === "completed")
        return theme.todoDone;
    if (todo.status === "failed")
        return theme.todoFailed;
    if (todo.status === "cancelled")
        return theme.todoCancelled;
    if (todo.status === "in_progress")
        return pulse ? theme.primary : theme.todoActive;
    return theme.todoPending;
}
export function TodoList(props) {
    return (<box width={props.panelWidth} height={props.contentHeight} flexDirection="column" paddingLeft={1} paddingRight={2} paddingTop={1} backgroundColor={theme.todoPanel}>
      <text fg={theme.primary} attributes={TextAttributes.BOLD}># Todos</text>
      <box paddingBottom={1}/>
      <For each={props.todos}>
        {(todo) => {
            const note = () => todo.note ? `  ${todo.note}` : todo.status === "failed" ? "  Retry or update with todo.write" : "";
            return (<box flexDirection="column" paddingBottom={1}>
              <text fg={todoLineColor(todo, props.todoPulse)} attributes={todo.active ? TextAttributes.BOLD : TextAttributes.NONE}>
                {`${todo.status === "completed" ? "[x]" : todo.status === "in_progress" ? (props.todoPulse ? "[•]" : "[>]") : todo.status === "failed" ? "[!]" : todo.status === "cancelled" ? "[-]" : "[ ]"} ${todo.content}${todo.tool ? ` (${todo.tool})` : ""}`}
              </text>
              {note() && <text fg={todo.status === "failed" ? theme.todoFailed : theme.muted}>{note()}</text>}
            </box>);
        }}
      </For>
    </box>);
}
