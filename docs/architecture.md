# BruteCode 工具架构文档

BruteCode 是一个 AI 代码助手，采用模块化架构设计，支持多模型 planner、灵活的 tool 扩展和安全策略控制。

## 1. 系统架构总览

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLI / API Server                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                   Runtime                                    │
│  ┌──────────────┐  ┌─────────────┐  ┌────────────┐  ┌──────────────────┐   │
│  │ Conversation │  │   Planner   │  │ Orchestrator│  │    Plugin        │   │
│  │   Manager    │  │             │  │             │  │    Registry      │   │
│  └──────────────┘  └─────────────┘  └────────────┘  └──────────────────┘   │
│         │                 │               │                  │              │
│         └─────────────────┴───────────────┴──────────────────┘              │
│                                    │                                         │
│                     ┌──────────────┴──────────────┐                         │
│                     │       Tool Registry         │                         │
│                     │  (fs/cmd/agent/skill/...)   │                         │
│                     └─────────────────────────────┘                         │
│                                    │                                         │
│         ┌─────────────────────────┼─────────────────────────┐              │
│         │             Policy Engine (Security)                │              │
│         └───────────────────────────────────────────────────────┘              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 2. 核心组件

### 2.1 Runtime (`internal/app/runtime.go`)

Runtime 是整个系统的主入口，负责组件的初始化和组装。

```go
type Runtime struct {
    cfg           config.Config      // 配置
    logger        *zap.Logger        // 日志
    conversation  *convo.Manager     // 会话管理
    planner       sdk.Planner        // 规划器
    orchestrator  *exec.Orchestrator // 执行器
    registry      *registry.Registry // 工具注册表
    session       *session.Writer    // 持久化
    plugins       *plugin.Registry   // 插件系统
    skills        *skill.Loader      // 技能加载器
}
```

**初始化流程**:
```
NewRuntime()
  ├─ newLogger()           → 初始化 zap 日志
  ├─ convo.NewManager()   → 创建会话管理器
  ├─ plugin.NewRegistry() → 创建插件注册表
  ├─ loadSoulPrompt()     → 加载 SOUL.md 系统提示
  ├─ skill.NewLoader()    → 初始化技能加载器
  ├─ registry.NewRegistry() → 创建工具注册表
  ├─ buildAvailableTools() → 注册所有内置工具
  ├─ policy.NewPolicyEngine() → 创建策略引擎
  ├─ buildPlanner()       → 创建 planner (openai/keyword)
  └─ newAuditWriter()     → 创建审计日志
```

### 2.2 AgentLoop (`internal/app/agent_loop.go`)

AgentLoop 实现迭代式 agent 执行，是核心的业务逻辑处理层。

```go
func (rt *Runtime) AgentLoop(ctx, sessionID, input string, format *OutputFormat) (Response, error)
```

**执行流程**:
```
AgentLoop
  │
  ├─ appendMessage()     → 添加用户消息到会话
  ├─ allowMentionedSkills() → 解析并允许引用的技能
  ├─ checkAndCompress()  → 检查并压缩历史消息
  ├─ collectToolSpecs()  → 收集可用工具定义
  ├─ buildAgentMessages() → 构建完整的消息列表
  │
  └─ Loop (maxAgentSteps = 12):
      │
      ├─ callChatWithTools() → 调用 LLM with tools
      │
      ├─ if format == json_schema:
      │   └─ extractStructuredOutput() → 提取结构化输出
      │
      ├─ for each tool call:
      │   ├─ normalizeToolName() → 规范化工具名
      │   ├─ orchestrator.ExecuteStep() → 执行工具
      │   ├─ formatToolResultContent() → 格式化结果
      │   └─ appendMessage() → 添加 assistant 消息
      │
      ├─ if resp.FinishReason == "stop":
      │   └─ break loop
      │
      └─ continue next iteration
```

### 2.3 Orchestrator (`internal/exec/orchestrator.go`)

Orchestrator 负责单步 plan 的执行，包含策略检查和插件钩子。

```go
type Orchestrator struct {
    registry sdk.ToolRegistry  // 工具注册表
    policy   *policy.Engine    // 策略引擎
    workdir  string           // 工作目录
    plugins  *plugin.Registry  // 插件系统
}

func (o *Orchestrator) ExecuteStep(ctx, sessionID string, step sdk.PlanStep) (sdk.ToolResult, error)
```

**执行流程**:
```
ExecuteStep
  │
  ├─ registry.Get(step.Tool) → 获取工具实例
  │
  ├─ checkPolicy(ctx, step)  → 策略检查
  │   ├─ cmd.exec: EvaluateCommand(command, workdir)
  │   └─ fs.read/write: EvaluateCommand(tool, path)
  │
  ├─ if step.Tool == "skill.invoke":
  │   └─ inputs["session_id"] = sessionID
  │
  ├─ plugins.ApplyToolBefore() → 工具执行前钩子
  │
  ├─ tool.Invoke(ctx, inputs) → 调用工具
  │
  ├─ plugins.ApplyToolAfter() → 工具执行后钩子
  │
  └─ return result
```

