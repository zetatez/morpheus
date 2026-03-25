import { build } from "vite"

await build({
  configFile: "vite.config.js",
  build: {
    ssr: true,
    outDir: "dist",
    rollupOptions: {
      input: "src/index.tsx",
      output: {
        entryFileNames: "index.js",
        format: "esm",
      },
      external: ["fsevents", "readline", "yargs", "clipboardy", "file-type", "jsdiff", "shiki", "table", "fs", "fs/promises", "os", "path", "child_process", "crypto", "yargs/helpers"],
    },
    target: "node18",
  },
})