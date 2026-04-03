import { TextAttributes } from "@opentui/core";

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
  ruby: "#cc342d",
  rb: "#cc342d",
  php: "#4F5D95",
  c: "#555555",
  cpp: "#6e84d4",
  "c++": "#6e84d4",
  cs: "#178600",
  "c#": "#178600",
  swift: "#F05138",
  kotlin: "#7f8cff",
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
  diff: "#6ec1e4",
  patch: "#6ec1e4",
};

function getLangColor(lang) {
  return LANGUAGE_COLORS[lang.toLowerCase()] || "#6ec1e4";
}

function getLangLabel(lang) {
  return LANGUAGE_LABELS[lang.toLowerCase()] || lang.toUpperCase();
}

const COLORS = {
  keyword: "#ffa657",
  keyword2: "#ffa657",
  string: "#98c379",
  number: "#ffa657",
  comment: "#5c6370",
  function: "#ffa657",
  variable: "#6ec1e4",
  type: "#ffa657",
  operator: "#56b6c2",
  punctuation: "#abb2bf",
  attribute: "#ffa657",
  builtin: "#ffa657",
  constant: "#ffa657",
  selector: "#ffa657",
  property: "#6ec1e4",
  tag: "#ffa657",
};

const KEYWORDS = {
  bash: ["if", "then", "else", "elif", "fi", "case", "esac", "for", "while", "do", "done", "in", "function", "return", "local", "export", "echo", "cd", "ls", "mkdir", "rm", "cp", "mv", "cat", "grep", "sed", "awk", "find", "chmod", "chown", "sudo", "apt", "yum", "npm", "yarn", "pnpm", "git", "docker", "kubectl", "curl", "wget", "ssh", "scp", "rsync", "tar", "zip", "unzip", "source", "alias", "unalias", "exit", "readonly", "declare", "typeset", "unset", "shift", "set", "trap", "wait", "exec", "eval", "true", "false"],
  python: ["def", "class", "return", "if", "elif", "else", "for", "while", "try", "except", "finally", "with", "as", "import", "from", "raise", "pass", "break", "continue", "and", "or", "not", "in", "is", "lambda", "yield", "global", "nonlocal", "assert", "async", "await", "None", "True", "False", "self", "cls"],
  javascript: ["const", "let", "var", "function", "return", "if", "else", "for", "while", "do", "switch", "case", "break", "continue", "default", "try", "catch", "finally", "throw", "new", "delete", "typeof", "instanceof", "void", "this", "class", "extends", "super", "import", "export", "from", "as", "async", "await", "yield", "static", "get", "set", "of", "in", "true", "false", "null", "undefined", "NaN", "Infinity"],
  typescript: ["const", "let", "var", "function", "return", "if", "else", "for", "while", "do", "switch", "case", "break", "continue", "default", "try", "catch", "finally", "throw", "new", "delete", "typeof", "instanceof", "void", "this", "class", "extends", "super", "import", "export", "from", "as", "async", "await", "yield", "static", "get", "set", "of", "in", "true", "false", "null", "undefined", "interface", "type", "enum", "implements", "private", "public", "protected", "readonly", "abstract", "keyof", "namespace", "module", "declare", "infer", "partial", "required", "omit", "pick", "exclude"],
  go: ["func", "return", "if", "else", "for", "range", "switch", "case", "default", "break", "continue", "fallthrough", "goto", "defer", "go", "select", "chan", "type", "struct", "interface", "map", "const", "var", "package", "import", "nil", "true", "false", "iota", "len", "cap", "make", "new", "append", "copy", "delete", "close", "panic", "recover", "error", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "complex64", "complex128", "bool", "byte", "rune", "any"],
  rust: ["fn", "let", "mut", "const", "static", "if", "else", "match", "for", "while", "loop", "break", "continue", "return", "struct", "enum", "impl", "trait", "type", "where", "pub", "mod", "use", "crate", "self", "super", "as", "ref", "move", "async", "await", "dyn", "unsafe", "extern", "true", "false", "Some", "None", "Ok", "Err", "Box", "Rc", "Arc", "Cell", "RefCell", "Mutex"],
  java: ["abstract", "assert", "boolean", "break", "byte", "case", "catch", "char", "class", "const", "continue", "default", "do", "double", "else", "enum", "extends", "final", "finally", "float", "for", "goto", "if", "implements", "import", "instanceof", "int", "interface", "long", "native", "new", "package", "private", "protected", "public", "return", "short", "static", "strictfp", "super", "switch", "synchronized", "this", "throw", "throws", "transient", "try", "void", "volatile", "while", "true", "false", "null"],
  c: ["auto", "break", "case", "char", "const", "continue", "default", "do", "double", "else", "enum", "extern", "float", "for", "goto", "if", "inline", "int", "long", "register", "restrict", "return", "short", "signed", "sizeof", "static", "struct", "switch", "typedef", "union", "unsigned", "void", "volatile", "while", "NULL", "printf", "scanf", "malloc", "free"],
  cpp: ["alignas", "alignof", "and", "and_eq", "asm", "auto", "bitand", "bitor", "bool", "break", "case", "catch", "char", "char16_t", "char32_t", "class", "compl", "const", "constexpr", "const_cast", "continue", "decltype", "default", "delete", "do", "double", "dynamic_cast", "else", "enum", "explicit", "export", "extern", "false", "float", "for", "friend", "goto", "if", "inline", "int", "long", "mutable", "namespace", "new", "noexcept", "not", "not_eq", "nullptr", "operator", "or", "or_eq", "private", "protected", "public", "register", "reinterpret_cast", "return", "short", "signed", "sizeof", "static", "static_assert", "static_cast", "struct", "switch", "template", "this", "thread_local", "throw", "true", "try", "typedef", "typeid", "typename", "union", "unsigned", "using", "virtual", "void", "volatile", "wchar_t", "while", "xor", "xor_eq", "std", "string", "vector", "map", "set", "cout", "cin", "endl", "malloc", "free"],
  sql: ["SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "IS", "NULL", "LIKE", "BETWEEN", "JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "FULL", "ON", "AS", "ORDER", "BY", "GROUP", "HAVING", "LIMIT", "OFFSET", "INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE", "CREATE", "TABLE", "INDEX", "VIEW", "DROP", "ALTER", "ADD", "COLUMN", "PRIMARY", "KEY", "FOREIGN", "REFERENCES", "UNIQUE", "CHECK", "DEFAULT", "CONSTRAINT", "CASCADE", "DISTINCT", "ALL", "EXISTS", "CASE", "WHEN", "THEN", "ELSE", "END", "UNION", "INTERSECT", "EXCEPT", "ASC", "DESC", "INTEGER", "INT", "BIGINT", "SMALLINT", "REAL", "DOUBLE", "DECIMAL", "NUMERIC", "CHAR", "VARCHAR", "TEXT", "DATE", "TIME", "TIMESTAMP", "BLOB"],
  json: ["true", "false", "null"],
  yaml: ["true", "false", "null", "yes", "no", "on", "off", "~"],
  dockerfile: ["FROM", "RUN", "CMD", "LABEL", "EXPOSE", "ENV", "ADD", "COPY", "ENTRYPOINT", "VOLUME", "USER", "WORKDIR", "ARG", "ONBUILD", "STOPSIGNAL", "HEALTHCHECK", "MAINTAINER", "SHELL"],
  html: ["html", "head", "body", "div", "span", "p", "a", "img", "ul", "ol", "li", "table", "tr", "td", "th", "form", "input", "button", "select", "option", "textarea", "h1", "h2", "h3", "h4", "h5", "h6", "header", "footer", "nav", "section", "article", "aside", "main", "script", "style", "link", "meta", "title"],
  css: ["important", "inherit", "initial", "unset", "none", "auto", "normal", "bold", "italic", "underline", "block", "inline", "flex", "grid", "relative", "absolute", "fixed", "sticky", "static", "hidden", "visible", "scroll", "transparent", "solid", "dashed", "dotted", "center", "left", "right", "top", "bottom", "wrap", "nowrap", "color", "background", "margin", "padding", "border", "width", "height", "display", "position", "font", "text"],
};

