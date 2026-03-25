# Morpheus Neovim Plugin

A Neovim plugin for interacting with Morpheus, a local AI agent runtime.

## Requirements

- Neovim 0.10+
- Morpheus server running (`morpheus serve`)

## Installation

### Using lazy.nvim

```lua
{
  "zetatez/morpheus",
  ft = "morpheus",
  config = function()
    require("morpheus").setup({
      endpoint = "http://localhost:8080",
      api_key = nil,  -- Set if using auth
      model = nil,    -- e.g., "MiniMax-M2.7"
      session = nil,  -- Session name, defaults to "default"
    })
  end,
}
```

### Using packer.nvim

```lua
use "zetatez/morpheus"
```

### Manual Installation

```bash
# Clone to your Neovim config
git clone https://github.com/zetatez/morpheus.git ~/.config/nvim/pack/morpheus/start/morpheus
```

## Setup

```lua
-- In your init.lua or lua/config/morpheus.lua
require("morpheus").setup({
  endpoint = "http://localhost:8080",
  api_key = nil,
  model = nil,
  session = nil,
})
```

## Commands

| Command | Description |
|---------|-------------|
| `:MorpheusChat` | Open chat panel |
| `:MorpheusChatWith {message}` | Send message directly |
| `:MorpheusPlan {task}` | Plan task (read-only) |
| `:MorpheusExplain {text}` | Explain code/text |
| `:MorpheusExplainVisual` | Explain selected text (visual mode) |
| `:MorpheusRefactor {code}` | Get refactoring suggestions |
| `:MorpheusRefactorVisual` | Refactor selected text (visual mode) |
| `:MorpheusReview` | Review git changes |
| `:MorpheusTest` | Get test recommendations |
| `:MorpheusSkills` | List available skills |
| `:MorpheusStatus` | Check connection status |
| `:MorpheusStop` | Close chat panel |

## Usage

### Chat

```vim
:MorpheusChatWith "Hello, help me write a function"
```

### Visual Mode Integration

Select code in visual mode and use:

```vim
:'<,'>MorpheusExplainVisual
:'<,'>MorpheusRefactorVisual
```

### Review Git Changes

```vim
:MorpheusReview
```

## Configuration

```lua
require("morpheus").setup({
  endpoint = "http://localhost:8080",  -- Morpheus server URL
  api_key = nil,                       -- Auth key if needed
  model = "MiniMax-M2.7",              -- Model to use
  session = "default",                 -- Session name
})
```

## Features

- **Chat Panel**: Floating window for interactive conversations
- **Visual Mode**: Select code and get explanations/refactoring
- **Git Integration**: Review uncommitted changes
- **Streaming Responses**: Real-time streaming via SSE
- **Skill System**: Access Morpheus built-in skills

## Architecture

```
extensions/neovim/
├── lua/morpheus/
│   ├── init.lua      # Setup and config
│   ├── api.lua       # REST API client
│   ├── ui.lua        # Chat panel UI
│   └── commands.lua  # Neovim commands
└── plugin/
    └── morpheus.lua  # Plugin loader
```

## License

MIT