## 3. 工具系统

### 3.1 Tool 接口 (`pkg/sdk/interfaces.go`)

```go
type Tool interface {
    Name()    string                      // 工具名称
    Describe() string                      // 工具描述
    Schema()  map[string]any              // 参数 schema
    Invoke(ctx context.Context, input map[string]any) (ToolResult, error)
}
```

### 3.2 ToolRegistry (`internal/tools/registry/registry.go`)

工具注册中心，提供工具的注册和查找。

```go
type Registry struct {
    mu    sync.RWMutex
    tools map[string]sdk.Tool
}

func (r *Registry) Register(tool sdk.Tool) error
func (r *Registry) Get(name string) (sdk.Tool, bool)
func (r *Registry) All() []sdk.Tool
```

### 3.3 内置工具

| 工具名称 | 实现文件 | 功能描述 |
|---------|---------|---------|
| `agent.run` | `internal/tools/agenttool/agent.go` | 运行子 agent |
| `fs.read` | `internal/tools/fs/fs.go` | 读取文件 |
| `fs.write` | `internal/tools/fs/fs.go` | 写入文件 |
| `fs.edit` | `internal/tools/fs/fs.go` | 编辑文件 |
| `fs.glob` | `internal/tools/fs/fs.go` | glob 匹配 |
| `fs.grep` | `internal/tools/fs/fs.go` | 文本搜索 |
| `cmd.exec` | `internal/tools/cmd/cmd.go` | 执行命令 |
| `skill.invoke` | `internal/tools/skilltool/skill.go` | 调用技能 |
| `lsp.codebase` | `internal/tools/lsp/lsp.go` | LSP 代码理解 |
| `conversation.ask` | `internal/tools/ask/question.go` | 向用户提问 |
| `web.fetch` | `internal/tools/webfetch/webfetch.go` | 网页抓取 |
| `respond.echo` | `internal/tools/respond/echo.go` | 直接响应 |

## 4. Planner 系统

### 4.1 Planner 接口

```go
type Planner interface {
    ID() string
    Capabilities() []string
    Plan(ctx context.Context, req PlanRequest) (Plan, error)
}
```

### 4.2 OpenAI Planner (`internal/planner/openai/openai.go`)

支持多 provider: openai, glm, minmax, deepseek, gemini

```go
type Planner struct {
    model      string
    temp       float64
    system     string
    endpoint   string
    apiKey     string
    provider   string
}
```

**规划流程**:
```
Planner.Plan()
  │
  ├─ 构建 system prompt (工具说明)
  ├─ 构建 user prompt (任务 + context)
  │
  ├─ http call to LLM API
  │
  └─ 解析响应返回 Plan
      Plan {
          Summary: string
          Steps: []PlanStep {
              ID: string
              Description: string
              Tool: string
              Inputs: map[string]any
              Status: StepStatus
          }
      }
```

### 4.3 Keyword Planner (`internal/planner/keyword/keyword.go`)

基于关键词的简单 planner，适用于无 API Key 场景。

## 5. 安全策略

### 5.1 Policy Engine (`internal/policy/engine.go`)

```go
type Engine struct {
    cfg                 config.Config
    compiledRiskFactors []compiledFactor  // 风险因子正则
    compiledPaths       []compiledPath    // 受保护路径
}
```

**策略评估**:
```
EvaluateCommand(ctx, command, workdir)
  │
  ├─ evaluateCommand(command) → 匹配风险因子
  │   RiskFactors: map[string][]string
  │   - critical: rm -rf, :(){ :|:& };:
  │   - high: sudo, chmod 777
  │   - medium: curl, wget
  │
  ├─ evaluatePath(workdir) → 匹配保护路径
  │   ConfirmProtectedPaths: []
  │
  └─ 返回 PolicyDecision
      PolicyDecision {
          Allowed: bool
          RiskLevel: RiskLevel
          RequiresConfirm: bool
          Reason: string
      }
```

### 5.2 风险等级

| 等级 | 说明 |
|-----|------|
| `low` | 安全操作，默认允许 |
| `medium` | 需确认后执行 |
| `high` | 需明确确认 |
| `critical` | 拒绝执行 |

## 6. 会话管理

### 6.1 Conversation Manager (`internal/convo/manager.go`)

内存会话管理，支持消息追加、历史压缩。

