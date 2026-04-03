import { TextAttributes } from "@opentui/core";

const MONOKAI = {
  keyword: "#f92672",
  built_in: "#66d9ef",
  type: "#66d9ef",
  literal: "#ae81ff",
  number: "#ae81ff",
  string: "#e6db74",
  comment: "#75715e",
  variable: "#f8f8f2",
  function: "#a6e22e",
  class: "#66d9ef",
  operator: "#f92672",
  punctuation: "#89ddff",
  attribute: "#a6e22e",
  constant: "#ae81ff",
  regex: "#e6db74",
  escape: "#ae81ff",
};

const LANGUAGE_COLORS = {
  js: "#f7df1e",
  javascript: "#f7df1e",
  ts: "#3178c6",
  typescript: "#3178c6",
  py: "#3572A5",
  python: "#3572A5",
  go: "#00ADD8",
  golang: "#00ADD8",
  rust: "#dea584",
  rs: "#dea584",
  java: "#b07219",
  bash: "#89e051",
  sh: "#89e051",
  shell: "#89e051",
  zsh: "#89e051",
  ruby: "#701516",
  rb: "#701516",
  php: "#4F5D95",
  c: "#555555",
  cpp: "#f34b7d",
  "c++": "#f34b7d",
  cs: "#178600",
  "c#": "#178600",
  swift: "#F05138",
  kotlin: "#A97BFF",
  scala: "#dc322f",
  dockerfile: "#384d54",
  yaml: "#cb171e",
  yml: "#cb171e",
  json: "#6ec1e4",
  xml: "#e37933",
  html: "#e34c26",
  css: "#563d7c",
  sql: "#e38c00",
  md: "#083fa1",
  markdown: "#083fa1",
  diff: "#ff7b72",
  patch: "#ff7b72",
};

const LANGUAGE_LABELS = {
  js: "JavaScript",
  javascript: "JavaScript",
  ts: "TypeScript",
  typescript: "TypeScript",
  py: "Python",
  python: "Python",
  go: "Go",
  golang: "Go",
  rust: "Rust",
  rs: "Rust",
  java: "Java",
  bash: "Bash",
  sh: "Shell",
  shell: "Shell",
  zsh: "Zsh",
  ruby: "Ruby",
  rb: "Ruby",
  php: "PHP",
  c: "C",
  cpp: "C++",
  "c++": "C++",
  cs: "C#",
  "c#": "C#",
  swift: "Swift",
  kotlin: "Kotlin",
  scala: "Scala",
  dockerfile: "Dockerfile",
  yaml: "YAML",
  yml: "YAML",
  json: "JSON",
  xml: "XML",
  html: "HTML",
  css: "CSS",
  sql: "SQL",
  markdown: "Markdown",
  md: "Markdown",
  diff: "Diff",
  patch: "Patch",
};

function getLangColor(lang) {
  return LANGUAGE_COLORS[lang.toLowerCase()] || "#6ec1e4";
}

function getLangLabel(lang) {
  return LANGUAGE_LABELS[lang.toLowerCase()] || lang.toUpperCase();
}

