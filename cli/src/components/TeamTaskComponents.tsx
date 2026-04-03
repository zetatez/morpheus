import { For, Show } from "solid-js"
import { TextAttributes } from "@opentui/core"
import type { TeamTaskEvent } from "../api"
import { theme } from "../theme"

interface TeamTaskPanelProps {
  tasks: TeamTaskEvent[]
  taskPulse: boolean
  panelWidth: number
  contentHeight: number
}

function taskStatusColor(status: string, pulse: boolean): string {
  if (status === "completed") return theme.todoDone
  if (status === "failed") return theme.todoFailed
  if (status === "cancelled") return theme.todoCancelled
  if (status === "running") return pulse ? theme.primary : theme.todoActive
  return theme.todoPending
}

export function TeamTaskPanel(props: TeamTaskPanelProps) {
  return (
    <Show when={props.tasks.length > 0}>
      <box
        width={props.panelWidth}
        height={props.contentHeight}
        flexDirection="column"
        paddingLeft={1}
        paddingRight={2}
        paddingTop={1}
        backgroundColor={theme.todoPanel}
      >
        <text fg={theme.primary} attributes={TextAttributes.BOLD}># Team Tasks</text>
        <box paddingBottom={1} />
        <For each={props.tasks}>
          {(task) => {
            const statusIcon = () => {
              if (task.status === "completed") return "[x]"
              if (task.status === "running") return props.taskPulse ? "[•]" : "[>]"
              if (task.status === "failed") return "[!]"
              if (task.status === "cancelled") return "[-]"
              return "[ ]"
            }
            return (
              <box flexDirection="column" paddingBottom={1}>
                <text
                  fg={taskStatusColor(task.status, props.taskPulse)}
                >
                  {`${statusIcon()} [${task.role}] ${truncate(task.prompt, 40)}`}
                </text>
                <Show when={task.summary}>
                  <text
                    fg={theme.muted}
                  >
                    {`  → ${truncate(task.summary || "", 60)}`}
                  </text>
                </Show>
                <Show when={task.error}>
                  <text
                    fg={theme.todoFailed}
                  >
                    {`  ! ${truncate(task.error || "", 60)}`}
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

function truncate(str: string, maxLen: number): string {
  if (str.length <= maxLen) return str
  return str.slice(0, maxLen - 3) + "..."
}
