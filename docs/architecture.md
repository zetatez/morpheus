# Morpheus 项目架构分析文档

## 一、项目概述

**Morpheus** 是一个本地 AI Agent 运行时，采用 **Go 1.25** 后端 + **TypeScript/Bun (Solid.js)** 前端 TUI 的混合架构。核心定位是为开发者提供可控的 AI 编程助手能力，支持多Agent协作、MCP协议扩展、Session持久化、安全策略控制等企业级特性。

**仓库**: https://github.com/zetatez/morpheus

---

## 二、技术架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                         CLI/TUI Layer                            │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │  Solid.js TUI   │  │   REST API      │  │   REPL Mode     │  │
│  │  (cli/)         │  │   :8080         │  │                 │  │
│  └────────┬────────┘  └────────┬────────┘  └────────┬────────┘  │
└───────────┼────────────────────┼────────────────────┼───────────┘
            │                    │                    │
┌───────────▼────────────────────▼────────────────────▼───────────┐
│                      Application Layer (Go)                       │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                      Runtime (4258行)                      │   │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐            │   │
│  │  │ AgentLoop   │ │ Coordinator│ │ MCP Manager│            │   │
│  │  │ (+Runner)   │ │            │ │            │            │   │
│  │  └────────────┘ └────────────┘ └────────────┘            │   │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐            │   │
│  │  │ ConvManager│ │ SkillLoader│ │SubagentLoad│            │   │
│  │  └────────────┘ └────────────┘ └────────────┘            │   │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐            │   │
│  │  │ PolicyEngine│ │ ToolRegistry│ │ TeamState  │            │   │
│  │  └────────────┘ └────────────┘ └────────────┘            │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
            │                    │                    │
┌───────────▼────────────────────▼────────────────────▼───────────┐
│                      Plugin & Tool Layer                           │
│  ┌────────────────┐ ┌────────────────┐ ┌────────────────┐         │
│  │ Plugin Registry│ │ Tool Registry  │ │ Policy Engine  │         │
│  └────────────────┘ └────────────────┘ └────────────────┘         │
│  ┌──────────────────────────────────────────────────────────┐     │
│  │                    Tool Implementations                   │     │
│  │  fs.*  cmd.*  git.*  web.*  mcp.*  lsp.*  agent.*  skill.*│     │
│  └──────────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────┘
            │