const BUILTINS = {
  python: ["print", "len", "range", "str", "int", "float", "list", "dict", "set", "tuple", "bool", "type", "isinstance", "hasattr", "getattr", "setattr", "open", "input", "map", "filter", "zip", "enumerate", "sorted", "reversed", "min", "max", "sum", "abs", "round", "pow", "divmod", "hex", "oct", "bin", "ord", "chr", "repr", "format", "slice", "super", "self", "cls", "object", "Exception"],
  javascript: ["console", "window", "document", "Math", "JSON", "Array", "Object", "String", "Number", "Boolean", "Function", "Symbol", "Map", "Set", "WeakMap", "WeakSet", "Promise", "Proxy", "Reflect", "Date", "RegExp", "Error", "parseInt", "parseFloat", "isNaN", "isFinite", "encodeURI", "decodeURI", "setTimeout", "setInterval", "clearTimeout", "clearInterval", "fetch", "require", "module", "exports", "process", "global", "Buffer"],
  typescript: ["console", "window", "document", "Math", "JSON", "Array", "Object", "String", "Number", "Boolean", "Function", "Symbol", "Map", "Set", "Promise", "Date", "RegExp", "Error", "parseInt", "parseFloat", "fetch", "require", "module", "exports", "Partial", "Required", "Readonly", "Record", "Pick", "Omit", "Exclude", "Extract", "NonNullable", "ReturnType", "InstanceType", "Parameters"],
  go: ["append", "cap", "close", "complex", "copy", "delete", "imag", "len", "make", "new", "panic", "print", "println", "real", "recover", "fmt", "strings", "strconv", "os", "io", "bufio", "bytes", "path", "filepath", "encoding", "json", "xml", "http", "net", "flag", "log", "sort", "container", "list", "ring", "sync", "time", "context"],
  rust: ["println", "print", "eprintln", "eprint", "format", "panic", "assert", "assert_eq", "assert_ne", "todo", "unimplemented", "unreachable", "dbg", "vec", "string", "to_string", "into_iter", "iter", "next", "Some", "None", "Ok", "Err", "Box", "Rc", "Arc", "Cell", "RefCell", "Mutex"],
  java: ["System", "out", "println", "print", "String", "Integer", "Double", "Boolean", "Long", "Float", "Short", "Byte", "Character", "Object", "Class", "Math", "Thread", "Runnable", "Exception", "Error", "Throwable", "List", "ArrayList", "Map", "HashMap", "Set", "HashSet", "Queue", "Deque", "ArrayDeque", "Collections", "Arrays", "StringBuilder"],
};

