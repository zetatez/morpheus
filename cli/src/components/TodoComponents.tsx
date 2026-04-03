import { For, Show } from "solid-js"
import { TextAttributes } from "@opentui/core"
import type { TodoItem } from "../api"
import { theme } from "../theme"

interface TodoListProps {
  todos: TodoItem[]
  todoPulse: boolean
  panelWidth: number
  contentHeight: number
}

function todoLineColor(todo: TodoItem, pulse: boolean): string {
  if (todo.status === "completed") return theme.todoDone
  if (todo.status === "failed") return theme.todoFailed
  if (todo.status === "cancelled") return theme.todoCancelled
  if (todo.status === "in_progress") return pulse ? theme.primary : theme.todoActive
  return theme.todoPending
}

export function TodoList(props: TodoListProps) {
  return (
    <Show when={props.todos.length > 0}>
      <box
        width={props.panelWidth}
        height={props.contentHeight}
        flexDirection="column"
        paddingLeft={1}
        paddingRight={2}
        paddingTop={1}
        backgroundColor={theme.todoPanel}
      >
        <text fg={theme.primary} attributes={TextAttributes.BOLD}># Todos</text>
        <box paddingBottom={1} />
        <For each={props.todos}>
          {(todo) => {
            const note = () => todo.note
              ? `  ${todo.note}`
              : todo.status === "failed"
                ? "  Retry or update with todo.write"
                : ""
            const statusIcon = () => {
              if (todo.status === "completed") return "[x]"
              if (todo.status === "in_progress") return props.todoPulse ? "[•]" : "[>]"
              if (todo.status === "failed") return "[!]"
              if (todo.status === "cancelled") return "[-]"
              return "[ ]"
            }
            return (
              <box flexDirection="column" paddingBottom={1}>
                <text
                  fg={todoLineColor(todo, props.todoPulse)}
                  attributes={todo.active ? TextAttributes.BOLD : TextAttributes.NONE}
                >
                  {`${statusIcon()} ${todo.content}${todo.tool ? ` (${todo.tool})` : ""}`}
                </text>
                <Show when={note()}>
                  <text
                    fg={todo.status === "failed" ? theme.todoFailed : theme.muted}
                  >
                    {note()}
                  </text>
                </Show>
              </box>
            )
          }}
        </For>
      </box>
    </Show>
  )
}
