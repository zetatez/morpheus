import { Clipboard } from "./clipboard";
export var Selection;
(function (Selection) {
    function copy(renderer) {
        const text = renderer.getSelection()?.getSelectedText();
        if (!text)
            return false;
        Clipboard.copy(text).catch(() => { });
        renderer.clearSelection();
        return true;
    }
    Selection.copy = copy;
})(Selection || (Selection = {}));
