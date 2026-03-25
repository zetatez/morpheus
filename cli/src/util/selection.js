import { Clipboard } from "./clipboard";
export var Selection;
(function (Selection) {
    async function copy(renderer) {
        const text = renderer.getSelection()?.getSelectedText();
        if (!text)
            return false;
        await Clipboard.copy(text);
        renderer.clearSelection();
        return true;
    }
    Selection.copy = copy;
})(Selection || (Selection = {}));