```go
type Manager struct {
    mu           sync.RWMutex
    sessions     map[string]*Session  // sessionID -> Session
    systemPrompt string                // 系统提示词
}

type Session struct {
    Messages []Message  // 消息列表
    Summary  string     // 压缩后的摘要
}
```

**压缩策略**:
- 超过 32 条消息时触发压缩
- 保留最近 32 条消息 + 生成摘要

### 6.2 Session Writer (`internal/session/writer.go`)

持久化会话数据到磁盘。

```go
type Writer struct {
    sessionPath string
    retention   time.Duration
}

// 文件结构:
sessionID/
  ├── conversation.raw.md   // 完整对话
  ├── summary.md           // 压缩摘要
  ├── tool-output-*.txt     // 工具输出
  └── session.meta.json    // 元数据
```

## 7. 插件系统

### 7.1 Plugin Registry (`internal/plugin/registry.go`)

基于钩子的扩展机制。

```go
type Registry struct {
    mu           sync.RWMutex
    messageHooks []MessageHook    // 消息处理钩子
    systemHooks  []SystemHook     // 系统提示钩子
    toolBefore   []ToolBeforeHook // 工具前钩子
    toolAfter    []ToolAfterHook  // 工具后钩子
}
```

**钩子类型**:
- `MessageHook`: 消息发送前/后处理
- `SystemHook`: 系统提示词修改
- `ToolBeforeHook`: 工具输入预处理
- `ToolAfterHook`: 工具输出后处理

## 8. 配置系统

### 8.1 Config (`internal/config/config.go`)

```go
type Config struct {
    WorkspaceRoot string              // 工作目录
    Logging       LoggingConfig       // 日志配置
    Planner       PlannerConfig       // Planner 配置
    Server        ServerConfig       // 服务配置
    Session       SessionConfig       // 会话配置
    KnowledgeBase KnowledgeBaseConfig // 知识库
    Permissions   Permissions         // 权限配置
}
```

### 8.2 配置加载流程

```
Load(configPath)
  │
  ├─ viper.ReadInConfig() → 读取 YAML
  ├─ cfg.expandPaths() → 展开路径
  ├─ cfg.loadAPIKeyFromEnv() → 从环境变量加载 API Key
  └─ cfg.Validate() → 验证配置
```

## 9. 架构流程图

### 9.1 完整请求处理流程

```
┌─────────┐     ┌─────────────┐     ┌──────────────┐     ┌───────────────┐
│  User   │────▶│    CLI/     │────▶│   Runtime    │────▶│  AgentLoop    │
│ Input   │     │  API Server │     │  (Init)      │     │  (Execute)    │
└─────────┘     └─────────────┘     └──────────────┘     └───────────────┘
                                                                   │
                                                                   ▼
                            ┌──────────────────────────────────────────────┐
                            │              Planner (LLM)                   │
                            │  ┌────────────────────────────────────────┐  │
                            │  │  1. Build System Prompt (tools)       │  │
                            │  │  2. Build User Prompt (task + context) │  │
                            │  │  3. HTTP Call to LLM API               │  │
                            │  │  4. Parse Plan (steps)                 │  │
                            │  └────────────────────────────────────────┘  │
                            └──────────────────────────────────────────────┘
                                           │
                    ┌──────────────────────┼──────────────────────┐
                    ▼                      ▼                      ▼
          ┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐
          │   ExecuteStep   │   │   ExecuteStep   │   │   ExecuteStep   │
          │   (Tool #1)     │   │   (Tool #2)     │   │   (Tool #N)     │
          └────────┬────────┘   └────────┬────────┘   └────────┬────────┘
                   │                      │                      │
                   ▼                      ▼                      ▼
          ┌───────────────────────────────────────────────────────────────┐
          │                    Orchestrator                             │
          │  1. Get Tool from Registry                                   │
          │  2. Policy Check (cmd/fs)                                    │
          │  3. Plugin Hook (Before)                                     │
          │  4. Tool.Invoke()                                             │
          │  5. Plugin Hook (After)                                      │
          └───────────────────────────────────────────────────────────────┘
                   │
                   ▼
          ┌─────────────────┐
          │  Return Result  │
          │  to AgentLoop   │
          └────────┬────────┘
                   │
                   ▼
          ┌───────────────────────────────────────────────────────────────┐
          │  AgentLoop: Append result → Call LLM again (loop)           │
          └───────────────────────────────────────────────────────────────┘
                   │
                   ▼                              ┌─────────────────────┐
          ┌─────────────────┐                   │  Session Writer     │
          │  Return Response │──────────────────▶│  (Persist to disk)  │
          └─────────────────┘                   └─────────────────────┘
```

### 9.2 工具执行详细流程

