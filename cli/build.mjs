import * as esbuild from "esbuild"

const result = await esbuild.build({
  entryPoints: ["src/index.tsx"],
  bundle: true,
  outfile: "dist/index.js",
  platform: "node",
  target: "node18",
  format: "esm",
  jsx: "preserve",
  jsxFactory: "h",
  jsxFragment: "Fragment",
  jsxImportSource: undefined,
  inject: [],
  external: ["fsevents", "readline"],
  logLevel: "info",
})