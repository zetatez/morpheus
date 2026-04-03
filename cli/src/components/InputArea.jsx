import { theme } from "../theme";
export function InputArea(props) {
    return (<box flexDirection="column">
      {props.attachments.length > 0 && (<box paddingBottom={1} paddingLeft={1}>
          <text fg={theme.muted}>Attachments: </text>
          {props.attachments.map((att, i) => (<text fg={theme.primary}>{att.path ?? att.url}{i < props.attachments.length - 1 ? ", " : ""}</text>))}
        </box>)}
      <textarea ref={(val) => props.setTextareaRef(val)} onInput={(value) => props.onInput(value)} flexGrow={1} onKeyDown={props.onKeyDown} onPaste={props.onPaste} placeholder={props.escapePressed
            ? "Press ESC again to cancel task"
            : props.modalHint || (props.busy ? "Task running. You can keep typing; new requests will be queued..." : "Ask anything...")} textColor={theme.text} cursorColor={theme.primary} focusedTextColor={theme.text} focusedCursorColor={theme.primary} backgroundColor={theme.background}/>
    </box>);
}
