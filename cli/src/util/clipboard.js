import clipboardy from "clipboardy";
function writeOsc52(text) {
    if (!process.stdout.isTTY)
        return;
    const base64 = Buffer.from(text).toString("base64");
    const osc52 = `\x1b]52;c;${base64}\x07`;
    const passthrough = process.env.TMUX || process.env.STY;
    const sequence = passthrough ? `\x1bPtmux;\x1b${osc52}\x1b\\` : osc52;
    process.stdout.write(sequence);
}
export var Clipboard;
(function (Clipboard) {
    async function copy(text) {
        writeOsc52(text);
        await clipboardy.write(text);
    }
    Clipboard.copy = copy;
})(Clipboard || (Clipboard = {}));