┌───────────▼───────────────────────────────────────────────────────┐
│                      Storage Layer                                 │
│  ┌─────────────────────┐         ┌─────────────────────┐         │
│  │   SQLite (WAL)      │         │   File System       │         │
│  │   sessions.db       │         │   sessions/*.md/*.json│        │
│  └─────────────────────┘         └─────────────────────┘         │
└─────────────────────────────────────────────────────────────────┘
```

---

## 三、技术栈

### 后端 (Go 1.25)

| 依赖 | 用途 |
|------|------|
| `github.com/spf13/cobra` | CLI 框架 |
| `github.com/spf13/viper` | 配置管理 |
| `go.lsp.dev/protocol` | LSP 代码智能 |
| `go.uber.org/zap` | 结构化日志 |
| `modernc.org/sqlite` | SQLite 驱动 |
| `gopkg.in/yaml.v3` | YAML 解析 |
| `github.com/google/uuid` | UUID 生成 |
| `github.com/shirou/gopsutil/v3` | 系统指标 |

### 前端 (TypeScript/Bun)

| 依赖 | 用途 |
|------|------|
| `@opentui/core` | TUI 组件 |
| `@opentui/solid` | Solid.js 集成 |
| `solid-js` | UI 框架 |
| `yargs` | CLI 参数解析 |
| `clipboardy` | 剪贴板访问 |
| `jsdiff` | 差异可视化 |

### 支持的 LLM 提供商

OpenAI, DeepSeek, MiniMax, GLM, Gemini, Anthropic, OpenRouter, Azure, Groq, Mistral, Cohere, TogetherAI, Perplexity, Ollama, LM Studio (本地), OpenAI 兼容接口

---

## 四、核心组件分析

### 4.1 Runtime — 应用核心引擎

**位置**: `internal/app/runtime.go` (4258行)

Runtime是整个系统的心脏，采用**组合模式**聚合了所有子组件：

```go
type Runtime struct {
    cfg              config.Config
    logger           *zap.Logger
    conversation     *convo.Manager
    planner          sdk.Planner
    orchestrator     *execpkg.Orchestrator
    registry         *registry.Registry
    session          *session.Writer
    sessionStore     *session.Store
    plugins          *plugin.Registry
    skills           *skill.Loader
    agentRegistry    *AgentRegistry
    subagents        *subagent.Loader
    // 内存状态
    teamState        *TeamState
    todos            *TodoState
    checkpoints      map[string]*gitCheckpoint
    sessionMemory    *sessionMemoryState
}
```

**初始化链路** (10步串行):

```
newLogger → convo.NewManager → plugin.NewRegistry → loadSoulPrompt
→ skill.NewLoader → mcp.NewManager → registry.NewRegistry
→ buildAvailableTools → policy.NewPolicyEngine → buildPlanner
```

### 4.2 AgentLoop — 迭代执行引擎

**位置**: `internal/app/agent_loop.go` + `agent_runner.go`

核心循环，最多12步迭代，采用**意图分类路由**：

```
AgentLoop
  ├─ normalizeInput()              # 标准化输入，提取附件
  ├─ classifyIntent()              # 意图分类
  │   ├─ simple_chat              # 简短对话，无工具
  │   ├─ lightweight_answer       # 通用知识，无需工具
  │   ├─ fresh_info               # 需实时信息(web.fetch优先)
  │   └─ tool_agent               # 完整工具循环
  ├─ buildMessages()              # 构建消息链
  │   ├─ System Prompt
  │   ├─ Team Context
  │   ├─ Context Documents
  │   ├─ Long-term Memory
  │   ├─ Short-term Memory
  │   ├─ Route System Prompt
  │   ├─ Conversation Summary
  │   └─ User Message
  └─ Loop (maxAgentSteps=12):
      ├─ callChatWithTools()      # 调用LLM
      ├─ extractStructuredOutput() # 提取JSON输出
      ├─ For each tool_call:
      │   ├─ policy.Check()       # 权限检查
      │   ├─ orchestrator.ExecuteStep()
      │   ├─ maybeCheckpoint()     # Git stash快照
      │   ├─ truncateToolResult()
      │   └─ appendMessage()
      ├─ checkAndCompress()       # 上下文压缩
      └─ If finish_reason=="stop": break
```

**Intent Cache**: 每个session缓存32条意图分类结果

### 4.3 Coordinator — 多Agent编排器

**位置**: `internal/app/coordinator.go`

**触发条件**:
- 输入 >= 12单词 且 包含换行/"then"/"and"/"also"
- 或包含 "plan"/"architecture"/"review"

**工作流**:
1. 构建协调者计划（LLM生成JSON任务图）
2. 任务分配角色：implementer, explorer, reviewer, architect, tester, devops, data, security, docs, shell-python-operator
3. 并行或DAG执行（最大6任务，3并发）
4. 汇总结果

**内置 Agent Profiles** (9个):

| Agent | 用途 |
|-------|------|
| `implementer` | 交付具体代码变更 |
| `explorer` | 调查代码库细节 |
| `reviewer` | 评审变更风险 |
| `architect` | 设计高层方案 |
| `tester` | 编写和验证测试 |
| `devops` | 处理部署和 CI/CD |
| `data` | 数据管道工作 |
| `security` | 安全漏洞评审 |
| `docs` | 创建文档 |

**DAG 调度**:
- 支持 `depends_on` 依赖声明
- 自动拓扑排序
- 最大3个并发任务
- 顺序执行有依赖的任务

### 4.4 Orchestrator — 工具执行编排器

**位置**: `internal/exec/orchestrator.go`

单步计划执行，带策略检查：

```go
type Orchestrator struct {
    registry sdk.ToolRegistry
    policy   *policy.Engine
    workdir  string
    plugins  *plugin.Registry
}
```

**执行流程**:

```
ExecuteStep
  ├─ registry.Get(step.Tool) → 获取工具实例
  ├─ checkPolicy() → 策略评估
  │   ├─ cmd.exec → EvaluateCommand(command, workdir)
  │   └─ fs.* → EvaluatePath(tool, path)
  ├─ ApplyToolBefore() → 插件前置钩子
  ├─ tool.Invoke() → 执行工具
  └─ ApplyToolAfter() → 插件后置钩子
```

### 4.5 Policy Engine — 安全策略引擎

**位置**: `internal/policy/engine.go`

4级风险评估体系:

| 等级 | 典型命令 | 处理方式 |
|------|----------|----------|
| `critical` | `dd of=`, `mkfs`, `>:/dev/` | 阻止执行 |
| `high` | `rm -rf`, `curl \| sh`, `chmod 4777` | 需确认 |
| `medium` | `chmod 755`, `kill`, `apt remove` | 需确认 |
| `low` | `pip install`, `npm install` | 可自动批准 |

**保护路径**: `/etc`, `/usr/bin`, `~/.ssh`, `~/.aws` 等系统敏感目录

### 4.6 Tool Registry — 工具注册表

**位置**: `internal/tools/registry/registry.go`

内存工具注册，带 sync.RWMutex。

**内置工具**:

| 工具 | 描述 |
|------|------|
| `fs.read` | 读取文件内容 |
| `fs.write` | 写入文件内容 |
| `fs.edit` | 编辑替换 |
| `fs.glob` | glob 模式匹配 |
| `fs.grep` | 文本搜索 |
| `cmd.exec` | Shell 命令执行 |
| `lsp.query` | LSP 操作 |
| `git.*` | Git 操作 |
| `web.fetch` | 获取网页 |
| `conversation.ask` | 向用户提问 |
| `skill.invoke` | 调用技能 |
| `mcp.query` | MCP 服务器管理 |
| `agent.run` | 运行子 Agent |
| `todo.write` | Todo 管理 |

### 4.7 Skills System — 技能系统

**位置**: `internal/skill/loader.go`

懒加载技能系统，内置9个技能：

`@review`, `@test`, `@docs`, `@refactor`, `@debug`, `@security`, `@git`, `@explain`, `@optimize`

**自定义技能位置** (按优先级):

1. `~/.config/morpheus/skills/`
2. `~/.config/opencode/skills/`
3. `~/.claude/skills/`
4. `~/.agents/skills/`
5. `.morpheus/skills/` (项目级)
6. `.opencode/skills/` (项目级)
7. `.claude/skills/` (项目级)
8. `.agents/skills/` (项目级)

### 4.8 Session Management — 会话管理

**位置**: `internal/session/`

双后端存储：

- **SQLite**: `sessions.db`，WAL 模式，事件溯源
- **File**: Markdown + JSON 文件在 sessions 目录

**内存状态**:
```go
type sessionMemoryState struct {
    shortTerm string  // 工作上下文 (max 4000 bytes)
    longTerm  string  // 持久上下文 (max 12000 bytes)
}
```

**数据库 Schema**:

```sql
sessions(id, created_at, updated_at, summary, metadata_json)
messages(id, session_id, idx, role, content, parts_json, timestamp)
runs(id, session_id, status, created_at, metadata_json)
run_events(id, run_id, seq, type, data_json)
```

### 4.9 MCP Protocol — MCP 协议客户端

**位置**: `internal/tools/mcp/mcp.go`

完整 MCP 客户端，支持三种传输方式：

- **stdio**: 本地进程通信
- **HTTP**: 远程服务器，支持 SSE
- **Auth**: Bearer Token 认证

**工具命名**: `mcp.{server}.{tool}` (e.g., `mcp.filesystem.read`)

### 4.10 Plugin System — 插件系统

**位置**: `internal/plugin/registry.go`

基于钩子的可扩展性：

```go
type Registry struct {
    messageHooks  []MessageHook   # 消息转换
    systemHooks   []SystemHook    # 系统提示转换
    toolBefore    []ToolBeforeHook # 执行前
    toolAfter     []ToolAfterHook  # 执行后
}
```

### 4.11 Checkpoint System — Git快照机制

**位置**: `internal/app/runtime.go:618-740`

基于 `git stash` 的代码状态快照：

- 每次工具执行后可自动创建checkpoint
- 支持回滚到指定checkpoint
- 使用 `git stash push -u` 保留未跟踪文件

---

## 五、上下文管理

### 5.1 Token阈值常量

```go
const (
    MaxHistoryTokens        = 60000   // 最大历史token
    CompactionReserveTokens = 20000   // 压缩摘要预留
    PruneMinimumTokens     = 20000   // 最小修剪token
    PruneProtectTokens     = 40000   // 修剪保护token
    CompactionCooldown     = 2 * time.Minute
    CompactionTriggerRatio = 0.95     // 95%
)
```

### 5.2 两阶段压缩

1. **Prune**: 当 total > 40000 tokens 时，移除已完成非失败步骤的工具输出
2. **Compress**: 当 remaining > 57000 tokens (95% of 60000) 时，总结最旧消息

### 5.3 消息构建顺序

1. System prompt
2. Team shared context
3. Context documents
4. Long-term memory
5. Short-term memory
6. Route-specific system prompt (tool agent vs lightweight)
7. Conversation summary
8. User message

---

## 六、REST API

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/v1/chat` | 与 Agent 聊天 |
| POST | `/api/v1/plan` | 生成计划 |
| POST | `/api/v1/execute` | 执行计划 |
| POST | `/api/v1/tasks` | 创建任务 |
| GET | `/api/v1/tasks/{id}` | 获取任务状态 |
| GET | `/api/v1/sessions` | 列出会话 |
| GET | `/api/v1/sessions/{id}` | 获取会话 |
| GET | `/api/v1/skills` | 列出技能 |
| POST | `/api/v1/models/select` | 切换模型 |
| GET | `/api/v1/models` | 列出模型 |
| POST | `/repl` | REPL 端点 |
| POST | `/repl/stream` | 流式 REPL |

---

## 七、Agent 类型

### 7.1 内置 Agent Profiles

| Profile | 工具权限 | 用途 |
|---------|----------|------|
| `implementer` | 全部 | 交付代码变更 |
| `explorer` | 只读 | 调查代码库 |
| `reviewer` | 只读 | 代码评审 |
| `architect` | 只读 | 架构设计 |
| `tester` | 写文件 | 测试编写 |
| `devops` | shell | 部署CI/CD |
| `data` | 数据工具 | 数据管道 |
| `security` | 只读 | 安全评审 |
| `docs` | 写文件 | 文档编写 |

### 7.2 自定义 Subagent

从 `SUBAGENT.md` 文件加载 YAML frontmatter 定义：

```yaml
name: CustomAgent
description: Description
tools: [fs.read, cmd.exec]
---
Instructions here...
```

---

## 八、配置管理

**位置**: `config.yaml`

```yaml
workspace_root: ./

logging:
  level: info
  file: ~/.local/share/morpheus/logs/morpheus.log

planner:
  provider: deepseek
  model: deepseek-chat
  temperature: 0.2
  api_key: ${DEEPSEEK_API_KEY}

server:
  listen: :8080
  remote:
    enabled: true
  limits:
    enabled: true
    max_cpu_percent: 85
    max_memory_percent: 85

session:
  path: ~/.local/share/morpheus/sessions
  sqlite_path: ~/.local/share/morpheus/sessions.db
  retention: 720h

mcp:
  servers:
    - name: filesystem
      transport: stdio
      command: npx -y @modelcontextprotocol/server-filesystem .

permissions:
  confirm_above: high
  confirm_protected_paths: [...]
  risk_factors: {...}
  auto_approve: false

subagents:
  max_concurrent_tasks: 3
```

**配置优先级**:

1. `./config.yaml`
2. `~/.config/morpheus/config.yaml`
3. `~/.morpheus/config.yaml`

---

## 九、已改进问题

### 9.1 Runtime 单点问题 ✅ 已优化

- 引入 `RuntimeOption` 选项模式，支持依赖注入
- 便于测试时替换实现（`WithPlanner`, `WithOrchestrator` 等）
- 文件: `internal/app/lazy.go`

### 9.2 初始化链路过重 ✅ 已优化

- 引入 `Lazy[T]` 泛型，支持按需初始化
- 使用 `sync.Once` 保证线程安全
- 文件: `internal/app/lazy.go`

### 9.3 并发模型简单 ✅ 已优化

- 引入 `WorkerPool`，可配置并发数
- 工具调用支持并行执行
- Coordinator 并发上限可配置化 (`max_concurrent_tasks`)
- 文件: `internal/exec/worker_pool.go`, `internal/app/coordinator.go`

### 9.4 会话管理耦合 ✅ 已优化

- 抽象 `SessionStore` 接口，职责边界清晰
- 支持多种存储后端替换（SQLite/File）
- SQLite 连接池管理（`NewStoreWithPool`）
- 文件: `internal/session/store.go`, `internal/session/store_sqlite.go`

### 9.5 错误处理不一致 ✅ 已优化

- 引入 `Result[T]` 统一结果类型
- `ToolError` 提供标准化错误信息
- `ExecutionMetrics` 统一执行指标
- 文件: `pkg/sdk/result.go`

### 9.6 配置管理分散 ✅ 已优化

- `ValidateAll()` 提供全量配置验证
- 各子配置独立验证函数
- 文件: `internal/config/validation.go`

### 9.7 配置热更新机制 ✅ 已优化

**文件**: `internal/config/hotreload.go`

支持 fsnotify 文件监控 + 定时轮询两种模式。

---

## 十、总结评分

| 维度 | 评分 | 说明 |
|------|------|------|
| **架构设计** | 9/10 | RuntimeBuilder + Lazy + Option 模式，扩展性大幅提升 |
| **安全性** | 9/10 | Policy Engine 设计优秀，多层防护 |
| **可维护性** | 9/10 | 统一结果类型 + 存储抽象 + 热更新，接口清晰 |
| **性能** | 9/10 | Worker Pool + SQLite 连接池，可配置化 |
| **可扩展性** | 9/10 | 插件/工具/技能系统 + 依赖注入 |
| **工程化** | 9/10 | 配置验证 + 可观测性 + 热更新 |
| **总计** | **9.0/10** | 已完成所有优化项，整体质量接近生产级 |

---

## 附录：关键文件索引

| 组件 | 文件路径 |
|------|----------|
| Runtime | `internal/app/runtime.go` |
| Runtime Builder | `internal/app/runtime_builder.go` |
| Runtime Options (DI) | `internal/app/lazy.go` |
| Agent Loop | `internal/app/agent_loop.go` |
| Agent Runner | `internal/app/agent_runner.go` |
| Coordinator | `internal/app/coordinator.go` |
| Team State | `internal/app/agent_team.go` |
| Orchestrator | `internal/exec/orchestrator.go` |
| Worker Pool | `internal/exec/worker_pool.go` |
| Policy Engine | `internal/policy/engine.go` |
| Tool Registry | `internal/tools/registry/registry.go` |
| MCP | `internal/tools/mcp/mcp.go` |
| Skills | `internal/skill/loader.go` |
| Subagents | `internal/subagent/loader.go` |
| Session Writer | `internal/session/writer.go` |
| Session Store | `internal/session/store.go` |
| Session SQLite | `internal/session/store_sqlite.go` |
| Plugin | `internal/plugin/registry.go` |
| Conversation | `internal/convo/manager.go` |
| Todos | `internal/app/todos.go` |
| Config | `internal/config/` |
| Config Validation | `internal/config/validation.go` |
| Config Hot Reload | `internal/config/hotreload.go` |
| SDK Types | `pkg/sdk/types.go` |
| SDK Result | `pkg/sdk/result.go` |
| CLI | `internal/cli/` |
