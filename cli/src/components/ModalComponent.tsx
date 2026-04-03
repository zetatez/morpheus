import { For, Show, createMemo } from "solid-js"
import { TextAttributes } from "@opentui/core"
import type { InputRenderable } from "@opentui/core"
import type { ModalKind, ModalItem } from "./shared"
import { theme } from "../theme"

interface ModalProps {
  modal: ModalKind | null
  modalTitle: string
  modalHint: string
  modalQuery: string
  modalItems: ModalItem[]
  modalSelected: number
  modalInput: string
  modalInputRef?: InputRenderable
  runsStatusFilter: string
  runTimeline?: string | null
  runTimelineID?: string | null
  onQueryChange: (query: string) => void
  onSubmit: (value: string) => void
  width: number
  height: number
}

function statusIcon(status?: string): string {
  if (status === "timed_out") return "⏱"
  if (status === "failed") return "✖"
  if (status === "cancelled") return "◌"
  if (status === "waiting_user") return "?"
  if (status === "waiting_tool") return "…"
  if (status === "running") return "▶"
  return "•"
}

export function Modal(props: ModalProps) {
  const modalWindow = createMemo(() => {
    let items = props.modalItems
    if (props.modalQuery) {
      const q = props.modalQuery.toLowerCase()
      items = items.filter(
        (item) =>
          item.title.toLowerCase().includes(q) ||
          (item.subtitle && item.subtitle.toLowerCase().includes(q)) ||
          (item.meta && item.meta.toLowerCase().includes(q))
      )
    }
    const start = Math.max(0, props.modalSelected - Math.floor(20 / 2))
    return { items, start }
  })

  const modalListHeight = createMemo(() => Math.max(5, props.height - 15))

  const quickFilters = createMemo(() => [
    { label: "all", value: "" },
    { label: "running", value: "running" },
    { label: "waiting_user", value: "waiting_user" },
    { label: "failed", value: "failed" },
    { label: "timed_out", value: "timed_out" },
    { label: "cancelled", value: "cancelled" },
  ])

  return (
    <Show when={props.modal}>
      <box width={props.width} height={props.height} justifyContent="center" alignItems="center">
        <box
          width={Math.min(80, props.width - 4)}
          flexDirection="column"
          paddingLeft={2}
          paddingRight={2}
          paddingTop={1}
          paddingBottom={1}
          backgroundColor={theme.panel}
        >
          <text fg={theme.primary} attributes={TextAttributes.BOLD}>
            {props.modalTitle}
          </text>
          <text fg={theme.muted}>{props.modalHint}</text>

          <input
            ref={(val: InputRenderable) => props.modalInputRef}
            value={props.modalInput}
            focused={true}
            placeholder={
              props.modal === "connect" || props.modal === "modelToken"
                ? "Enter value"
                : props.modal === "confirm"
                  ? "Type approve/deny"
                  : "Search"
            }
            textColor={theme.text}
            focusedTextColor={theme.text}
            cursorColor={theme.primary}
            onInput={(value) => props.onQueryChange(typeof value === "string" ? value : props.modalInput)}
            onSubmit={(value) => props.onSubmit(typeof value === "string" ? value : props.modalInput)}
          />

          <Show when={props.modalQuery}>
            <text fg={theme.text}>query: {props.modalQuery}</text>
          </Show>

          <Show when={props.modal === "runs"}>
            <text fg={theme.muted}>
              Filter by status: running, waiting_user, timed_out, failed, cancelled · Type status or use quick filters
            </text>
            <text fg={theme.muted}>
              {"Quick filters: "}
              <For each={quickFilters()}>
                {(filter, i) => (
                  <text
                    fg={props.runsStatusFilter === filter.value ? theme.primary : theme.muted}
                  >
                    {props.runsStatusFilter === filter.value ? `[${filter.label}]` : filter.label}{" "}
                  </text>
                )}
              </For>
              {"· Enter to open · d for details"}
            </text>
          </Show>

          <Show when={props.modal === "timeline"}>
            <text fg={theme.muted}>
              {props.runTimelineID ? `Run: ${props.runTimelineID}` : "Timeline"}
            </text>
          </Show>

          <scrollbox height={modalListHeight()} paddingLeft={1} paddingRight={1}>
            <Show
              when={props.modal === "timeline"}
              fallback={
                <For each={modalWindow().items}>
                  {(item, index) => {
                    const selected = () => modalWindow().start + index() === props.modalSelected
                    const current = () => props.modal === "sessions" && item.id === props.runTimelineID
                    const number = () => modalWindow().start + index() + 1
                    return (
                      <text
                        fg={selected() ? theme.primary : current() ? theme.tool : theme.text}
                        attributes={selected() ? TextAttributes.BOLD : TextAttributes.NONE}
                      >
                        {selected() ? ">" : " "}{number()}. {statusIcon(item.status)} {item.title}
                        <Show when={item.subtitle}>
                          <text fg={theme.muted}> ({item.subtitle})</text>
                        </Show>
                        <Show when={item.meta}>
                          <text fg={theme.muted}> - {item.meta}</text>
                        </Show>
                      </text>
                    )
                  }}
                </For>
              }
            >
              <text fg={theme.text}>
                {props.runTimeline ?? "No timeline available."}
              </text>
            </Show>
          </scrollbox>
        </box>
      </box>
    </Show>
  )
}