const LANG_KEYWORDS = {
  bash: ["if", "then", "else", "elif", "fi", "case", "esac", "for", "while", "do", "done", "in", "function", "return", "local", "export", "echo", "cd", "ls", "mkdir", "rm", "cp", "mv", "cat", "grep", "sed", "awk", "find", "chmod", "chown", "sudo", "apt", "yum", "npm", "yarn", "pnpm", "git", "docker", "kubectl", "curl", "wget", "ssh", "scp", "rsync", "tar", "zip", "unzip", "source", "alias", "unalias", "exit", "export", "readonly", "declare", "typeset", "unset", "shift", "set", "trap", "wait", "exec", "eval", "true", "false"],
  python: ["def", "class", "return", "if", "elif", "else", "for", "while", "try", "except", "finally", "with", "as", "import", "from", "raise", "pass", "break", "continue", "and", "or", "not", "in", "is", "lambda", "yield", "global", "nonlocal", "assert", "async", "await", "None", "True", "False", "self", "cls"],
  javascript: ["const", "let", "var", "function", "return", "if", "else", "for", "while", "do", "switch", "case", "break", "continue", "default", "try", "catch", "finally", "throw", "new", "delete", "typeof", "instanceof", "void", "this", "class", "extends", "super", "import", "export", "from", "as", "async", "await", "yield", "static", "get", "set", "of", "in", "true", "false", "null", "undefined", "NaN", "Infinity"],
  typescript: ["const", "let", "var", "function", "return", "if", "else", "for", "while", "do", "switch", "case", "break", "continue", "default", "try", "catch", "finally", "throw", "new", "delete", "typeof", "instanceof", "void", "this", "class", "extends", "super", "import", "export", "from", "as", "async", "await", "yield", "static", "get", "set", "of", "in", "true", "false", "null", "undefined", "NaN", "Infinity", "interface", "type", "enum", "implements", "private", "public", "protected", "readonly", "abstract", "as", "keyof", "typeof", "namespace", "module", "declare", "infer", "extends", "partial", "required", "omit", "pick", "exclude"],
  go: ["func", "return", "if", "else", "for", "range", "switch", "case", "default", "break", "continue", "fallthrough", "goto", "defer", "go", "select", "chan", "type", "struct", "interface", "map", "const", "var", "package", "import", "nil", "true", "false", "iota", "len", "cap", "make", "new", "append", "copy", "delete", "close", "panic", "recover", "print", "println", "error", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "complex64", "complex128", "bool", "byte", "rune", "any"],
  rust: ["fn", "let", "mut", "const", "static", "if", "else", "match", "for", "while", "loop", "break", "continue", "return", "struct", "enum", "impl", "trait", "type", "where", "pub", "mod", "use", "crate", "self", "super", "as", "ref", "move", "async", "await", "dyn", "unsafe", "extern", "true", "false", "Some", "None", "Ok", "Err", "impl", "trait", "struct", "enum", "type", "mod", "pub", "crate", "self", "super"],
  java: ["abstract", "assert", "boolean", "break", "byte", "case", "catch", "char", "class", "const", "continue", "default", "do", "double", "else", "enum", "extends", "final", "finally", "float", "for", "goto", "if", "implements", "import", "instanceof", "int", "interface", "long", "native", "new", "package", "private", "protected", "public", "return", "short", "static", "strictfp", "super", "switch", "synchronized", "this", "throw", "throws", "transient", "try", "void", "volatile", "while", "true", "false", "null"],
  c: ["auto", "break", "case", "char", "const", "continue", "default", "do", "double", "else", "enum", "extern", "float", "for", "goto", "if", "inline", "int", "long", "register", "restrict", "return", "short", "signed", "sizeof", "static", "struct", "switch", "typedef", "union", "unsigned", "void", "volatile", "while", "_Bool", "_Complex", "_Imaginary", "NULL", "true", "false", "printf", "scanf", "malloc", "free", "sizeof"],
  cpp: ["alignas", "alignof", "and", "and_eq", "asm", "auto", "bitand", "bitor", "bool", "break", "case", "catch", "char", "char16_t", "char32_t", "class", "compl", "const", "constexpr", "const_cast", "continue", "decltype", "default", "delete", "do", "double", "dynamic_cast", "else", "enum", "explicit", "export", "extern", "false", "float", "for", "friend", "goto", "if", "inline", "int", "long", "mutable", "namespace", "new", "noexcept", "not", "not_eq", "nullptr", "operator", "or", "or_eq", "private", "protected", "public", "register", "reinterpret_cast", "return", "short", "signed", "sizeof", "static", "static_assert", "static_cast", "struct", "switch", "template", "this", "thread_local", "throw", "true", "try", "typedef", "typeid", "typename", "union", "unsigned", "using", "virtual", "void", "volatile", "wchar_t", "while", "xor", "xor_eq", "std", "string", "vector", "map", "set", "cout", "cin", "endl", "printf", "scanf", "malloc", "free"],
  sql: ["SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "IS", "NULL", "LIKE", "BETWEEN", "JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "FULL", "ON", "AS", "ORDER", "BY", "GROUP", "HAVING", "LIMIT", "OFFSET", "INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE", "CREATE", "TABLE", "INDEX", "VIEW", "DROP", "ALTER", "ADD", "COLUMN", "PRIMARY", "KEY", "FOREIGN", "REFERENCES", "UNIQUE", "CHECK", "DEFAULT", "CONSTRAINT", "CASCADE", "DISTINCT", "ALL", "EXISTS", "CASE", "WHEN", "THEN", "ELSE", "END", "UNION", "INTERSECT", "EXCEPT", "ASC", "DESC", "TRUE", "FALSE", "INTEGER", "INT", "BIGINT", "SMALLINT", "REAL", "DOUBLE", "DECIMAL", "NUMERIC", "CHAR", "VARCHAR", "TEXT", "DATE", "TIME", "TIMESTAMP", "BLOB"],
  json: ["true", "false", "null"],
  yaml: ["true", "false", "null", "yes", "no", "on", "off", "~"],
  dockerfile: ["FROM", "RUN", "CMD", "LABEL", "EXPOSE", "ENV", "ADD", "COPY", "ENTRYPOINT", "VOLUME", "USER", "WORKDIR", "ARG", "ONBUILD", "STOPSIGNAL", "HEALTHCHECK", "MAINTAINER", "SHELL", "COMMENT", "ENV", "EXPOSE", "ARG", "LABEL"],
  markdown: [],
  html: ["html", "head", "body", "div", "span", "p", "a", "img", "ul", "ol", "li", "table", "tr", "td", "th", "form", "input", "button", "select", "option", "textarea", "h1", "h2", "h3", "h4", "h5", "h6", "header", "footer", "nav", "section", "article", "aside", "main", "script", "style", "link", "meta", "title", "class", "id", "src", "href", "alt", "type", "name", "value"],
  css: ["important", "inherit", "initial", "unset", "none", "auto", "normal", "bold", "italic", "underline", "block", "inline", "flex", "grid", "relative", "absolute", "fixed", "sticky", "static", "hidden", "visible", "scroll", "transparent", "solid", "dashed", "dotted", "center", "left", "right", "top", "bottom", "wrap", "nowrap", "color", "background", "margin", "padding", "border", "width", "height", "display", "position", "font", "text", "flex", "grid"],
};

