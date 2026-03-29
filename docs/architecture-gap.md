# Morpheus 架构差距分析报告

本文档对比 Morpheus 与当前业界领先 AI 编码助手（Claude Code、OpenCode、 Gemini-CLI、OpenAI Codex）的架构实现，识别功能差距并提出改进建议。

## 1. 架构对比总览

  | 维度           | Morpheus            | Claude Code          | OpenCode               | Gemini-CLI        | Codex          |
  | ------         | ----------          | -------------        | ----------             | ------------      | -------        |
  | **开源协议**   | MIT                 | 专有                 | Apache 2.0             | Apache 2.0        | 专有           |
  | **架构模式**   | 多 Agent 协调       | 多 Agent 协调        | 双 pipeline + 多 Agent | ReAct loop + 并行 | Agent 模式     |
  | **模型支持**   | 6 + builtin         | Claude only          | 75+                    | Gemini only       | GPT-5 系列     |
  | **持久化**     | SQLite + 文件       | SQLite               | SQLite                 | 文件 + Session    | SQLite         |
  | **上下文管理** | 智能 token 压缩     | CLAUDE.md + 自动压缩 | Auto Compact 95%       | GEMINI.md         | AGENTS.md      |
  | **工具协议**   | MCP (已实现)        | MCP (开放标准)       | MCP                    | MCP + Extensions  | MCP            |
  | **插件系统**   | 基础钩子            | Skills + Plugins     | 30+ 自定义工具         | Extensions        | Plugins        |
  | **Agent 类型** | 多 Profile + 自定义 | 单一 + 子 Agent      | Plan/Build + 自定义    | 单一              | Agent 模式切换 |
  | **Skills**     | 9 内置 + Lazy Load  | Bundled Skills       | Skills                 | Skills            | Skills         |
  | **权限控制**   | 策略引擎 + 交互确认 | 细粒度权限           | 工具级别权限           | 确认机制          | Approval modes |
  | **TUI**        | OpenTUI             | 原生 TUI             | 原生 TUI               | 原生 TUI          | 原生 TUI       |
  | **平台**       | macOS/Linux         | 全平台               | 全平台                 | 全平台            | macOS/Linux    |

---

## 2. 竞品深度分析

### 2.1 Claude Code (Anthropic)

**最新特性 (2025-2026)**:
- **Agent Teams**: 多会话协调，共享任务和 P2P 消息
- **Subagents**: 可配置子 Agent，支持隔离上下文
- **Background Agents**: 子 Agent 并行运行，不阻塞主对话
- **/btw 命令**: 2026年3月发布，可在不破坏对话历史的情况下提问
- **Remote Control**: 从 Claude.ai 或 Claude App 远程控制
- **Bare Mode**: 最小化模式，跳过自动发现以加速脚本调用
- **Planning Mode**: 结构化项目规划，目标设定和风险识别
- **Checkpoints**: 自动代码状态快照，便于回滚
- **Hooks**: 生命周期自动化钩子，HTTP hooks (2026年2月)
- **Plugins**: 可分享的包，含斜杠命令、MCP 服务器、agents、hooks
- **模型**: Sonnet 4.6 (默认), Opus 4.6, Haiku 4.5
- **1M token 上下文**: Sonnet 4.6 支持扩展思考和自适应思考
- **MCP**: 支持远程 MCP 服务器和 OAuth 认证
- **GitHub 集成**: `/pr-comments` 获取 PR 评论
- **多工作目录**: `--add-dir` 添加额外工作目录
- **工作树隔离**: `--worktree` 在独立 git worktree 中运行
- **VS Code 扩展**: 原生 IDE 集成 (beta)

**权限系统**:
```
Hook Chain:
PreToolUse → Permissions → Execute
            → PostToolUse

Permissions:
- Allow/Deny per tool
- Confirmation for sensitive ops
- Rate limiting
```

**CLI 引用**:
```bash
claude                    # 交互模式
claude "query"            # 带初始提示
claude -p "query"         # 单次查询模式
claude --resume [id]      # 恢复会话
claude --model claude-opus-4-6
claude --max-turns 50
claude --dangerously-skip-permissions
claude --bare -p "query"
```