const TYPES = {
  python: ["int", "str", "float", "bool", "list", "dict", "set", "tuple", "bytes", "object", "type", "Exception", "TypeError", "ValueError", "KeyError", "IndexError", "AttributeError"],
  javascript: ["Array", "Object", "String", "Number", "Boolean", "Function", "Symbol", "Map", "Set", "WeakMap", "WeakSet", "Promise", "Proxy", "Reflect", "Date", "RegExp", "Error", "ArrayBuffer", "DataView", "Int8Array", "Uint8Array", "Float32Array", "Float64Array"],
  typescript: ["Array", "Object", "String", "Number", "Boolean", "Function", "Symbol", "Map", "Set", "Promise", "Date", "RegExp", "Error", "Partial", "Required", "Readonly", "Record", "Pick", "Omit", "Exclude", "Extract", "any", "unknown", "never", "void", "null", "undefined", "string", "number", "boolean", "symbol", "bigint"],
  go: ["int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "complex64", "complex128", "bool", "byte", "rune", "string", "error", "any", "interface", "struct", "map", "slice", "chan"],
  rust: ["i8", "i16", "i32", "i64", "i128", "isize", "u8", "u16", "u32", "u64", "u128", "usize", "f32", "f64", "bool", "char", "str", "String", "Vec", "Box", "Option", "Result", "HashMap", "HashSet"],
  java: ["int", "char", "float", "double", "void", "long", "short", "byte", "boolean", "Integer", "Long", "Float", "Double", "Short", "Byte", "Character", "Boolean", "String", "Object", "Class", "List", "ArrayList", "Map", "HashMap", "Set", "HashSet"],
  c: ["int", "char", "float", "double", "void", "long", "short", "unsigned", "signed", "size_t", "ptrdiff_t", "int8_t", "int16_t", "int32_t", "int64_t", "uint8_t", "uint16_t", "uint32_t", "uint64_t", "bool", "FILE", "DIR", "NULL"],
};

function escapeRegex(str) {
  return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function escapeHtml(str) {
  return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function highlightCode(code, lang) {
  const keywords = KEYWORDS[lang] || [];
  const builtins = BUILTINS[lang] || [];
  const types = TYPES[lang] || [];

  const keywordSet = new Set(keywords);
  const builtinSet = new Set(builtins);
  const typeSet = new Set(types);

  const lines = code.split("\n");
  const result = [];

  for (const line of lines) {
    const tokens = [];
    let remaining = line;
    let pos = 0;

    while (pos < remaining.length) {
      let matched = false;

      if (remaining.slice(pos).startsWith("//")) {
        tokens.push({ type: "comment", text: remaining.slice(pos) });
        break;
      }

      if (remaining.slice(pos).startsWith("/*")) {
        const end = remaining.indexOf("*/", pos + 2);
        const endPos = end >= 0 ? end + 2 : remaining.length;
        tokens.push({ type: "comment", text: remaining.slice(pos, endPos) });
        pos = endPos;
        matched = true;
        continue;
      }

      if (remaining.slice(pos).startsWith("#") && (lang === "python" || lang === "bash" || lang === "sh" || lang === "shell" || lang === "zsh" || lang === "yaml" || lang === "dockerfile")) {
        tokens.push({ type: "comment", text: remaining.slice(pos) });
        break;
      }

      if (remaining[pos] === '"' || remaining[pos] === "'" || remaining[pos] === "`") {
        const quote = remaining[pos];
        let end = pos + 1;
        while (end < remaining.length) {
          if (remaining[end] === "\\" && end + 1 < remaining.length) {
            end += 2;
            continue;
          }
          if (remaining[end] === quote) {
            end++;
            break;
          }
          end++;
        }
        tokens.push({ type: "string", text: remaining.slice(pos, end) });
        pos = end;
        matched = true;
        continue;
      }

      if (/\d/.test(remaining[pos])) {
        let end = pos;
        while (end < remaining.length && /[\d.xXoObBeE_]/.test(remaining[end])) {
          end++;
        }
        if (end > pos) {
          tokens.push({ type: "number", text: remaining.slice(pos, end) });
          pos = end;
          matched = true;
          continue;
        }
      }

      if (remaining.slice(pos).startsWith("<!--") && (lang === "html" || lang === "xml")) {
        const end = remaining.indexOf("-->", pos + 4);
        const endPos = end >= 0 ? end + 3 : remaining.length;
        tokens.push({ type: "comment", text: remaining.slice(pos, endPos) });
        pos = endPos;
        matched = true;
        continue;
      }

      if ((lang === "html" || lang === "xml" || lang === "css") && remaining[pos] === "<") {
        let end = pos + 1;
        while (end < remaining.length && remaining[end] !== ">") {
          if (remaining[end] === '"' || remaining[end] === "'") {
            const quote = remaining[end];
            end++;
            while (end < remaining.length && remaining[end] !== quote) {
              if (remaining[end] === "\\") end++;
              end++;
            }
            if (end < remaining.length) end++;
          } else {
            end++;
          }
        }
        if (end < remaining.length) end++;
        tokens.push({ type: "tag", text: remaining.slice(pos, end) });
        pos = end;
        matched = true;
        continue;
      }

      if (lang === "css" || lang === "html") {
        const selectorMatch = remaining.slice(pos).match(/^[.#]?[a-zA-Z_-][a-zA-Z0-9_-]*/);
        if (selectorMatch && remaining[pos] === ".") {
          tokens.push({ type: "selector", text: selectorMatch[0] });
          pos += selectorMatch[0].length;
          matched = true;
          continue;
        }
        if (selectorMatch && remaining[pos] === "#") {
          tokens.push({ type: "selector", text: "#" + selectorMatch[0].slice(1) });
          pos += selectorMatch[0].length;
          matched = true;
          continue;
        }
      }

      const wordMatch = remaining.slice(pos).match(/^[a-zA-Z_][a-zA-Z0-9_]*/);
      if (wordMatch && wordMatch[0].length > 0) {
        const word = wordMatch[0];
        if (keywordSet.has(word)) {
          tokens.push({ type: "keyword", text: word });
        } else if (builtinSet.has(word)) {
          tokens.push({ type: "builtin", text: word });
        } else if (typeSet.has(word)) {
          tokens.push({ type: "type", text: word });
        } else if (remaining.slice(pos + word.length).match(/^\s*\(/)) {
          tokens.push({ type: "function", text: word });
        } else if (/^[A-Z][a-zA-Z0-9]*$/.test(word)) {
          tokens.push({ type: "type", text: word });
        } else {
          tokens.push({ type: "variable", text: word });
        }
        pos += word.length;
        matched = true;
        continue;
      }

      if (/[\+\-\*\/\=\%\&\|\^\!\~\<\>]/.test(remaining[pos])) {
        let end = pos + 1;
        while (end < remaining.length && /[\+\-\*\/\=\%\&\|\^\!\~\<\>]/.test(remaining[end])) {
          end++;
        }
        tokens.push({ type: "operator", text: remaining.slice(pos, end) });
        pos = end;
        matched = true;
        continue;
      }

      if (/[\{\}\[\]\(\)\,\.\;]/.test(remaining[pos])) {
        tokens.push({ type: "punctuation", text: remaining[pos] });
        pos++;
        matched = true;
        continue;
      }

      if (/\s/.test(remaining[pos])) {
        let end = pos + 1;
        while (end < remaining.length && /\s/.test(remaining[end])) {
          end++;
        }
        tokens.push({ type: "whitespace", text: remaining.slice(pos, end) });
        pos = end;
        matched = true;
        continue;
      }

      tokens.push({ type: "text", text: remaining[pos] });
      pos++;
    }

    result.push(tokens);
  }

  return result;
}

function tokenToFg(type) {
  return COLORS[type] || "#abb2bf";
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
        out.push({ text: "  " + line, fg: colors.success || "#98c379" });
      } else if (trimmed.startsWith("-")) {
        out.push({ text: "  " + line, fg: colors.error || "#e06c75" });
      } else if (trimmed.startsWith("@@")) {
        out.push({ text: "  " + line, fg: COLORS.comment });
      } else if (trimmed.startsWith("\\")) {
        out.push({ text: "  " + line, fg: COLORS.string });
      } else {
        out.push({ text: "  " + line, fg: colors.muted || "#5c6370" });
      }
    }
  } else {
    const code = lines.join("\n");
    const highlighted = highlightCode(code, lowerLang);

    for (const lineTokens of highlighted) {
      if (lineTokens.length === 0) {
        out.push({ text: "  ", fg: colors.code });
        continue;
      }

      const spans = [];
      for (const token of lineTokens) {
        if (token.type === "keyword") {
          spans.push({ text: token.text, fg: COLORS.keyword });
        } else if (token.type === "comment") {
          spans.push({ text: token.text, fg: COLORS.comment });
        } else {
          spans.push({ text: token.text, fg: colors.code });
        }
      }

      out.push({ spans });
    }
  }

  out.push({ text: bottomLine, fg: langColor });
}

function renderInlineCode(text, colors) {
  return { text, fg: "#ffa657" };
}

function renderStrong(text, colors) {
  return { text, fg: "#ffa657", attributes: TextAttributes.BOLD };
}

function renderEmphasis(text, colors) {
  return { text, fg: colors.muted || "#5c6370", attributes: TextAttributes.ITALIC };
}

function parseInlineTokens(line, colors) {
  const tokens = [];
  let remaining = line;
  let pos = 0;

  while (pos < remaining.length) {
    if (remaining[pos] === "`") {
      let end = pos + 1;
      while (end < remaining.length && remaining[end] !== "`") {
        if (remaining[end] === "\\" && end + 1 < remaining.length) end++;
        end++;
      }
      if (end < remaining.length) end++;
      const code = remaining.slice(pos + 1, end - 1);
      tokens.push({ type: "code", text: code });
      pos = end;
      continue;
    }

    if (remaining.slice(pos, pos + 2) === "**") {
      let end = pos + 2;
      while (end < remaining.length - 1 && remaining.slice(end, end + 2) !== "**") {
        end++;
      }
      if (remaining.slice(end, end + 2) === "**") {
        const strong = remaining.slice(pos + 2, end);
        tokens.push({ type: "strong", text: strong });
        pos = end + 2;
        continue;
      }
    }

    if (remaining[pos] === "*" || remaining[pos] === "_") {
      const marker = remaining[pos];
      let end = pos + 1;
      while (end < remaining.length && remaining[end] !== marker) {
        if (remaining[end] === "\\" && end + 1 < remaining.length) end++;
        end++;
      }
      if (end < remaining.length && remaining[end] === marker && end > pos + 1) {
        const emph = remaining.slice(pos + 1, end);
        tokens.push({ type: "emphasis", text: emph });
        pos = end + 1;
        continue;
      }
    }

    let end = pos + 1;
    while (end < remaining.length && !"`*_".includes(remaining[end])) {
      end++;
    }
    tokens.push({ type: "text", text: remaining.slice(pos, end) });
    pos = end;
  }

  return tokens;
}

function renderInline(line, colors) {
  const tokens = parseInlineTokens(line, colors);
  const out = [];
  for (const token of tokens) {
    switch (token.type) {
      case "code":
        out.push(renderInlineCode(token.text, colors));
        break;
      case "strong":
        out.push(renderStrong(token.text, colors));
        break;
      case "emphasis":
        out.push(renderEmphasis(token.text, colors));
        break;
      default:
        out.push({ text: token.text, fg: colors.text });
    }
  }
  return out;
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
  const padRow = (cells) => {
    const padded = cells.map((cell, i) => {
      const width = table.colWidths[i] ?? cell.length;
      return cell.padEnd(width);
    });
    return "│ " + padded.join(" │ ") + " │";
  };
  out.push({ text: "┌" + divider + "┐", fg: colors.accent ?? colors.muted });
  out.push({ text: padRow(table.rows[0]), fg: colors.accent ?? colors.text, attributes: TextAttributes.BOLD });
  if (table.rows.length > 1) {
    out.push({ text: "├" + divider + "┤", fg: colors.accent ?? colors.muted });
  }
  for (let i = 1; i < table.rows.length; i++) {
    out.push({ text: padRow(table.rows[i]), fg: colors.text });
  }
  out.push({ text: "└" + divider + "┘", fg: colors.accent ?? colors.muted });
  return out;
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

  const flushCode = () => {
    if (codeBuffer.length > 0) {
      renderCodeBlock(out, codeLang, codeBuffer, colors);
      codeBuffer = [];
    }
  };

  for (const raw of lines) {
    const line = raw ?? "";
    const trimmed = line.trim();

    if (trimmed.startsWith("```")) {
      flushTable();
      if (inCode) {
        flushCode();
        inCode = false;
        codeLang = "";
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
      out.push({ text: "Summary:", fg: colors.summary ?? colors.accent ?? colors.code, attributes: TextAttributes.BOLD });
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

    if (/^---+$/.test(trimmed)) {
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

    const list = /^[-*]\s+(.*)$/.exec(trimmed);
    if (list) {
      flushTable();
      const parts = renderInline(list[1], colors);
      for (const part of parts) {
        if (part.text === "- ") continue;
      }
      out.push({ text: `  • ${list[1]}`, fg: colors.text });
      continue;
    }

    const numberedList = /^\d+\.\s+(.*)$/.exec(trimmed);
    if (numberedList) {
      flushTable();
      out.push({ text: `    ${numberedList[1]}`, fg: colors.text });
      continue;
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(trimmed);
    if (heading) {
      flushTable();
      const level = heading[1].length;
      const text = heading[2] || "";
      const lowered = text.toLowerCase().trim();

      const headingColors = ["#ffa657", "#ffb347", "#ffc266", "#ffd180", "#ffe0a0", "#fff0c0"];
      const headingFg = headingColors[level - 1] || "#fff0c0";
      const attributes = TextAttributes.BOLD;

      let fg;
      if (lowered === "error" && colors.error) {
        fg = colors.error;
      } else if (lowered === "output" || lowered === "result") {
        fg = colors.output || "#98c379";
      } else {
        fg = headingFg;
      }

      out.push({ text, fg, attributes });
      continue;
    }

    if (trimmed.startsWith("> ")) {
      flushTable();
      out.push({ text: "  " + trimmed.slice(2), fg: colors.muted || "#5c6370", attributes: TextAttributes.ITALIC });
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
    const inlineParts = renderInline(line, colors);
    for (const part of inlineParts) {
      out.push(part);
    }
  }

  flushTable();
  return out;
}