```
┌─────────────────────────────────────────────────────────────┐
│                   ExecuteStep(sdk.PlanStep)                 │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
              ┌──────────────────────────────┐
              │  registry.Get(step.Tool)     │
              │  → sdk.Tool                  │
              └──────────────┬───────────────┘
                             │
                             ▼
              ┌──────────────────────────────┐
              │  policy.checkPolicy()        │
              │  → Allowed?                  │
              └──────────────┬───────────────┘
                             │
              ┌──────────────┴───────────────┐
              ▼                               ▼
        ┌─────────────┐               ┌─────────────┐
        │   Allowed   │               │  Rejected   │
        └──────┬──────┘               └─────────────┘
               │
               ▼
        ┌───────────────────────────────────┐
        │  plugins.ApplyToolBefore()       │
        │  → input = hook(ctx, input)      │
        └───────────────┬─────────────────┘
                         │
                         ▼
        ┌───────────────────────────────────┐
        │  tool.Invoke(ctx, input)          │
        │  → sdk.ToolResult                 │
        └───────────────┬─────────────────┘
                         │
                         ▼
        ┌───────────────────────────────────┐
        │  plugins.ApplyToolAfter()         │
        │  → result = hook(ctx, result)    │
        └───────────────┬─────────────────┘
                         │
                         ▼
        ┌───────────────────────────────────┐
        │  return sdk.ToolResult            │
        └───────────────────────────────────┘
```

### 9.3 配置初始化流程

```
┌─────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│   config.yaml  │────▶│  config.Load()   │────▶│  NewRuntime()    │
└─────────────────┘     └────────┬─────────┘     └────────┬─────────┘
                                 │                         │
                                 ▼                         ▼
                        ┌──────────────────┐       ┌──────────────────┐
                        │  Viper + Validate │       │  Wire Components │
                        │  - expandPaths    │       │  - Logger        │
                        │  - loadAPIKeyEnv  │       │  - Conversation  │
                        │  - Permission    │       │  - Planner       │
                        └──────────────────┘       │  - Registry       │
                                                   │  - Policy         │
                                                   │  - Plugins        │
                                                   └──────────────────┘
```

## 10. 扩展指南

### 10.1 添加新工具

1. 实现 `sdk.Tool` 接口:
```go
type MyTool struct { ... }

func (t *MyTool) Name() string    { return "my.tool" }
func (t *MyTool) Describe() string { return "Description" }
func (t *MyTool) Schema() map[string]any { ... }
func (t *MyTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) { ... }
```

2. 在 `buildAvailableTools()` 中注册:
```go
fstool.NewMyTool(cfg.WorkspaceRoot)
```

### 10.2 添加新 Planner

1. 实现 `sdk.Planner` 接口
2. 在 `buildPlanner()` 中添加 case

### 10.3 添加插件钩子

1. 在 `plugin.Registry` 中添加新钩子类型
2. 在对应位置调用钩子函数

## 11. 目录结构

```
internal/
├── app/                    # 主应用逻辑
│   ├── runtime.go          # Runtime 主入口
│   ├── agent_loop.go       # Agent 循环执行
│   ├── agent_loop_stream.go# 流式执行
│   └── SOUL.go             # 系统提示加载
├── planner/                # 规划器
│   ├── openai/             # OpenAI compatible planner
│   └── keyword/            # 关键词 planner
├── exec/                   # 执行器
│   └── orchestrator.go     # 步骤执行编排
├── tools/                  # 工具实现
│   ├── registry/           # 工具注册表
│   ├── fs/                 # 文件系统工具
│   ├── cmd/                # 命令执行工具
│   ├── agenttool/          # Agent 工具
│   ├── skilltool/         # 技能工具
│   ├── ask/                # 问答工具
│   ├── webfetch/           # 网页抓取
│   ├── respond/            # 响应工具
│   └── lsp/                # LSP 工具
├── convo/                  # 会话管理
│   └── manager.go         # 内存会话管理
├── session/                # 持久化
│   └── writer.go          # 会话写入器
├── policy/                 # 策略引擎
│   └── engine.go          # 策略评估
├── plugin/                 # 插件系统
│   └── registry.go       # 钩子注册表
├── skill/                  # 技能系统
│   └── loader.go         # 技能加载
├── config/                 # 配置系统
│   └── config.go          # 配置加载
├── modelstore/            # 模型存储
│   └── store.go           # 模型配置
└── cli/                   # CLI 命令
    ├── root.go            # 根命令
    ├── serve.go           # API 服务
    └── repl.go            # REPL 模式
```

## 12. 技术栈

- **语言**: Go 1.21+
- **LLM SDK**: OpenAI API compatible (支持 glm/minmax/deepseek/gemini)
- **日志**: zap
- **配置**: viper + YAML
- **Web 框架**: 标准库 net/http
- **CLI 框架**: cobra