### 2.2 OpenCode (Anomaly)

**最新特性 (2026)**:
- **100% 开源**: Apache 2.0 许可证 (131K stars)
- **模型无关**: 支持 75+ LLM 提供商
- **客户端/服务器架构**: 可远程控制
- **原生 LSP 支持**: 自动语言服务器集成
- **多会话**: 并行运行多个 Agent
- **分享链接**: 分享会话链接供调试
- **GitHub Copilot 集成**: 使用 Copilot 账号
- **Zen 服务**: 优化和基准测试的精选模型
- **TUI Mission Control**: v1.3.3 任务控制中心
- **Event-sourced Syncing**: 事件源同步
- **Desktop App (beta)**: macOS/Windows/Linux + IDE 扩展
- **OpenCode Zen**: 精选编码代理模型

**内置 Agent**:
 | Agent   | 用途              |
 | ------- | ------            |
 | build   | 全权限开发 (默认) |
 | plan    | 只读分析和规划    |
 | general | 通用子 Agent      |
 | explore | 快速代码探索      |

**配置示例**:
```json
{
  "$schema": "https://opencode.ai/config.json",
  "agent": {
    "review": {
      "mode": "subagent",
      "description": "Reviews code for best practices",
      "tools": {
        "write": false,
        "edit": false
      }
    }
  }
}
```

**Skills 配置**:
```yaml
---
name: git-release
description: Create consistent releases and changelogs
license: MIT
---
# Skills 从 .opencode/skills/ 或 ~/.config/opencode/skills/ 加载
```

### 2.3 Gemini CLI (Google)

**最新特性 (2026)**:
- **免费额度**: 60 请求/分钟, 1000 请求/天 (个人 Google 账号)
- **Gemini 3 模型**: 1M token 上下文窗口
- **会话管理**: 自动保存和恢复会话 (v0.20.0+)
- **会话浏览器**: `/resume` 交互式会话选择
- **Checkpoints**: 自动保存和恢复快照
- **沙盒隔离**: 容器化安全执行环境
- **Token 缓存**: 优化 API 成本
- **Trusted Folders**: 安全控制项目访问
- **GEMINI.md**: 分层持久上下文
- **.geminiignore**: 排除文件和目录
- **Extensions**: 扩展生态系统 (Dynatrace, Elastic, Figma, Shopify)
- **Agent Skills**: 2026年3月发布，更智能的扩展
- **GitHub Action**: 官方 Action 支持 PR 审查、issue 分类
- **非交互模式**: 管道输入和脚本自动化
- **内置搜索 grounding**: 实时网络信息辅助调试

**CLI 命令**:
```bash
gemini                           # 交互模式
gemini "query"                   # 单次查询
gemini --file path/to/file       # 分析文件
gemini --include "**/*.py"       # 文件模式
gemini --stream                  # 流式响应
gemini --model gemini-2.5-pro
gemini --resume                  # 恢复会话
gemini --list-sessions           # 列出所有会话
gemini config set model gemini-2.5-pro
gemini extensions install <name> # 安装扩展
```

**工具**:
- 文件操作: read_file, write_file, replace, glob, search_file_content
- Shell 命令: ! 前缀
- Web: google_web_search, web_fetch
- Agent: browser_agent, activate_skill, write_todos, save_memory

### 2.4 Codex CLI (OpenAI)

**最新特性 (2026年3月)**:
- **GPT-5.4**: 默认推荐模型，结合前沿编码能力和强推理
- **Rust 实现**: 速度和效率 (v0.117.0)
- **Plugins 一等公民**: 产品级插件系统，同步/浏览/安装
- **子 Agent v2**: 路径寻址 (如 `/root/agent_a`)，结构化消息
- **App Server TUI**: 默认启用，支持远程连接
- **图像工作流**: `view_image` 返回 URL，生成历史可恢复
- **Approval Modes**: auto, pre-commit, full-access, yolo
- **多平台沙盒**: Linux bubblewrap + Windows restricted-token
- **代码审查**: `/review` 启动专用审查
- **Feature Flags**: 特性开关配置
- **多主题**: TUI 语法高亮和主题
- **Worktrees**: 独立 git worktree 隔离
- **Webhook/Automation**: 非交互模式，CI/CD 集成
- **GitHub/Slack/Linear 集成**: 官方集成支持
- **企业配置**: Managed configuration, RBAC

