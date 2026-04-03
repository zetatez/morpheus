import { For } from "solid-js";
import { TextAttributes } from "@opentui/core";
import { theme } from "../theme";
import { renderMarkdownLines } from "../markdown";
export function ChatEntries(props) {
    return (<scrollbox ref={(val) => props.scroll} width={props.mainPanelWidth} height={props.contentHeight} paddingLeft={2} paddingRight={2} paddingTop={1}>
      <For each={props.entries}>
        {(entry) => (<box flexDirection="column" paddingBottom={1}>
            {(entry.role === "tool" || entry.role === "error") && entry.title && (<text fg={entry.role === "error" ? theme.error : theme.tool} attributes={TextAttributes.BOLD}>
                {entry.role === "tool" ? entry.title ?? "Tool" : "Error"}
              </text>)}
            {entry.role === "assistant" ? (<box flexDirection="column">
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
                  {(line) => (<text fg={line.fg ?? theme.text} attributes={line.attributes}>
                      {line.text}
                    </text>)}
                </For>
              </box>) : entry.role === "user" ? (<text fg={theme.user} attributes={TextAttributes.BOLD}>
                {">"} {entry.content.split("\n")[0]}
              </text>) : entry.role === "system" ? (<text fg={theme.muted} attributes={TextAttributes.ITALIC}>
                {entry.content}
              </text>) : entry.role === "tool" ? (<box flexDirection="column" paddingLeft={2}>
                {entry.content.split("\n").slice(0, 10).map((line) => (<text fg={theme.muted}>{line}</text>))}
              </box>) : entry.role === "queue" ? (<box flexDirection="column" paddingLeft={1}>
                {entry.content.split("\n").map((line) => (<text fg={theme.muted} attributes={TextAttributes.BOLD}>
                    {line}
                  </text>))}
              </box>) : (<text fg={theme.error}>{entry.content}</text>)}
          </box>)}
      </For>
    </scrollbox>);
}
