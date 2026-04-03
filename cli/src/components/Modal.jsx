import { For } from "solid-js";
import { TextAttributes } from "@opentui/core";
import { theme } from "../theme";
export function Modal(props) {
    const modalWindow = () => {
        let items = props.modalItems;
        if (props.modalQuery) {
            const q = props.modalQuery.toLowerCase();
            items = items.filter((item) => item.title.toLowerCase().includes(q) ||
                (item.subtitle && item.subtitle.toLowerCase().includes(q)) ||
                (item.meta && item.meta.toLowerCase().includes(q)));
        }
        const start = Math.max(0, props.modalSelected - Math.floor(20 / 2));
        return { items, start };
    };
    const modalListHeight = () => Math.max(5, props.height - 15);
    return (<box width={props.width} height={props.height} justifyContent="center" alignItems="center">
      <box width={Math.min(80, props.width - 4)} flexDirection="column" paddingLeft={2} paddingRight={2} paddingTop={1} paddingBottom={1} backgroundColor={theme.panel}>
        <text fg={theme.primary} attributes={TextAttributes.BOLD}>
          {props.modalTitle}
        </text>
        <text fg={theme.muted}>{props.modalHint}</text>
        <input ref={(val) => props.modalInputRef} value={props.modalInput} focused={true} placeholder={props.modal === "connect" || props.modal === "modelToken"
            ? "Enter value"
            : props.modal === "confirm"
                ? "Type approve/deny"
                : "Search"} textColor={theme.text} focusedTextColor={theme.text} cursorColor={theme.primary} onInput={(value) => props.onQueryChange(typeof value === "string" ? value : props.modalInput)} onSubmit={(value) => props.onSubmit(typeof value === "string" ? value : props.modalInput)}/>
        {props.modalQuery && <text fg={theme.text}>query: {props.modalQuery}</text>}
        {props.modal === "runs" && <text fg={theme.muted}>Filter by status: running, waiting_user, timed_out, failed, cancelled · Type status or use quick filters</text>}
        {props.modal === "runs" && (<text fg={theme.muted}>{`Quick filters: ${props.runsStatusFilter === "" ? "[all]" : "all"} ${props.runsStatusFilter === "running" ? "[running]" : "running"} ${props.runsStatusFilter === "failed" ? "[failed]" : "failed"} ${props.runsStatusFilter === "timed_out" ? "[timed_out]" : "timed_out"} ${props.runsStatusFilter === "cancelled" ? "[cancelled]" : "cancelled"} · Enter to open · d for details`}</text>)}
        {props.modal === "timeline" && <text fg={theme.muted}>{props.runTimelineID ? `Run: ${props.runTimelineID}` : "Timeline"}</text>}
        <scrollbox height={modalListHeight()} paddingLeft={1} paddingRight={1}>
          {props.modal === "timeline" ? (<text fg={theme.text}>{props.runTimeline ?? "No timeline available."}</text>) : (<For each={modalWindow().items}>
              {(item, index) => {
                const selected = () => modalWindow().start + index() === props.modalSelected;
                const prefix = () => (selected() ? ">" : " ");
                const current = () => props.modal === "sessions" && item.id === props.runTimelineID;
                const number = () => modalWindow().start + index() + 1;
                const statusIcon = () => {
                    if (item.status === "timed_out")
                        return "⏱";
                    if (item.status === "failed")
                        return "✖";
                    if (item.status === "cancelled")
                        return "◌";
                    if (item.status === "waiting_user")
                        return "?";
                    if (item.status === "waiting_tool")
                        return "…";
                    if (item.status === "running")
                        return "▶";
                    return "•";
                };
                return (<text fg={selected() ? theme.primary : current() ? theme.tool : theme.text} attributes={selected() ? TextAttributes.BOLD : TextAttributes.NONE}>
                    {prefix()}{number()}. {statusIcon()} {item.title}
                    {item.subtitle && <text fg={theme.muted}> ({item.subtitle})</text>}
                    {item.meta && <text fg={theme.muted}> - {item.meta}</text>}
                  </text>);
            }}
            </For>)}
        </scrollbox>
      </box>
    </box>);
}