**CLI 命令**:
```bash
codex                           # 交互 TUI
codex "query"                   # 单次查询
codex --model gpt-5.4
codex --path /path/to/project
codex exec "fix the CI failure" # 非交互执行
codex --image img1.png,img2.jpg # 图像输入
codex features list             # 特性列表
codex features enable unified_exec
codex /review                   # 代码审查
codex /model                    # 切换模型
codex /permissions              # 权限设置
```

**配置**:
```toml
model = "gpt-5.4"
review_model = "gpt-5.4"
[agents]
```

---

## 3. Morpheus 当前实现

### 3.1 已实现功能 ✅

**MCP 协议**:
- 完整的 MCP 客户端实现
- 支持 stdio、HTTP、SSE 三种传输方式
- 动态代理 MCP 服务器工具
- 资源订阅和缓存机制

```go
type Manager struct {
    clients          map[string]*client
    tools            map[string]*ProxyTool
    onChange         func() error
    onResourceUpdate func(server, uri string, payload map[string]any)
}
```

**多 Agent 协调**:
- Coordinator 实现 (`internal/app/coordinator.go`)
- 任务自动分解 (最大 6 个任务)
- 并行执行 (最大 3 并发)
- 9 种内置 Agent: implementer, explorer, reviewer, architect, tester, devops, data, security, docs

**自定义 Agent**:
- YAML 配置支持
- 工具权限配置 (tools 字段)
- AgentRegistry 管理

**交互式确认**:
- approve/deny 机制
- 4 级风险评估 (low/medium/high/critical)
- 受保护路径配置

**Skills 系统**:
- 9 个内置 Skills: review, test, docs, refactor, debug, security, git, explain, optimize
- Lazy Load 机制
- 自定义 skill 目录支持

**SQLite 持久化**:
- 会话存储
- 消息记录

### 3.2 待实现功能

 | 功能                | 描述                  | 优先级   |
 | ------              | ------                | -------- |
 | claude.md/gemini.md | 项目级上下文文件      | high     |
 | agent teams         | 多会话协调            | medium   |
 | remote control      | 远程控制协议          | medium   |
 | subagents 增强      | 更灵活的子 agent 配置 | medium   |
 | 插件市场            | 插件打包和分发        | low      |
 | 多平台支持          | windows 原生          | low      |
 | ide 集成            | vs code/editor 扩展   | low      |

---

## 4. 功能差距详解

### 4.1 上下文管理

**Morpheus**:
- 基于 token 计数的智能压缩 (60000 tokens 阈值)
- 保留最近 4 条消息 + 生成结构化摘要

**竞品**:
- Claude Code: CLAUDE.md (项目级), ~/.claude.md (全局级), Auto Memory
- OpenCode: Auto Compact 95%, AGENTS.md
- Gemini CLI: GEMINI.md, 分层上下文
- Codex: AGENTS.md

**差距**:
- 无项目级上下文文件机制
- 无全局级上下文文件
- 无自动记忆截断

### 4.2 Agent 系统

**Morpheus**:
- 9 种内置 Agent Profile
- 自定义 Agent 配置
- DAG 任务调度

**竞品**:
- Claude Code: Subagents (可配置), Agent Teams
- OpenCode: Primary (Build/Plan) + Subagents (General/Explore) + 自定义
- Gemini CLI: 单一 Agent, Extension 扩展
- Codex: Agent 模式切换, Subagents

**差距**:
- 无 Agent Teams 多会话协调
- 无远程控制协议

### 4.3 权限系统

**Morpheus**:
- 策略引擎 (risk factors + protected paths)
- 4 级风险评估
- 交互式确认

**竞品**:
- Claude Code: Hook Chain, Allow/Deny, Rate limiting
- OpenCode: Pattern-based permissions
- Gemini CLI: Trusted Folders, .geminiignore
- Codex: Approval modes (auto, pre-commit, full-access, yolo)

