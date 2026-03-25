import clipboardy from "clipboardy";
export var Clipboard;
(function (Clipboard) {
    async function copy(text) {
        await clipboardy.write(text);
    }
    Clipboard.copy = copy;
})(Clipboard || (Clipboard = {}));
