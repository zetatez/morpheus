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
│  │  │ AgentLoop  │ │ Coordinator│ │ MCP Manager│            │   │
│  │  └────────────┘ └────────────┘ └────────────┘            │   │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐            │   │
│  │  │  Planner   │ │ConvoManager│ │Skill Loader│            │   │
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
│  │   sessions.db       │         │   sessions/*.json   │         │
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
}
```

**初始化链路** (10步串行):

```
newLogger → convo.NewManager → plugin.NewRegistry → loadSoulPrompt
→ skill.NewLoader → mcp.NewManager → registry.NewRegistry
→ buildAvailableTools → policy.NewPolicyEngine → buildPlanner
```

### 4.2 AgentLoop — 迭代执行引擎

**位置**: `internal/app/agent_loop.go`

核心循环，最多12步迭代：

```
AgentLoop
  ├─ getPendingConfirmation()    # 待确认操作
  ├─ preprocessSkillCommand()     # @skill 命令预处理
  ├─ appendMessage()              # 追加用户消息
  ├─ allowMentionedSkills()       # 解析并启用引用的技能
  ├─ checkAndCompress()           # 60k token 上下文压缩
  ├─ collectToolSpecs()           # 收集可用工具
  ├─ buildAgentMessages()         # 构建完整消息列表
  ├─ maybeCoordinate()            # 多Agent协调检测
  └─ Loop (maxAgentSteps=12):
      ├─ callChatWithTools()      # 调用LLM
      ├─ extractStructuredOutput() # 提取JSON输出
      ├─ For each tool_call:
      │   ├─ normalizeToolName()
      │   ├─ orchestrator.ExecuteStep()
      │   ├─ truncateToolResult()
      │   └─ appendMessage()
      └─ If finish_reason=="stop": break
```

**Agent 运行模式**:

- `build` - 完全访问权限（执行命令、写入文件）
- `plan` - 只读模式（仅生成计划）

### 4.3 Coordinator — 多Agent编排器

**位置**: `internal/app/coordinator.go`

**触发条件**:

- 输入 >= 12单词 且 包含换行/"then"/"and"/"also"
- 或包含 "plan"/"architecture"/"review"

**内置 Agent Profiles** (10个):

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
| `shell-python-operator` | Shell 管道和 Python 脚本 |

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
  │   └─ fs.* → EvaluateCommand(tool, path)
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

**自定义技能位置**:

- `~/.config/morpheus/skills/`
- `.morpheus/skills/`

### 4.8 Session Management — 会话管理

**位置**: `internal/session/`

双后端存储：

- **SQLite**: `sessions.db`，WAL 模式
- **File**: JSON 文件在 sessions 目录

**数据库 Schema**:

```sql
sessions(id, created_at, updated_at, summary, metadata_json)
messages(id, session_id, idx, role, content, parts_json, timestamp)
```

### 4.9 MCP Protocol — MCP 协议客户端

**位置**: `internal/tools/mcp/mcp.go`

完整 MCP 客户端，支持三种传输方式：

- **stdio**: 本地进程通信
- **HTTP**: 远程服务器，支持 SSE
- **Auth**: Bearer Token 认证

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

---

## 五、配置管理

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
```

**配置优先级**:

1. `./config.yaml`
2. `~/.config/morpheus/config.yaml`
3. `~/.morpheus/config.yaml`

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

## 七、优势分析

### 7.1 架构设计优秀

1. **清晰的关注点分离**
   - Runtime 作为协调者，各子组件职责单一
   - Tool Registry 模式便于扩展
   - Plugin Hook 机制支持无侵入增强

2. **多层次的可扩展性**
   - Tools: 实现 `sdk.Tool` 接口即可添加新工具
   - Plugins: 消息、系统提示、工具前后钩子
   - Skills: 懒加载的技能系统
   - Subagents: 自定义 Agent profile

3. **安全第一的设计**
   - Policy Engine 实现细粒度风险评估
   - Protected paths 保护系统敏感区域
   - 分级确认机制 (confirm_above 配置)

### 7.2 功能完整性

1. **MCP 协议完整支持** — stdio/HTTP/SSE 三种传输方式
2. **多 LLM Provider 支持** — 覆盖 OpenAI 系、本地 Ollama、SambaNova 等 20+ 提供商
3. **混合存储** — SQLite WAL + JSON 文件，双重保障
4. **智能上下文压缩** — 60k token 阈值自动触发
5. **DAG 任务调度** — 多 Agent 协作有依赖管理

### 7.3 工程化水平

1. **配置驱动** — Viper 支持多源配置和环境变量
2. **结构化日志** — zap logger 带上下文
3. **错误处理规范** — 统一的错误类型和传播
4. **TUI + API 双入口** — 兼顾交互和程序化使用

---

## 八、劣势分析

### 8.1 Runtime 单点问题

```go
// internal/app/runtime.go:1-200 (4258行总长度)
type Runtime struct {
    // 12个核心组件全部在Runtime中直接实例化
    conversation *convo.Manager
    planner      sdk.Planner
    orchestrator *execpkg.Orchestrator
    // ... 全部是具体类型，非接口
}
```

**问题**:

- 4258行的 Runtime 类违背了**单一职责原则**
- 所有组件直接在 Runtime 内部 `new`，无法单独测试
- 接口依赖不足，难以替换实现（如换用不同的 planner 实现）

### 8.2 初始化链路过重

```
newLogger → convo.NewManager → plugin.NewRegistry → loadSoulPrompt
→ skill.NewLoader → mcp.NewManager → registry.NewRegistry
→ buildAvailableTools → policy.NewPolicyEngine → buildPlanner
```

10步串行初始化，即使某些功能不用也会初始化（如 MCP Manager）。

### 8.3 并发模型简单

```go
// internal/app/agent_loop.go
for step := 0; step < maxAgentSteps; step++ {
    // 串行执行每个 tool call
    for _, tc := range toolCalls {
        result := o.ExecuteStep(...)
    }
}
```

- AgentLoop 内完全串行
- Coordinator 的并发任务上限硬编码为3
- 无协程池复用，频繁创建销毁 goroutine

### 8.4 会话管理耦合

```go
// internal/session/writer.go & store.go
type Writer struct { db *sql.DB }
type Store struct { ... }
```

- Writer 和 Store 职责边界模糊
- 事务边界不清晰
- 无连接池管理

### 8.5 错误处理不一致

各工具的错误返回形式不统一：

- 有的返回 error
- 有的返回空 result + error
- 缺少统一的 `Result` 类型

### 8.6 配置管理分散

- 部分配置硬编码在代码中（如 maxAgentSteps=12, 60k token 阈值）
- 配置项缺少 schema 验证
- 无配置热更新机制

---

## 九、优化建议

### 9.1 重构 Runtime — 引入依赖注入

```go
// 优化方向：使用选项模式 + 接口依赖
type RuntimeOption func(*Runtime)

type PlannerProvider interface {
    GetPlanner(cfg config.PlannerConfig) (sdk.Planner, error)
}

func NewRuntime(opts ...RuntimeOption) (*Runtime, error) {
    r := &Runtime{}
    for _, opt := range opts {
        opt(r)
    }
    return r, nil
}

// 便于测试时替换实现
func WithPlanner(p sdk.Planner) RuntimeOption {
    return func(r *Runtime) { r.planner = p }
}
```

### 9.2 懒加载所有组件

```go
// 当前：10步串行初始化
// 优化：按需初始化
type Lazy[T any] struct {
    init func() T
    val  T
    once sync.Once
}

func (l *Lazy[T]) Get() T {
    l.once.Do(func() { l.val = l.init() })
    return l.val
}
```

### 9.3 引入 Worker Pool 并发模型

```go
// 优化 AgentLoop 内的 tool call 并发
func (r *Runtime) executeToolCalls(ctx context.Context, calls []ToolCall) {
    pool := make(chan struct{}, MaxConcurrentTools) // 可配置并发数
    var wg sync.WaitGroup

    for _, call := range calls {
        wg.Add(1)
        go func(c ToolCall) {
            defer wg.Done()
            pool <- struct{}{}
            defer func() { <-pool }()
            r.orchestrator.ExecuteStep(ctx, c)
        }(call)
    }
    wg.Wait()
}
```

### 9.4 统一结果类型

```go
// 建立统一的 Tool Result 抽象
type Result[T any] struct {
    Value   T
    Error   *ToolError
    Metrics ExecutionMetrics
}

type ToolError struct {
    Code      ErrorCode
    Message   string
    Retryable bool
}
```

### 9.5 配置集中 + Schema 验证

```go
// 硬编码值抽取到配置
type AgentConfig struct {
    MaxSteps          int `validate:"min=1,max=100"`
    ContextTokenLimit int `validate:"min=1000"`
    MaxConcurrent     int `validate:"min=1,max=10"`
}

// 使用 viper + JSON Schema 验证
func LoadConfig(path string) (*Config, error) {
    viper.SetConfigFile(path)
    // 注册默认值
    // 启用毒性检测
}
```

### 9.6 指标与可观测性

```go
// 添加 OpenTelemetry 支持
import "go.opentelemetry.io/otel"

func (r *Runtime) ExecuteStep(ctx context.Context, step *Step) {
    ctx, span := otel.Tracer("morpheus").Start(ctx, "ExecuteStep",
        trace.WithAttributes(attribute.String("tool", step.Tool)))
    defer span.End()
    // ...
}
```

### 9.7 会话存储抽象

```go
// 抽象存储接口，支持替换
type SessionStore interface {
    Save(ctx context.Context, s *Session) error
    Get(ctx context.Context, id string) (*Session, error)
    List(ctx context.Context, filter *SessionFilter) ([]*Session, error)
}

// 默认实现仍为 SQLite，但可替换为 Redis/PostgreSQL 等
type SQLiteStore struct{ db *sql.DB }
```

---

## 十、总结评分

| 维度 | 评分 | 说明 |
|------|------|------|
| **架构设计** | 8/10 | 模块划分清晰，扩展点充足，但 Runtime 过于庞大 |
| **安全性** | 9/10 | Policy Engine 设计优秀，多层防护 |
| **可维护性** | 6/10 | 4258行 Runtime 难以维护，接口依赖不足 |
| **性能** | 7/10 | 并发模型简单，连接池缺失 |
| **可扩展性** | 8/10 | 插件/工具/技能系统设计良好 |
| **工程化** | 7/10 | 结构化日志/配置驱动，但缺少单元测试框架 |
| **总计** | **7.5/10** | 整体优秀，Runtime 重构是首要优化点 |

---

## 附录：关键文件索引

| 组件 | 文件路径 |
|------|----------|
| Runtime | `internal/app/runtime.go` |
| AgentLoop | `internal/app/agent_loop.go` |
| Coordinator | `internal/app/coordinator.go` |
| Orchestrator | `internal/exec/orchestrator.go` |
| Policy Engine | `internal/policy/engine.go` |
| Tool Registry | `internal/tools/registry/registry.go` |
| Session | `internal/session/` |
| Skills | `internal/skill/loader.go` |
| MCP | `internal/tools/mcp/mcp.go` |
| Plugin | `internal/plugin/registry.go` |
| Config | `internal/config/` |
| CLI | `internal/cli/` |