**差距**:
- 无细粒度权限规则
- 无运行时权限调整

### 4.4 生态

**Morpheus**:
- 基础 MCP 支持
- 9 个内置 Skills
- 自定义 Skills 目录

**竞品**:
- Claude Code: 插件市场, 第三方 Skills
- OpenCode: 30+ 自定义工具, Zen 优化模型
- Gemini CLI: Extensions 生态 (Dynatrace, Elastic, Figma)
- Codex: Plugins, Codex Cloud

**差距**:
- 无官方插件市场
- 无社区 Extensions/Skills 生态
- 无 Zen/Cloud 托管服务

---

## 5. 架构改进建议

### 5.1 High Priority: 上下文管理

#### 5.1.1 引入项目上下文文件
```yaml
# 支持 .morpheus.md (项目级)
# 支持 ~/.morpheus.md (全局级)
```

#### 5.1.2 增强智能上下文压缩
- 基于 token 计数而非固定阈值
- 保留关键决策点
- 生成结构化摘要

### 5.2 Medium Priority: Agent 增强

#### 5.2.1 Agent Teams
```go
type Team struct {
    ID      string
    Agents  []string  // Agent IDs
    SharedContext bool
}
```

#### 5.2.2 Remote Control
```bash
morph remote --listen ws://localhost:8080
morph remote --connect ws://remote:8080
```

### 5.3 Low Priority: 生态完善

#### 5.3.1 插件市场
- 支持插件打包和分发
- 集成社区 Skills

#### 5.3.2 IDE 集成
- VS Code 扩展
- Editor 集成

---

## 7. 用户体验对比 (2026)

### 7.1 对话用户友好程度

 | 维度         | Morpheus          | Claude Code          | OpenCode            | Gemini CLI       | Codex           |
 | ----------   | ----------------- | -----------------    | ------------------- | ---------------- | --------------- |
 | **上手难度** | 中等 (需配置 API) | 高 (需订阅)          | 低 (多平台登录)     | 低 (免费)        | 中 (需订阅)     |
 | **交互模式** | TUI + REST API    | 原生 TUI             | TUI + Desktop App   | 原生 TUI         | TUI + App       |
 | **会话恢复** | SQLite + 文件     | Checkpoints          | 事件源同步          | Checkpoints      | 自动保存        |
 | **快捷命令** | 基础              | 丰富 (/btw, /review) | 丰富 (@general)     | 基础             | 丰富 (/model)   |
 | **错误恢复** | 手动              | 自动 Checkpoints     | 分享链接            | Checkpoints      | 自动快照        |
 | **无障碍**   | 需手动安装        | 一键安装             | 多平台              | npx 即用         | 需安装          |

**Morpheus 现状**:
- TUI 交互相对基础
- 无 Checkpoints/自动快照机制
- 会话恢复依赖手动指定 session ID

### 7.2 Token 节省程度

 | 维度           | Morpheus        | Claude Code     | OpenCode        | Gemini CLI    | Codex           |
 | -------------- | --------------- | --------------- | --------------- | ------------- | --------------- |
 | **上下文窗口** | 依赖模型        | 1M (Sonnet)     | 依赖模型        | 1M            | 依赖模型        |
 | **压缩策略**   | 60K tokens      | Auto Memory     | 95% 压缩        | Token 缓存    | Compaction      |
 | **压缩方式**   | 固定阈值        | 自适应          | 百分比          | 智能缓存      | 自动            |
 | **项目感知**   | 无              | CLAUDE.md       | AGENTS.md       | GEMINI.md     | AGENTS.md       |
 | **忽略机制**   | .gitignore      | 自动发现        | 自动 LSP        | .geminiignore | .aiderignore    |

**Morpheus 现状**:
- 固定 60K tokens 压缩阈值，不够智能
- 无项目级上下文文件 (.morpheus.md)
- 无自定义忽略规则