const LANG_BUILTINS = {
  python: ["print", "len", "range", "str", "int", "float", "list", "dict", "set", "tuple", "bool", "type", "isinstance", "hasattr", "getattr", "setattr", "open", "input", "map", "filter", "zip", "enumerate", "sorted", "reversed", "min", "max", "sum", "abs", "round", "pow", "divmod", "hex", "oct", "bin", "ord", "chr", "repr", "format", "slice", "super", "self", "cls", "object", "Exception"],
  javascript: ["console", "window", "document", "Math", "JSON", "Array", "Object", "String", "Number", "Boolean", "Function", "Symbol", "Map", "Set", "WeakMap", "WeakSet", "Promise", "Proxy", "Reflect", "Date", "RegExp", "Error", "parseInt", "parseFloat", "isNaN", "isFinite", "encodeURI", "decodeURI", "setTimeout", "setInterval", "clearTimeout", "clearInterval", "fetch", "require", "module", "exports", "process", "global", "Buffer"],
  typescript: ["console", "window", "document", "Math", "JSON", "Array", "Object", "String", "Number", "Boolean", "Function", "Symbol", "Map", "Set", "Promise", "Date", "RegExp", "Error", "parseInt", "parseFloat", "fetch", "require", "module", "exports", "Partial", "Required", "Readonly", "Record", "Pick", "Omit", "Exclude", "Extract", "NonNullable", "ReturnType", "InstanceType", "Parameters"],
  go: ["append", "cap", "close", "complex", "copy", "delete", "imag", "len", "make", "new", "panic", "print", "println", "real", "recover", "fmt", "strings", "strconv", "os", "io", "bufio", "bytes", "path", "filepath", "encoding", "json", "xml", "http", "net", "flag", "log", "sort", "container", "list", "ring", "sync", "time", "context"],
  rust: ["println", "print", "eprintln", "eprint", "format", "panic", "assert", "assert_eq", "assert_ne", "todo", "unimplemented", "unreachable", "dbg", "vec", "string", "to_string", "into_iter", "iter", "next", "Some", "None", "Ok", "Err", "Box", "Rc", "Arc", "Cell", "RefCell", "Mutex"],
  java: ["System", "out", "println", "print", "String", "Integer", "Double", "Boolean", "Long", "Float", "Short", "Byte", "Character", "Object", "Class", "Math", "Thread", "Runnable", "Exception", "Error", "Throwable", "List", "ArrayList", "Map", "HashMap", "Set", "HashSet", "Queue", "Deque", "ArrayDeque", "Collections", "Arrays", "StringBuilder"],
};

