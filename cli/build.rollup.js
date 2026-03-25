import { rollup } from "rollup"
import config from "./rollup.config.js"

const bundle = await rollup(config)
await bundle.write(config.output)
await bundle.close()