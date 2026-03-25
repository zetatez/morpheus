import { Clipboard } from "./clipboard";
function stripAnsi(text) {
    return text
        .replace(/\x1b\[[0-9;]*[a-zA-Z=<?>]*/g, "")
        .replace(/\x1b\][^\x07]*\x07/g, "")
        .replace(/\x1b[()][AB012]/g, "");
}
export var Selection;
(function (Selection) {
    async function copy(renderer) {
        const text = renderer.getSelection()?.getSelectedText();
        if (!text)
            return false;
        await Clipboard.copy(stripAnsi(text));
        renderer.clearSelection();
        return true;
    }
    Selection.copy = copy;
})(Selection || (Selection = {}));
