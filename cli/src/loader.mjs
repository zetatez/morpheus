import { fileURLToPath } from "node:url"

export async function resolve(specifier, context, defaultResolve) {
  return defaultResolve(specifier, context, defaultResolve)
}

export async function load(url, context, defaultLoad) {
  if (context.importAttributes?.type === "file" || url.endsWith(".scm")) {
    const source = fileURLToPath(url)
    return {
      format: "module",
      source: `export default ${JSON.stringify(source)};`,
      shortCircuit: true,
    }
  }

  return defaultLoad(url, context, defaultLoad)
}
