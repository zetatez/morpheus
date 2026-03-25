import { createSignal, onCleanup } from "solid-js";
export const TextAttributes = {
    NONE: 0,
    BOLD: 1,
    ITALIC: 3,
    UNDERLINE: 4,
    STRIKETHROUGH: 9,
};
let globalRenderer = null;
function getTerminalSize() {
    try {
        return { width: process.stdout.columns || 80, height: process.stdout.rows || 40 };
    }
    catch {
        return { width: 80, height: 40 };
    }
}
function moveCursor(row, col) {
    process.stdout.write(`\x1b[${row};${col}H`);
}
function clearScreen() {
    process.stdout.write("\x1b[2J\x1b[H");
}
function hideCursor() {
    process.stdout.write("\x1b[?25l");
}
function showCursor() {
    process.stdout.write("\x1b[?25h");
}
function eraseLine() {
    process.stdout.write("\x1b[2K");
}
function resetAttributes() {
    process.stdout.write("\x1b[0m");
}
function setForeground(color) {
    if (color === undefined)
        return;
    if (typeof color === "number") {
        process.stdout.write(`\x1b[38;5;${color}m`);
    }
    else if (color.startsWith("#")) {
        const r = parseInt(color.slice(1, 3), 16);
        const g = parseInt(color.slice(3, 5), 16);
        const b = parseInt(color.slice(5, 7), 16);
        process.stdout.write(`\x1b[38;2;${r};${g};${b}m`);
    }
    else {
        process.stdout.write(`\x1b[38;5;${color}m`);
    }
}
function setBackground(color) {
    if (color === undefined)
        return;
    if (typeof color === "number") {
        process.stdout.write(`\x1b[48;5;${color}m`);
    }
    else if (color.startsWith("#")) {
        const r = parseInt(color.slice(1, 3), 16);
        const g = parseInt(color.slice(3, 5), 16);
        const b = parseInt(color.slice(5, 7), 16);
        process.stdout.write(`\x1b[48;2;${r};${g};${b}m`);
    }
    else {
        process.stdout.write(`\x1b[48;5;${color}m`);
    }
}
function setBold(on) {
    process.stdout.write(on ? "\x1b[1m" : "\x1b[22m");
}
function setItalic(on) {
    process.stdout.write(on ? "\x1b[3m" : "\x1b[23m");
}
function setUnderline(on) {
    process.stdout.write(on ? "\x1b[4m" : "\x1b[24m");
}
export class TerminalRenderer {
    constructor(options) {
        this.eventListeners = new Map();
        this.renderCallback = null;
        this.running = false;
        this.suspended = false;
        this.pendingRender = false;
        this.exitOnCtrlC = true;
        this.consoleOptions = {};
        this.exitOnCtrlC = options?.exitOnCtrlC ?? true;
        this.consoleOptions = options?.consoleOptions ?? {};
    }
    requestRender() {
        if (this.suspended)
            return;
        this.pendingRender = true;
        if (this.renderCallback && !this.running) {
            this.running = true;
            queueMicrotask(() => {
                this.running = false;
                if (this.pendingRender && this.renderCallback) {
                    this.pendingRender = false;
                    this.renderCallback();
                }
            });
        }
    }
    suspend() {
        this.suspended = true;
        showCursor();
    }
    resume() {
        this.suspended = false;
        hideCursor();
        this.requestRender();
    }
    on(event, cb) {
        if (!this.eventListeners.has(event)) {
            this.eventListeners.set(event, new Set());
        }
        this.eventListeners.get(event).add(cb);
    }
    off(event, cb) {
        const listeners = this.eventListeners.get(event);
        if (listeners) {
            listeners.delete(cb);
        }
    }
    emit(event, ...args) {
        const listeners = this.eventListeners.get(event);
        if (listeners) {
            listeners.forEach((cb) => cb(...args));
        }
    }
    setRenderCallback(cb) {
        this.renderCallback = cb;
    }
    getConsoleOptions() {
        return this.consoleOptions;
    }
    cleanup() {
        showCursor();
        process.stdout.write("\x1b[0m");
        clearScreen();
        moveCursor(1, 1);
    }
    render(app) {
        hideCursor();
        clearScreen();
        this.setupInputHandlers();
        this.setupSignalHandlers();
        const frameTime = 1000 / 60;
        let lastFrame = 0;
        const renderLoop = (timestamp) => {
            if (timestamp - lastFrame >= frameTime) {
                lastFrame = timestamp;
                if (this.renderCallback) {
                    this.renderCallback();
                }
            }
            setTimeout(() => renderLoop(Date.now()), 16);
        };
        this.setRenderCallback(() => {
            const content = app();
            if (content) {
                this.renderContent(content);
            }
        });
        renderLoop(Date.now());
    }
    setupInputHandlers() {
        import("readline").then(({ createInterface }) => {
            const rl = createInterface({
                input: process.stdin,
                escapeCodeTimeout: 50,
            });
            rl.on("keypress", (_key, info) => {
                if (this.exitOnCtrlC && info.ctrl && info.name === "c") {
                    this.cleanup();
                    process.exit(0);
                    return;
                }
                const evt = {
                    name: info.name || "unknown",
                    ctrl: info.ctrl,
                    shift: info.shift,
                    alt: info.meta,
                    meta: info.meta,
                    option: info.meta,
                    preventDefault: () => { },
                };
                this.emit("key", evt);
                this.emit(`key:${info.name}`, evt);
            });
            rl.on("paste", (text) => {
                this.emit("paste", { text, preventDefault: () => { } });
            });
        });
    }
    setupSignalHandlers() {
        process.on("SIGWINCH", () => {
            this.emit("resize", getTerminalSize());
            this.requestRender();
        });
    }
    renderContent(root) {
        const dims = getTerminalSize();
        moveCursor(1, 1);
        process.stdout.write("\x1b[J");
        this.renderElement(root, 0, 0, dims.width, dims.height);
        process.stdout.write("\x1b[0m");
    }
    renderElement(el, x, y, width, height) {
        if (!el)
            return 0;
        const node = el;
        switch (node.type) {
            case "box":
                return this.renderBox(node, x, y, width, height);
            case "text":
                return this.renderText(node, x, y);
            case "scrollbox":
                return this.renderScrollbox(node, x, y, width, height);
            case "textarea":
                return this.renderTextarea(node, x, y, width, height);
            case "input":
                return this.renderInput(node, x, y, width);
            case "fragment":
                return this.renderFragment(node, x, y, width, height);
            default:
                return 0;
        }
    }
    renderBox(el, x, y, width, height) {
        const props = el.props || {};
        const { flexDirection = "row", backgroundColor, paddingLeft = 0, paddingRight = 0, paddingTop = 0, paddingBottom = 0, } = props;
        if (backgroundColor != null) {
            setBackground(backgroundColor);
            moveCursor(y + 1, x + 1);
            for (let i = 0; i < height; i++) {
                process.stdout.write(" ".repeat(width));
                if (i < height - 1)
                    moveCursor(y + i + 2, x + 1);
            }
            resetAttributes();
        }
        const contentWidth = Math.max(1, width - paddingLeft - paddingRight);
        const contentHeight = Math.max(1, height - paddingTop - paddingBottom);
        let offsetX = x + paddingLeft;
        let offsetY = y + paddingTop;
        const children = props.children;
        if (children) {
            const childArray = Array.isArray(children) ? children : [children];
            for (const child of childArray) {
                if (!child)
                    continue;
                if (typeof child === "string" || typeof child === "number") {
                    moveCursor(offsetY + 1, offsetX + 1);
                    process.stdout.write(String(child));
                    if (flexDirection === "row") {
                        offsetX += String(child).length;
                    }
                    else {
                        offsetY += 1;
                    }
                    continue;
                }
                if (flexDirection === "row") {
                    const childWidth = this.getNodeWidth(child, contentWidth);
                    this.renderElement(child, offsetX, offsetY, childWidth, contentHeight);
                    offsetX += childWidth;
                }
                else {
                    const childHeight = this.getNodeHeight(child, contentHeight);
                    this.renderElement(child, offsetX, offsetY, contentWidth, childHeight);
                    offsetY += childHeight;
                }
            }
        }
        return flexDirection === "row" ? height : width;
    }
    renderText(el, x, y) {
        const props = el.props || {};
        const fg = props.fg;
        const attributes = props.attributes;
        const text = this.extractText(el);
        if (!text)
            return 0;
        moveCursor(y + 1, x + 1);
        if (fg != null)
            setForeground(fg);
        if (attributes === TextAttributes.BOLD)
            setBold(true);
        else if (attributes === TextAttributes.ITALIC)
            setItalic(true);
        else if (attributes === TextAttributes.UNDERLINE)
            setUnderline(true);
        process.stdout.write(text);
        resetAttributes();
        return 1;
    }
    renderScrollbox(el, x, y, width, height) {
        const props = el.props || {};
        const paddingLeft = props.paddingLeft || 0;
        const paddingRight = props.paddingRight || 0;
        const paddingTop = props.paddingTop || 0;
        const contentWidth = Math.max(1, width - paddingLeft - paddingRight);
        const contentHeight = Math.max(1, height - paddingTop);
        let offsetY = y + paddingTop;
        const children = props.children;
        if (children) {
            const childArray = Array.isArray(children) ? children : [children];
            for (const child of childArray) {
                if (!child || typeof child === "string" || typeof child === "number")
                    continue;
                const childHeight = this.getNodeHeight(child, contentHeight);
                if (offsetY + childHeight <= y + height) {
                    this.renderElement(child, x + paddingLeft, offsetY, contentWidth, childHeight);
                    offsetY += childHeight;
                }
            }
        }
        return contentHeight;
    }
    renderTextarea(el, x, y, width, height) {
        const props = el.props || {};
        const focused = props.focused ?? true;
        const textColor = props.textColor;
        const focusedTextColor = props.focusedTextColor;
        const placeholder = props.placeholder;
        const text = props.value || "";
        const displayText = text || placeholder || "";
        moveCursor(y + height, x + 1);
        eraseLine();
        if (displayText) {
            const color = focused ? textColor : focusedTextColor;
            if (color != null)
                setForeground(color);
            process.stdout.write(displayText);
            resetAttributes();
        }
        return height;
    }
    renderInput(el, x, y, width) {
        const props = el.props || {};
        const focused = props.focused ?? true;
        const textColor = props.textColor;
        const focusedTextColor = props.focusedTextColor;
        const placeholder = props.placeholder;
        const value = props.value;
        const text = value || "";
        const displayText = text || placeholder || "";
        moveCursor(y + 1, x + 1);
        eraseLine();
        if (displayText) {
            const color = focused ? textColor : focusedTextColor;
            if (color != null)
                setForeground(color);
            process.stdout.write(displayText);
            resetAttributes();
        }
        return 1;
    }
    renderFragment(el, x, y, width, height) {
        const children = el.props?.children;
        if (!children)
            return 0;
        let maxY = y;
        const childArray = Array.isArray(children) ? children : [children];
        for (const child of childArray) {
            if (!child || typeof child === "string" || typeof child === "number") {
                maxY += 1;
                continue;
            }
            if (child.props?.flexDirection === "row") {
                maxY = y;
            }
            else {
                maxY += this.getNodeHeight(child, height);
            }
        }
        return Math.max(1, maxY - y);
    }
    extractText(el) {
        const props = el.props || {};
        const children = props.children;
        if (!children)
            return "";
        const childArray = Array.isArray(children) ? children : [children];
        return childArray.map((c) => {
            if (typeof c === "string")
                return c;
            if (typeof c === "number")
                return String(c);
            if (c && typeof c === "object" && "props" in c) {
                return this.extractText(c);
            }
            return "";
        }).join("");
    }
    getNodeWidth(node, defaultWidth) {
        const props = node.props || {};
        const w = props.width;
        return typeof w === "number" ? w : defaultWidth;
    }
    getNodeHeight(node, defaultHeight) {
        const props = node.props || {};
        const h = props.height;
        return typeof h === "number" ? h : defaultHeight;
    }
}
export function createTerminalRenderer(options) {
    return new TerminalRenderer(options);
}
export function useRenderer() {
    if (!globalRenderer) {
        globalRenderer = new TerminalRenderer();
    }
    return globalRenderer;
}
export function useTerminalDimensions() {
    const [dimensions, setDimensions] = createSignal(getTerminalSize());
    if (globalRenderer) {
        globalRenderer.on("resize", (dims) => {
            setDimensions(dims);
        });
    }
    return dimensions;
}
export function createInputRenderable(initialValue = "") {
    let value = initialValue;
    let focused = true;
    return {
        get value() { return value; },
        get focused() { return focused; },
        focus() { focused = true; },
    };
}
export function createTextareaRenderable(initialText = "") {
    let text = initialText;
    let cursor = 0;
    let isFocused = true;
    return {
        get plainText() { return text; },
        get cursorOffset() { return cursor; },
        get focused() { return isFocused; },
        setText(newText) { text = newText; },
        clear() { text = ""; cursor = 0; },
        focus() { isFocused = true; },
    };
}
export function createScrollBoxRenderable() {
    let scrollOffset = 0;
    return {
        scrollBy(amount) {
            scrollOffset += amount;
        },
    };
}
export function useKeyboard(handler) {
    if (!globalRenderer)
        return;
    const handlerWrapper = (evt) => {
        handler(evt);
    };
    globalRenderer.on("key", handlerWrapper);
    onCleanup(() => {
        globalRenderer?.off("key", handlerWrapper);
    });
}
export function h(type, props, ...children) {
    return {
        type,
        props: props || {},
        children: children.length > 0 ? children : undefined,
    };
}
export function Fragment(props) {
    return h("fragment", null, ...(props.children || []));
}
