import { theme } from "../theme";
import { formatToken } from "./shared";
export function StatusBar(props) {
    return (<box flexDirection="row" paddingLeft={2} paddingRight={2} paddingTop={1} paddingBottom={1}>
      <text fg={theme.muted}>{props.apiUrl}</text>
      <box flexGrow={1}/>
      <text fg={theme.muted}>session {props.sessionID}</text>
      <box paddingLeft={3}/>
      <text fg={theme.muted}>↑ {formatToken(props.serverMetrics?.input_tokens ?? 0)}</text>
      <box paddingLeft={2}/>
      <text fg={theme.muted}>↓ {formatToken(props.serverMetrics?.output_tokens ?? 0)}</text>
    </box>);
}
