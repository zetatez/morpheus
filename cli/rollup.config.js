import { babel } from "@rollup/plugin-babel"
import resolve from "@rollup/plugin-node-resolve"
import commonjs from "@rollup/plugin-commonjs"

export default {
  input: "src/index.tsx",
  output: {
    file: "dist/index.js",
    format: "esm",
    sourcemap: true,
  },
  external: ["fsevents", "readline", "yargs", "clipboardy", "file-type", "jsdiff", "shiki", "table", "fs", "fs/promises", "os", "path", "child_process", "crypto", "yargs/helpers", "assert", "util", "url", "stream", "events", "http", "https", "zlib", "querystring", "string_decoder", "timers", "buffer", "domain", "tty", "net", "tls", "dns", "dgram", "punycode"],
  plugins: [
    resolve({
      extensions: [".ts", ".tsx", ".js", ".jsx"],
    }),
    commonjs(),
    babel({
      babelHelpers: "bundled",
      extensions: [".ts", ".tsx", ".js", ".jsx"],
      presets: [
        ["@babel/preset-typescript", { isTSX: true, allExtensions: true }],
      ],
    }),
  ],
}