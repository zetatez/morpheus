import yargs from "yargs";
import { hideBin } from "yargs/helpers";
import { render } from "@opentui/solid";
import { App } from "./app";
import { Clipboard } from "./util/clipboard";
const argv = yargs(hideBin(process.argv))
    .option("url", {
    type: "string",
    default: process.env.DARWIN_API_URL ?? "http://localhost:8080",
    describe: "BruteCode API base URL",
})
    .option("session", {
    type: "string",
    describe: "Session ID",
})
    .option("prompt", {
    type: "string",
    describe: "Initial prompt to submit",
})
    .option("debug-stream", {
    type: "boolean",
    default: process.env.MORPHEUS_CLI_DEBUG_STREAM === "1",
    describe: "Log stream events and rendered entries to stderr",
})
    .parseSync();
const debugStream = argv.debugStream ?? process.env.MORPHEUS_CLI_DEBUG_STREAM === "1";
const session = argv.session ?? formatSessionID();
function formatSessionID() {
    const now = new Date();
    const pad = (value, size = 2) => String(value).padStart(size, "0");
    const date = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}`;
    const time = `${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
    const ms = pad(now.getMilliseconds(), 3);
    return `${date}-${time}-${ms}`;
}
render(() => <App apiUrl={argv.url} sessionID={session} initialPrompt={argv.prompt} debugStream={debugStream}/>, {
    targetFps: 60,
    exitOnCtrlC: true,
    autoFocus: false,
    consoleOptions: {
        keyBindings: [{ name: "y", ctrl: true, action: "copy-selection" }],
        onCopySelection: (text) => {
            Clipboard.copy(text).catch(() => { });
        },
    },
});
