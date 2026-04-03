import { createContext, useContext, createMemo } from "solid-js";
import { createStore } from "solid-js/store";
const initialStore = {
    apiUrl: "",
    entries: [],
    input: "",
    busy: false,
    sessionID: "",
    agentMode: "build",
    inputHistory: [],
    historyIndex: null,
    historyDraft: "",
    modal: null,
    modalTitle: "",
    modalHint: "",
    modalQuery: "",
    modalItems: [],
    runsNextCursor: null,
    runsStatusFilter: "",
    activeRunBanner: null,
    activeTodos: [],
    activeStreamToken: null,
    runTimeline: null,
    runTimelineID: null,
    modalError: "",
    notification: null,
    pendingModel: null,
    modalSelected: 0,
    modalInput: "",
    confirmPayload: null,
    lastConfirmID: null,
    pendingConfirmation: null,
    expandedToolEntries: [],
    queuedRequests: [],
    monitorActive: false,
    attachments: [],
    escapePressed: false,
    isTyping: false,
    serverMetrics: null,
    currentModel: "",
    todoPulse: false,
};
export const AppContext = createContext();
export function useApp() {
    const ctx = useContext(AppContext);
    if (!ctx)
        throw new Error("useApp must be used within AppProvider");
    return ctx;
}
export const AppProvider = (props) => {
    const [store, setStore] = createStore({
        ...initialStore,
        apiUrl: props.apiUrl,
        sessionID: props.sessionID,
    });
    const client = createMemo(() => {
        const { createClient } = require("../api");
        return createClient(store.apiUrl);
    });
    const terminal = { width: 80, height: 24 };
    const expandedSet = new Set();
    const isToolExpanded = (id) => expandedSet.has(id);
    const toggleToolExpanded = (id) => {
        if (expandedSet.has(id)) {
            expandedSet.delete(id);
        }
        else {
            expandedSet.add(id);
        }
    };
    const value = {
        store,
        setStore,
        get client() { return client(); },
        terminal,
        isToolExpanded,
        toggleToolExpanded,
    };
    return (<AppContext.Provider value={value}>
      {props.children}
    </AppContext.Provider>);
};