### 7.3 完成工作的好坏程度

 | 维度         | Morpheus        | Claude Code     | OpenCode        | Gemini CLI      | Codex           |
 | ------------ | --------------- | --------------- | --------------- | -------------   | --------------- |
 | **自主能力** | 中等            | 强              | 强              | 中等            | 强              |
 | **工具广度** | 15+ 内置        | MCP + 插件      | 30+ 自定义      | Extensions      | MCP + 插件      |
 | **执行模式** | Build/Plan      | 单一 + Subagent | Build/Plan      | 单一            | Agent 模式切换  |
 | **多任务**   | 6 Agent DAG     | Agent Teams     | 多会话并行      | 并行            | Subagents       |
 | **代码质量** | 依赖模型        | 高 (Opus)       | 高 (Claude)     | 中等            | 高 (GPT-5)      |
 | **调试能力** | 基础            | 强              | 强              | 中等            | 强              |
 | **安全策略** | 4级风险         | Hook Chain      | 权限模式        | Trusted Folders | Approval modes  |

**Morpheus 现状**:
- 工具生态相对单一
- 无插件市场，扩展性有限
- 自主能力中等，适合有经验的开发者

---

## 8. 综合评估

### 8.1 优劣势总结

**Morpheus 优势**:
- 开源 MIT 协议，可自由定制
- 多模型支持 (6+ provider)
- 完整 MCP 客户端实现
- DAG 任务调度
- 工具权限配置灵活
- Go 1.25 高性能后端

**Morpheus 劣势**:
- 上下文管理不如竞品智能
- 无项目级上下文文件
- 生态插件较少
- 无 Checkpoints/自动快照
- IDE 集成缺失
- Windows 支持待完善

### 8.2 改进优先级 (Todo List)

#### P0 - 高优先级 (立即实现)

- [ ] **.morpheus.md 上下文文件**
  - 支持项目级上下文文件 (.morpheus.md)
  - 支持全局级上下文文件 (~/.morpheus.md)
  - 配置项: `morpheus.context_file`

- [ ] **Checkpoints 机制**
  - 自动保存代码状态快照
  - 支持回滚到指定 checkpoint
  - 冷却时间: 每次工具执行后

#### P1 - 中优先级 (下一版本)

- [ ] **Agent Teams**
  - 多会话协调，共享任务和 P2P 消息
  - 支持子 Agent 并行运行
  - 共享上下文管理

- [ ] **远程控制协议**
  - WebSocket 远程连接支持
  - 移动端/浏览器远程控制
  - 认证和会话管理

#### P2 - 低优先级 (规划中)

- [ ] **插件系统增强**
  - 插件市场机制
  - MCP 插件支持
  - 社区 Skills 集成

- [ ] **IDE 集成**
  - VS Code 扩展
  - Editor 集成
  - LSP 深度集成

---

## 9. 结论

Mpheus 当前架构已实现完整的 AI 编码助手核心功能：

  | 功能                | 状态      |
  | ------              | ------    |
  | MCP 协议支持        | ✅ 已实现 |
  | 多 Agent 协调       | ✅ 已实现 |
  | 自定义 Agent 配置   | ✅ 已实现 |
  | Agent Profile (9种) | ✅ 已实现 |
  | 交互式确认机制      | ✅ 已实现 |
  | 策略引擎 (4级风险)  | ✅ 已实现 |
  | Skills 系统 (9内置) | ✅ 已实现 |
  | Lazy Load 机制      | ✅ 已实现 |
  | SQLite 持久化       | ✅ 已实现 |
  | 自定义 Skill 目录   | ✅ 已实现 |

**与竞品的主要差距**:

1. **生态**: 无插件市场/Extensions 生态
2. **上下文**: 无 CLAUDE.md/GEMINI.md 项目上下文文件
3. **协作**: 无 Agent Teams 多会话协调
4. **远程**: 无 Remote Control 协议
5. **平台**: Windows 支持待完善
6. **IDE**: 无官方 Editor 集成

**建议优先实现**:
1. 项目上下文文件机制 (.morpheus.md)
2. Agent Teams 多会话协调
3. 远程控制协议

这两个改进能在较短时间内显著提升用户体验，缩小与竞品的差距。
