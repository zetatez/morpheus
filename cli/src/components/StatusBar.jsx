import { theme } from "../theme";
export function StatusBar(props) {
    return (<box flexDirection="row" paddingRight={2} paddingBottom={1}>
      <text fg={props.agentModeColor}>{props.agentModeIcon} {props.agentModeLabel}</text>
      <text fg={theme.muted}> · {props.model}{props.busy ? " · Running" : ""}{props.activeRunBanner ? ` · ${props.activeRunBanner}` : ""}</text>
      <box flexGrow={1}/>
      <text fg={theme.success}>{props.notification}</text>
    </box>);
}