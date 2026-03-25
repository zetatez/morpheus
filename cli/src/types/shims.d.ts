declare module "yargs" {
  type OptionConfig = {
    type?: string
    default?: unknown
    describe?: string
  }

  type ParsedArgv = {
    session?: string
    url: string
    prompt?: string
    debugStream?: boolean
  }

  interface YargsInstance {
    option(name: string, config: OptionConfig): YargsInstance
    parseSync(): ParsedArgv
  }

  export default function yargs(args?: string[]): YargsInstance
}

declare module "yargs/helpers" {
  export function hideBin(argv: string[]): string[]
}