const LANG_TYPES = {
  python: ["int", "str", "float", "bool", "list", "dict", "set", "tuple", "bytes", "object", "type", "Exception", "TypeError", "ValueError", "KeyError", "IndexError", "AttributeError"],
  javascript: ["Array", "Object", "String", "Number", "Boolean", "Function", "Symbol", "Map", "Set", "WeakMap", "WeakSet", "Promise", "Proxy", "Reflect", "Date", "RegExp", "Error", "ArrayBuffer", "DataView", "Int8Array", "Uint8Array", "Float32Array", "Float64Array"],
  typescript: ["Array", "Object", "String", "Number", "Boolean", "Function", "Symbol", "Map", "Set", "Promise", "Date", "RegExp", "Error", "Partial", "Required", "Readonly", "Record", "Pick", "Omit", "Exclude", "Extract", "any", "unknown", "never", "void", "null", "undefined", "string", "number", "boolean", "symbol", "bigint"],
  go: ["int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "complex64", "complex128", "bool", "byte", "rune", "string", "error", "any", "interface", "struct", "map", "slice", "chan"],
  rust: ["i8", "i16", "i32", "i64", "i128", "isize", "u8", "u16", "u32", "u64", "u128", "usize", "f32", "f64", "bool", "char", "str", "String", "Vec", "Box", "Option", "Result", "HashMap", "HashSet", "String", "Vec"],
  java: ["int", "char", "float", "double", "void", "long", "short", "byte", "boolean", "Integer", "Long", "Float", "Double", "Short", "Byte", "Character", "Boolean", "String", "Object", "Class", "List", "ArrayList", "Map", "HashMap", "Set", "HashSet"],
  c: ["int", "char", "float", "double", "void", "long", "short", "unsigned", "signed", "size_t", "ptrdiff_t", "int8_t", "int16_t", "int32_t", "int64_t", "uint8_t", "uint16_t", "uint32_t", "uint64_t", "bool", "FILE", "DIR", "NULL"],
};

function escapeRegex(str) {
  return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function createLanguageHighlighter(lang) {
  const keywords = LANG_KEYWORDS[lang] || [];
  const builtins = LANG_BUILTINS[lang] || [];
  const types = LANG_TYPES[lang] || [];

  const keywordSet = new Set(keywords.map(k => k.toLowerCase()));
  const builtinSet = new Set(builtins.map(b => b.toLowerCase()));
  const typeSet = new Set(types.map(t => t.toLowerCase()));

  const patterns = [];

  if (lang === "bash" || lang === "shell" || lang === "sh" || lang === "zsh") {
    patterns.push({ type: "comment", regex: /#.*$/gm });
    patterns.push({ type: "string", regex: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g });
    patterns.push({ type: "variable", regex: /\$[a-zA-Z_][a-zA-Z0-9_]*|\$\{[^}]+\}/g });
    patterns.push({ type: "function", regex: /\b[a-zA-Z_][a-zA-Z0-9]*(?=\s*\()/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.map(escapeRegex).join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b[0-9]+\b/g });
  } else if (lang === "python") {
    patterns.push({ type: "comment", regex: /#.*$/gm });
    patterns.push({ type: "string", regex: /f?"""[\s\S]*?"""|'''[\s\S]*?'''|@?r?"(?:[^"\\]|\\.)*"|@?r?'(?:[^'\\]|\\.)*'/g });
    patterns.push({ type: "string", regex: /\b[f][Rr]?(?:"""[\s\S]*?"""|'''[\s\S]*?'''|"[^"]*"|'[^']*')/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "builtin", regex: new RegExp(`\\b(?:${builtins.join("|")})\\b`, "g") });
    patterns.push({ type: "type", regex: new RegExp(`\\b(?:${types.join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b0[xX][0-9a-fA-F]+\b|\b0[oO][0-7]+\b|\b0[bB][01]+\b|\b\d+\.?\d*(?:[eE][+-]?\d+)?\b/g });
    patterns.push({ type: "decorator", regex: /@\w+/g });
    patterns.push({ type: "function", regex: /\b[a-zA-Z_][a-zA-Z0-9_]*(?=\s*\()/g });
  } else if (lang === "javascript" || lang === "js" || lang === "typescript" || lang === "ts") {
    patterns.push({ type: "comment", regex: /\/\/.*$|\/\*[\s\S]*?\*\//gm });
    patterns.push({ type: "string", regex: /`(?:[^`\\]|\\.)*`|"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "builtin", regex: new RegExp(`\\b(?:${builtins.join("|")})\\b`, "g") });
    patterns.push({ type: "type", regex: new RegExp(`\\b(?:${types.join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b0[xX][0-9a-fA-F]+\b|\b0[oO][0-7]+\b|\b0[bB][01]+\b|\b\d+\.?\d*(?:[eE][+-]?\d+)?\b/g });
    patterns.push({ type: "function", regex: /\b[A-Z][a-zA-Z0-9]*(?=\s*\.)|\b[a-zA-Z_][a-zA-Z0-9_]*(?=\s*\()/g });
    patterns.push({ type: "class", regex: /\b[A-Z][a-zA-Z0-9]+\b/g });
  } else if (lang === "go") {
    patterns.push({ type: "comment", regex: /\/\/.*$|\/\*[\s\S]*?\*\//gm });
    patterns.push({ type: "string", regex: /`(?:[^`\\]|\\.)*`|"(?:[^"\\]|\\.)*"/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "builtin", regex: new RegExp(`\\b(?:${builtins.join("|")})\\b`, "g") });
    patterns.push({ type: "type", regex: new RegExp(`\\b(?:${types.join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b0[xX][0-9a-fA-F]+\b|\b0[oO][0-7]+\b|\b0[bB][01]+\b|\b\d+\.?\d*(?:[eE][+-]?\d+)?\b|\b\d+i\b/g });
    patterns.push({ type: "function", regex: /\b[a-zA-Z_][a-zA-Z0-9_]*(?=\s*\()/g });
  } else if (lang === "rust") {
    patterns.push({ type: "comment", regex: /\/\/.*$|\/\*[\s\S]*?\*\//gm });
    patterns.push({ type: "string", regex: /"(?:[^"\\]|\\.)*"/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "builtin", regex: new RegExp(`\\b(?:${builtins.join("|")})\\b`, "g") });
    patterns.push({ type: "type", regex: new RegExp(`\\b(?:${types.join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b0[xX][0-9a-fA-F_]+\b|\b0[oO][0-7_]+\b|\b0[bB][01_]+\b|\b\d+\.?\d*(?:[eE][+-]?\d+)?(?:_?[fiu](?:8|16|32|64|size))?\b|\b\d+_?\d*:.*?(?=:)\b/g });
    patterns.push({ type: "function", regex: /\b[a-zA-Z_][a-zA-Z0-9_]*(?=\s*[!(<])?/g });
    patterns.push({ type: "macro", regex: /\b[a-zA-Z_][a-zA-Z0-9_]*!/g });
  } else if (lang === "java" || lang === "c" || lang === "cpp") {
    patterns.push({ type: "comment", regex: /\/\/.*$|\/\*[\s\S]*?\*\//gm });
    patterns.push({ type: "string", regex: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "builtin", regex: new RegExp(`\\b(?:${builtins.join("|")})\\b`, "g") });
    patterns.push({ type: "type", regex: new RegExp(`\\b(?:${types.join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b0[xX][0-9a-fA-F]+\b|\b0[oO][0-7]+\b|\b0[bB][01]+\b|\b\d+\.?\d*(?:[eE][+-]?\d+)?[fFdDlL]?\b/g });
    patterns.push({ type: "function", regex: /\b[a-zA-Z_][a-zA-Z0-9_]*(?=\s*\()/g });
    patterns.push({ type: "class", regex: /\b[A-Z][a-zA-Z0-9_]+\b/g });
  } else if (lang === "sql") {
    patterns.push({ type: "comment", regex: /--.*$|\/\*[\s\S]*?\*\//gm });
    patterns.push({ type: "string", regex: /'(?:[^']|'')*'/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b\d+\.?\d*\b/g });
  } else if (lang === "json") {
    patterns.push({ type: "string", regex: /"(?:[^"\\]|\\.)*"/g });
    patterns.push({ type: "keyword", regex: /\b(?:true|false|null)\b/g });
    patterns.push({ type: "number", regex: /-?\b\d+\.?\d*(?:[eE][+-]?\d+)?\b/g });
  } else if (lang === "yaml") {
    patterns.push({ type: "comment", regex: /#.*$/gm });
    patterns.push({ type: "string", regex: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b\d+\.?\d*\b/g });
    patterns.push({ type: "attr", regex: /^[\w-]+(?=:)/gm });
  } else if (lang === "html" || lang === "xml") {
    patterns.push({ type: "comment", regex: /<!--[\s\S]*?-->/g });
    patterns.push({ type: "tag", regex: /<\/?[\w-]+/g });
    patterns.push({ type: "attr", regex: /\b[\w-]+(?==)/g });
    patterns.push({ type: "string", regex: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g });
  } else if (lang === "css") {
    patterns.push({ type: "comment", regex: /\/\*[\s\S]*?\*\//g });
    patterns.push({ type: "selector", regex: /[.#]?[a-zA-Z_-][a-zA-Z0-9_-]*(?=\s*\{)/g });
    patterns.push({ type: "property", regex: /[a-zA-Z-]+(?=\s*:)/g });
    patterns.push({ type: "string", regex: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b\d+\.?\d*(px|em|rem|%|vh|vw|pt|cm|mm|in)?\b/g });
  } else if (lang === "dockerfile") {
    patterns.push({ type: "comment", regex: /#.*$/gm });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "string", regex: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g });
    patterns.push({ type: "variable", regex: /\$\{?[a-zA-Z_][a-zA-Z0-9_]*\}?/g });
    patterns.push({ type: "number", regex: /\b\d+\b/g });
  } else {
    patterns.push({ type: "comment", regex: /\/\/.*$|\/\*[\s\S]*?\/|#.*$/gm });
    patterns.push({ type: "string", regex: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g });
    patterns.push({ type: "keyword", regex: new RegExp(`\\b(?:${keywords.join("|")})\\b`, "g") });
    patterns.push({ type: "number", regex: /\b\d+\.?\d*\b/g });
  }

  return function highlight(code) {
    const tokens = [];
    let remaining = code;
    let position = 0;

    while (remaining.length > 0) {
      let earliest = null;
      let earliestType = null;
      let earliestLength = 0;

      for (const pattern of patterns) {
        pattern.regex.lastIndex = 0;
        const match = pattern.regex.exec(remaining);
        if (match && match.index === 0) {
          if (!earliest || match[0].length > earliestLength) {
            earliest = match[0];
            earliestType = pattern.type;
            earliestLength = match[0].length;
          }
        }
      }

      if (earliest && earliestType) {
        if (earliestLength > 0) {
          tokens.push({ type: earliestType, value: earliest });
          remaining = remaining.slice(earliestLength);
          position += earliestLength;
        }
      } else {
        tokens.push({ type: "text", value: remaining[0] });
        remaining = remaining.slice(1);
        position++;
      }
    }

    return tokens;
  };
}

function tokenToFg(token) {
  switch (token.type) {
    case "keyword": return MONOKAI.keyword;
    case "built_in": return MONOKAI.built_in;
    case "type": return MONOKAI.type;
    case "literal": return MONOKAI.literal;
    case "number": return MONOKAI.number;
    case "string": return MONOKAI.string;
    case "comment": return MONOKAI.comment;
    case "variable": return MONOKAI.variable;
    case "function": return MONOKAI.function;
    case "class": return MONOKAI.class;
    case "operator": return MONOKAI.operator;
    case "punctuation": return MONOKAI.punctuation;
    case "attribute": return MONOKAI.attribute;
    case "constant": return MONOKAI.constant;
    case "regex": return MONOKAI.regex;
    case "escape": return MONOKAI.escape;
    case "decorator": return MONOKAI.function;
    case "macro": return MONOKAI.function;
    case "selector": return MONOKAI.keyword;
    case "property": return MONOKAI.attribute;
    case "tag": return MONOKAI.keyword;
    default: return "#f8f8f2";
  }
}

function highlightCodeFallback(code, lang) {
  const highlighter = createLanguageHighlighter(lang);
  const tokens = highlighter(code);
  return tokens.map(t => ({ text: t.value, fg: tokenToFg(t) }));
}

function normalizeThinkingLine(trimmed) {
  if (trimmed.startsWith("Thinking:")) return trimmed;
  if (trimmed.toLowerCase().startsWith("thinking:")) {
    const rest = trimmed.slice("thinking:".length).trimStart();
    return rest ? `Thinking: ${rest}` : "Thinking:";
  }
  return null;
}

function normalizeSummaryLine(trimmed) {
  if (trimmed.startsWith("Summary:")) return trimmed;
  if (trimmed.toLowerCase().startsWith("summary:")) {
    const rest = trimmed.slice("summary:".length).trimStart();
    return rest ? `Summary: ${rest}` : "Summary:";
  }
  return null;
}

function isThinkingCommand(trimmed) {
  if (trimmed.startsWith("$ ") || trimmed.startsWith("$")) return true;
  if (trimmed.startsWith("* ") || trimmed.startsWith("- ")) return true;
  if (trimmed.startsWith("-> ")) return true;
  return false;
}

function renderTable(table, colors) {
  if (table.rows.length === 0) return [];
  const out = [];
  const totalWidth = table.colWidths.reduce((a, b) => a + b, 0) + table.colCount * 3 + 1;
  const divider = "─".repeat(totalWidth - 2);
  const padRow = (cells, isHeader) => {
    const padded = cells.map((cell, i) => {
      const width = table.colWidths[i] ?? cell.length;
      return cell.padEnd(width);
    });
    return "│ " + padded.join(" │ ") + " │";
  };
  out.push({ text: "┌" + divider + "┐", fg: colors.accent ?? colors.muted });
  const header = table.rows[0];
  out.push({ text: padRow(header, true), fg: colors.accent ?? colors.text, attributes: TextAttributes.BOLD });
  if (table.rows.length > 1) {
    out.push({ text: "├" + divider + "┤", fg: colors.accent ?? colors.muted });
  }
  for (let i = 1; i < table.rows.length; i++) {
    out.push({ text: padRow(table.rows[i], false), fg: colors.text });
  }
  out.push({ text: "└" + divider + "┘", fg: colors.accent ?? colors.muted });
  return out;
}

function renderCodeBlock(out, lang, lines, colors) {
  const langColor = colors.accent || colors.code;
  const langLabel = getLangLabel(lang);
  const maxLen = Math.max(40, ...lines.map(l => l.length));
  const contentWidth = Math.min(maxLen + 2, 80);
  const topLine = `  ${langLabel} ` + "─".repeat(Math.max(0, contentWidth - langLabel.length - 2));
  const bottomLine = "  " + "─".repeat(contentWidth);

  out.push({ text: topLine, fg: langColor });

  const lowerLang = lang.toLowerCase();
  if (lowerLang === "diff" || lowerLang === "patch") {
    for (const line of lines) {
      const trimmed = line.trimStart();
      if (trimmed.startsWith("+")) {
        out.push({ text: `  ${line}`, fg: colors.success || "#98fb98" });
      } else if (trimmed.startsWith("-")) {
        out.push({ text: `  ${line}`, fg: colors.error || "#ff7b72" });
      } else if (trimmed.startsWith("@@")) {
        out.push({ text: `  ${line}`, fg: MONOKAI.comment });
      } else {
        out.push({ text: `  ${line}`, fg: colors.muted });
      }
    }
  } else {
    for (const line of lines) {
      out.push({ text: "  " + line, fg: colors.code });
    }
  }

  out.push({ text: bottomLine, fg: langColor });
}

export function renderMarkdownLines(content, colors) {
  const lines = (content ?? "").split("\n");
  const out = [];
  let inCode = false;
  let codeLang = "";
  let codeBuffer = [];
  let inThinking = false;
  let tableState = null;

  const flushTable = () => {
    if (tableState) {
      out.push(...renderTable(tableState, colors));
      tableState = null;
    }
  };

  for (const raw of lines) {
    const line = raw ?? "";
    const trimmed = line.trim();

    if (trimmed.startsWith("```")) {
      flushTable();
      if (inCode) {
        renderCodeBlock(out, codeLang, codeBuffer, colors);
        inCode = false;
        codeLang = "";
        codeBuffer = [];
      } else {
        inCode = true;
        codeLang = trimmed.slice(3).trim().toLowerCase();
        codeBuffer = [];
      }
      continue;
    }

    if (inCode) {
      codeBuffer.push(line);
      continue;
    }

    const thinkingLine = normalizeThinkingLine(trimmed);
    if (thinkingLine) {
      flushTable();
      inThinking = true;
      const rest = thinkingLine.slice("Thinking:".length).trimStart();
      out.push({ text: "Thinking:", fg: colors.accent ?? colors.code, attributes: TextAttributes.BOLD | TextAttributes.ITALIC });
      if (rest) {
        out.push({ text: " " + rest, fg: colors.thinking ?? colors.muted, attributes: TextAttributes.ITALIC });
      }
      continue;
    }

    const summaryLine = normalizeSummaryLine(trimmed);
    if (summaryLine) {
      flushTable();
      const rest = summaryLine.slice("Summary:".length).trimStart();
      out.push({ text: "Summary:", fg: colors.accent ?? colors.code, attributes: TextAttributes.BOLD });
      if (rest) {
        out.push({ text: " " + rest, fg: colors.text });
      }
      continue;
    }

    if (inThinking) {
      if (trimmed === "") {
        inThinking = false;
        out.push({ text: " " });
        continue;
      }
      if (isThinkingCommand(trimmed)) {
        out.push({ text: `  ${trimmed}`, fg: colors.code });
        continue;
      }
      out.push({ text: line, fg: colors.thinking ?? colors.muted });
      continue;
    }

    if (trimmed === "") {
      flushTable();
      out.push({ text: " " });
      continue;
    }

    const checkbox = /^[-*]\s+\[([ xX~\-])\]\s+(.*)$/.exec(trimmed);
    if (checkbox) {
      flushTable();
      const status = checkbox[1];
      const label = checkbox[2] ?? "";
      const normalized = `[${status}] ${label}`.trimEnd();
      if (status === "x" || status === "X") {
        out.push({ text: normalized, fg: colors.muted });
        continue;
      }
      if (status === "-" || status === "~") {
        out.push({ text: normalized, fg: colors.accent ?? colors.code, attributes: TextAttributes.BOLD });
        continue;
      }
      out.push({ text: normalized, fg: colors.muted });
      continue;
    }

    if (/^---+$/.test(trimmed)) {
      continue;
    }

    const list = /^[-*]\s+(.*)$/.exec(trimmed);
    if (list) {
      flushTable();
      out.push({ text: `- ${list[1]}`, fg: colors.text });
      continue;
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(trimmed);
    if (heading) {
      flushTable();
      const level = heading[1].length;
      const text = heading[2] || "";
      const lowered = text.toLowerCase().trim();
      const fg = lowered === "error" && colors.error
        ? colors.error
        : lowered === "output"
          ? colors.accent ?? colors.code
          : colors.text;
      out.push({
        text,
        fg,
        attributes: level <= 2 ? TextAttributes.BOLD : undefined,
      });
      continue;
    }

    if (trimmed.startsWith("> ")) {
      flushTable();
      out.push({ text: trimmed.slice(2), fg: colors.muted });
      continue;
    }

    if (trimmed.startsWith("|")) {
      const cells = trimmed.split("|").filter(c => c !== "").map(c => c.trim());
      if (cells.length >= 2) {
        if (!tableState) {
          tableState = { rows: [], colWidths: [], colCount: cells.length };
        }
        tableState.rows.push(cells);
        for (let i = 0; i < cells.length; i++) {
          if (i >= tableState.colWidths.length) {
            tableState.colWidths.push(0);
          }
          tableState.colWidths[i] = Math.max(tableState.colWidths[i], cells[i].length);
        }
        continue;
      }
    }

    flushTable();
    out.push({ text: line, fg: colors.text });
  }

  flushTable();
  return out;
}