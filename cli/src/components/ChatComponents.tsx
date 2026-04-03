import { For, Show, createMemo } from "solid-js"
import { TextAttributes } from "@opentui/core"
import type { Entry } from "../types"
import { theme } from "../theme"
import { renderMarkdownLines } from "../markdown"
import { formatToken } from "../util/helpers"

interface StatusBarProps {
  apiUrl: string
  sessionID: string
  serverMetrics: { input_tokens?: number; output_tokens?: number; cost?: number } | null
  agentMode: "build" | "plan"
}

export function StatusBar(props: StatusBarProps) {
  return (
    <box flexDirection="row" paddingLeft={2} paddingRight={2} paddingTop={1} paddingBottom={1}>
      <text fg={theme.muted}>{props.apiUrl}</text>
      <box flexGrow={1} />
      <text fg={theme.muted}>session {props.sessionID}</text>
      <box paddingLeft={3} />
      <text fg={theme.muted}>↑{formatToken(props.serverMetrics?.input_tokens ?? 0)}</text>
      <box paddingLeft={1} />
      <text fg={theme.muted}>↓{formatToken(props.serverMetrics?.output_tokens ?? 0)}</text>
      <box paddingLeft={1} />
      <text fg={theme.muted}>${props.serverMetrics?.cost?.toFixed(3) ?? "0.000"}</text>
    </box>
  )
}

interface ToolEntryProps {
  entry: Entry
  isExpanded: boolean
  onToggle: () => void
}

export function ToolEntry(props: ToolEntryProps) {
  const lines = createMemo(() => renderMarkdownLines(props.entry.content ?? "", {
    text: theme.muted,
    muted: theme.muted,
    code: theme.muted,
    success: theme.success,
    output: theme.muted,
    file: theme.file,
    accent: theme.tool,
    error: theme.error,
    thinking: theme.thinking,
  }))

  const maxLines = 12
  const visibleLines = createMemo(() => props.isExpanded ? lines() : lines().slice(0, maxLines))
  const hiddenCount = createMemo(() => lines().length - visibleLines().length)

  return (
    <box flexDirection="column">
      <For each={visibleLines()}>
        {(line) => (
          <text fg={line.fg ?? theme.text} attributes={line.attributes}>
            {line.text}
          </text>
        )}
      </For>
      <Show when={lines().length > maxLines && !props.isExpanded}>
        <text
          fg={theme.muted}
          attributes={TextAttributes.BOLD}
          onMouseDown={() => props.onToggle()}
        >
          … {hiddenCount()} more (click to expand)
        </text>
      </Show>
      <Show when={lines().length > maxLines && props.isExpanded}>
        <text
          fg={theme.muted}
          attributes={TextAttributes.BOLD}
          onMouseDown={() => props.onToggle()}
        >
          Collapse output
        </text>
      </Show>
    </box>
  )
}

interface ChatEntriesProps {
  entries: Entry[]
  isToolExpanded: (id: string) => boolean
  onToggleTool: (id: string) => void
  mainPanelWidth: number
  contentHeight: number
  scrollRef?: (val: import("@opentui/core").ScrollBoxRenderable) => void
}

export function ChatEntries(props: ChatEntriesProps) {
  return (
    <scrollbox
      ref={props.scrollRef}
      width={props.mainPanelWidth}
      height={props.contentHeight}
      paddingLeft={2}
      paddingRight={2}
      paddingTop={1}
    >
      <For each={props.entries}>
        {(entry) => (
          <box flexDirection="column" paddingBottom={1}>
            <Show when={(entry.role === "tool" || entry.role === "error") && entry.title}>
              <text
                fg={entry.role === "error" ? theme.error : theme.tool}
                attributes={TextAttributes.BOLD}
              >
                {entry.role === "tool" ? entry.title ?? "Tool" : "Error"}
              </text>
            </Show>

            <Show when={entry.role === "assistant"}>
              <box flexDirection="column">
                <For each={renderMarkdownLines(entry.content, {
                  text: theme.text,
                  muted: theme.thinking,
                  code: theme.primary,
                  success: theme.success,
                  output: theme.output,
                  file: theme.file,
                  accent: theme.tool,
                  error: theme.error,
                  thinking: theme.thinking,
                })}>
                  {(line) => (
                    <text fg={line.content ? undefined : (line.fg ?? theme.text)} attributes={line.attributes} content={line.content}>
                      {line.content ? undefined : line.text}
                    </text>
                  )}
                </For>
              </box>
            </Show>

            <Show when={entry.role === "user"}>
              <text fg={theme.user} attributes={TextAttributes.BOLD}>
                {">"} {entry.content.split("\n")[0]}
              </text>
            </Show>

            <Show when={entry.role === "system"}>
              <text fg={theme.muted} attributes={TextAttributes.ITALIC}>
                {entry.content}
              </text>
            </Show>

            <Show when={entry.role === "tool"}>
              <ToolEntry
                entry={entry}
                isExpanded={props.isToolExpanded(entry.id)}
                onToggle={() => props.onToggleTool(entry.id)}
              />
            </Show>

            <Show when={entry.role === "queue"}>
              <box flexDirection="column" paddingLeft={1}>
                <For each={entry.content.split("\n")}>
                  {(line) => (
                    <text fg={theme.muted} attributes={TextAttributes.BOLD}>
                      {line}
                    </text>
                  )}
                </For>
              </box>
            </Show>

            <Show when={entry.role === "error"}>
              <text fg={theme.error}>{entry.content}</text>
            </Show>
          </box>
        )}
      </For>
    </scrollbox>
  )
}
