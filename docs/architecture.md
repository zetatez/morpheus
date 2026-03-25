# Morpheus 工具架构文档

Morpheus 是一个 AI 代码助手，采用模块化架构设计，支持多模型 planner、灵活的 tool 扩展、MCP 协议集成、安全策略控制、多 Agent 协调和自定义 Skills。

## 1. 系统架构总览

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLI / API Server                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                   Runtime                                    │
│  ┌──────────────┐  ┌─────────────┐  ┌────────────┐  ┌──────────────────┐   │
│  │ Conversation │  │   Planner   │  │Coordinator │  │    Plugin        │   │
│  │   Manager    │  │   (LLM)    │  │            │  │    Registry      │   │
│  └──────────────┘  └─────────────┘  └────────────┘  └──────────────────┘   │
│  ┌──────────────┐  ┌─────────────┐  ┌────────────┐  ┌──────────────────┐   │
│  │   Skills     │  │ AgentRegistry│  │   Policy   │  │   Session        │   │
│  │   (Lazy)     │  │(Custom Agents)│  │   Engine  │  │   (SQLite)      │   │
│  └──────────────┘  └─────────────┘  └────────────┘  └──────────────────┘   │
│         │                 │               │                  │              │
│         └─────────────────┴───────────────┴──────────────────┘              │
│                                    │                                         │
│                     ┌──────────────┴──────────────┐                         │
│                     │       Tool Registry         │                         │
│                     │  (fs/cmd/agent/skill/mcp/...)│                         │
│                     └─────────────────────────────┘                         │
│                                    │                                         │
│         ┌─────────────────────────┼─────────────────────────┐              │
│         │             Policy Engine (Security)                │              │
│         └───────────────────────────────────────────────────────┘              │
│                                    │                                         │
│                     ┌──────────────┴──────────────┐                         │
│                     │       AgentLoop             │                         │
│                     │  (Build/Plan modes)         │                         │
│                     └─────────────────────────────┘                         │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 2. 核心组件

### 2.1 Runtime (`internal/app/runtime.go`)

Runtime 是整个系统的主入口，负责组件的初始化和组装。

```go
type Runtime struct {
    cfg           config.Config      // 配置
    logger        *zap.Logger        // 日志
    conversation  *convo.Manager    // 会话管理
    planner       sdk.Planner        // 规划器
    orchestrator  *exec.Orchestrator // 执行器
    registry      *registry.Registry // 工具注册表
    audit         *auditWriter       // 审计日志
    session       *session.Writer    // 文件持久化
    sessionStore  *session.Store     // SQLite 持久化
    plugins       *plugin.Registry   // 插件系统
    skills        *skill.Loader      // 技能加载器
    mcpSessions   sync.Map          // MCP 会话管理
    taskState     sync.Map          // 任务状态
    pendingConfirmations sync.Map    // 待确认操作
    agentRegistry *AgentRegistry     // 自定义 Agent 注册表
}
```

**初始化流程**:
```
NewRuntime()
  ├─ newLogger()             → 初始化 zap 日志
  ├─ convo.NewManager()      → 创建会话管理器
  ├─ plugin.NewRegistry()    → 创建插件注册表
  ├─ loadSoulPrompt()        → 加载 SOUL.md 系统提示
  ├─ skill.NewLoader()       → 初始化技能加载器 (Lazy Mode)
  ├─ mcp.NewManager()        → 初始化 MCP 管理器
  ├─ registry.NewRegistry()   → 创建工具注册表
  ├─ buildAvailableTools()   → 注册所有内置工具
  ├─ mcp.Bootstrap()         → 启动 MCP 服务器
  ├─ policy.NewPolicyEngine() → 创建策略引擎
  ├─ NewAgentRegistry()      → 创建自定义 Agent 注册表
  ├─ buildPlanner()          → 创建 planner (openai/keyword)
  └─ newAuditWriter()        → 创建审计日志
```
    plugins       *plugin.Registry   // 插件系统
    skills        *skill.Loader      // 技能加载器
    mcpSessions   sync.Map           // MCP 会话管理
    taskState     sync.Map           // 任务状态
}
```

**初始化流程**:
```
NewRuntime()
  ├─ newLogger()             → 初始化 zap 日志
  ├─ convo.NewManager()      → 创建会话管理器
  ├─ plugin.NewRegistry()    → 创建插件注册表
  ├─ loadSoulPrompt()        → 加载 SOUL.md 系统提示
  ├─ skill.NewLoader()       → 初始化技能加载器
  ├─ mcp.NewManager()        → 初始化 MCP 管理器
  ├─ registry.NewRegistry() → 创建工具注册表
  ├─ buildAvailableTools()   → 注册所有内置工具
  ├─ mcp.Bootstrap()         → 启动 MCP 服务器
  ├─ policy.NewPolicyEngine()→ 创建策略引擎
  ├─ buildPlanner()          → 创建 planner (openai/keyword)
  └─ newAuditWriter()        → 创建审计日志
```

### 2.2 AgentLoop (`internal/app/agent_loop.go`)

AgentLoop 实现迭代式 agent 执行，是核心的业务逻辑处理层。

```go
func (rt *Runtime) AgentLoop(ctx, sessionID, input string, format *OutputFormat, mode AgentMode) (Response, error)
```

**AgentMode 模式**:
| 模式 | 说明 |
|------|------|
| `build` | 完整开发能力，可执行命令和写入文件 |
| `plan` | 只读分析模式，仅生成计划不执行 |

**交互式确认**:
```
pendingConfirmation
  ├─ Tool: 待确认工具名
  ├─ Inputs: 工具参数
  └─ Decision: 决策信息
```

**执行流程**:
```
AgentLoop
  │
  ├─ getPendingConfirmation() → 检查待确认操作
  │   ├─ approve: 执行待确认操作
  │   ├─ deny: 取消操作
  │   └─ 其他: 提示用户确认
  │
  ├─ preprocessSkillCommand() → 预处理技能命令
  ├─ appendMessage()        → 添加用户消息到会话
  ├─ allowMentionedSkills() → 解析并允许引用的技能
  ├─ checkAndCompress()     → 基于 token 计数的智能压缩
  ├─ collectToolSpecs()     → 收集可用工具定义
  ├─ buildAgentMessages()   → 构建完整的消息列表
  │
  ├─ if mode == AgentModePlan:
  │   └─ 添加只读提示
  │
  ├─ maybeCoordinate()     → 检查是否需要多 Agent 协调
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
      │   ├─ truncateToolResult() → 截断大型输出
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
    plugins  *plugin.Registry // 插件系统
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
  ├─ if step.Tool == "mcp.query":
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

### 2.4 Coordinator (`internal/app/coordinator.go`)

Coordinator 实现多 Agent 协调，支持任务分解和 DAG 依赖调度。

```go
type coordinatorPlan struct {
    Summary string            `json:"summary"`
    Tasks   []coordinatorTask `json:"tasks"`
}

type coordinatorTask struct {
    ID        string   `json:"id"`
    Role      string   `json:"role"`
    Prompt    string   `json:"prompt"`
    DependsOn []string `json:"depends_on,omitempty"`
}
```

**协调流程**:
```
maybeCoordinate()
  │
  ├─ shouldCoordinate() → 判断是否需要协调
  │   - 输入长度 >= 12 词
  │   - 包含换行、"then"、"and"、"also"
  │   - 包含 "plan"、"architecture"、"review"
  │
  ├─ buildCoordinatorPlan() → LLM 分解任务
  │   └─ 返回 JSON 任务计划 (max 6 tasks)
  │
  └─ runCoordinatorTasks() → 执行任务
      ├─ hasDependencies() → 检查是否有依赖
      ├─ 无依赖: runTasksParallel() → 并行执行 (max 3)
      └─ 有依赖: runTasksDAG() → DAG 拓扑排序执行
```

**DAG 调度**:
- 支持 `depends_on` 字段定义任务依赖
- 自动进行拓扑排序
- 等待依赖完成后执行后续任务

**触发条件**:
- 输入长度 >= 12 个词
- 包含多步骤关键词 (then, and, also)
- 包含任务类型关键词 (plan, architecture, review)
- 非 builtin planner 且有 API Key

### 2.5 Agent Profile (`internal/tools/agenttool/coordinator.go`)

Morpheus 支持内置和自定义 Agent Profile：

```go
type AgentProfile struct {
    Name         string
    Description  string
    Instructions string
}
```

**内置 Profile (9种)**:
| Profile | 描述 | 用途 |
|---------|------|------|
| `implementer` | 交付具体代码修改 | 聚焦可执行步骤 |
| `explorer` | 调查代码库细节 | 定位文件、API |
| `reviewer` | 审查变更风险 | 识别问题、边界情况 |
| `architect` | 设计高层方案 | 提出架构和权衡 |
| `tester` | 编写和验证测试 | 单元测试、集成测试 |
| `devops` | 部署和 CI/CD | Dockerfile、流水线 |
| `data` | 数据管道处理 | SQL、ETL、数据模型 |
| `security` | 安全漏洞审查 | 认证、加密、secret |
| `docs` | 技术文档编写 | API 文档、README |

### 2.6 自定义 Agent 配置 (`internal/app/agent_config.go`)

Morpheus 支持在配置文件中定义自定义 Agent，包括工具权限控制：

```yaml
agent:
  default_mode: build
  agents:
    - name: implementer
      description: Deliver concrete code changes efficiently
      instructions: Focus on actionable implementation steps...
      tools:
        - fs.read
        - fs.write
        - cmd.exec
        - git.*
      enabled: true

    - name: analyst
      description: Read-only code analysis
      tools:
        - fs.read
        - fs.glob
        - fs.grep
        - lsp.query
      enabled: true
```

**工具权限控制**:
- 每个 Agent 可以配置独立的工具白名单
- 支持通配符匹配 (如 `git.*` 匹配所有 git 工具)
- 子 agent 执行时自动应用工具限制
- `agent.run` 工具也支持 `tools` 参数

**AgentDefinition 结构**:
```go
type AgentDefinition struct {
    Name         string   // Agent 名称
    Description  string   // Agent 描述
    Instructions string   // Agent 指令
    Tools        []string // 可用工具列表 (* 支持通配符)
    Enabled      bool     // 是否启用
}
```

**运行时流程**:
```
RunSubAgentWithProfile(profile, prompt)
  │
  ├─ GetTools(profile.Name) → 获取工具列表
  │
  └─ RunSubAgent(ctx, prompt, allowedTools)
      │
      └─ ctx = WithAllowedTools(ctx, tools)
          │
          └─ collectToolSpecs()
              │
              └─ IsToolAllowedWithList(tools, toolName) → 过滤工具
```

**AgentRegistry**:
- 自动合并默认 Profile 和自定义配置
- 支持覆盖内置 Profile
- 支持工具权限配置
    Instructions string
}
```

| Profile | 描述 | 指令 |
|---------|------|------|
| `implementer` | 交付具体代码修改 | 聚焦可执行步骤，指出文件、编辑和测试 |
| `explorer` | 调查代码库细节 | 定位相关文件、API 和行为，汇总发现 |
| `reviewer` | 审查变更风险 | 识别正确性问题、边界情况和测试缺口 |
| `architect` | 设计高层方案 | 提出架构或系统级方案，指出权衡

## 3. 工具系统

### 3.1 Tool 接口 (`pkg/sdk/interfaces.go`)

```go
type Tool interface {
    Name()    string                      // 工具名称
    Describe() string                      // 工具描述
    Schema()  map[string]any              // 参数 schema
    Invoke(ctx context.Context, input map[string]any) (ToolResult, error)
}

type ToolSpec interface {
    Tool
    Schema() map[string]any // 工具参数 schema
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
| `lsp.query` | `internal/tools/lsp/lsp.go` | LSP 代码理解 |
| `conversation.ask` | `internal/tools/ask/question.go` | 向用户提问 |
| `web.fetch` | `internal/tools/webfetch/webfetch.go` | 网页抓取 |
| `respond.echo` | `internal/tools/respond/echo.go` | 直接响应 |
| `mcp.query` | `internal/tools/mcp/mcp.go` | MCP 服务器控制 |
| `mcp.*` | `internal/tools/mcp/mcp.go` | MCP 代理工具 (动态) |
| `git.*` | `internal/tools/git/git.go` | Git 操作 |

### 3.4 MCP 协议支持 (`internal/tools/mcp/mcp.go`)

Morpheus 实现了完整的 MCP (Model Context Protocol) 客户端支持：

```go
type Manager struct {
    clients          map[string]*client
    tools            map[string]*ProxyTool
    onChange         func() error
    onResourceUpdate func(server, uri string, payload map[string]any)
}

type ServerConfig struct {
    Name       string
    Command    string  // stdio 传输命令
    Transport  string  // stdio | http | sse
    URL        string  // HTTP 端点
    SSEURL     string  // SSE 端点
    AuthToken  string
    AuthHeader string
}
```

**支持的传输方式**:
- **stdio**: 通过子进程 stdio 通信 (如 `npx -y @modelcontextprotocol/server-filesystem .`)
- **http**: REST API 方式的 MCP 调用
- **sse**: Server-Sent Events 方式接收通知

**MCP 工具**:
- `mcp.query`: 管理 MCP 服务器连接、列出工具/资源、订阅资源更新
- `mcp.<server>.<tool>`: 动态代理 MCP 服务器工具

## 4. Planner 系统

### 4.1 Planner 接口

```go
type Planner interface {
    ID() string
    Capabilities() []string
    Plan(ctx context.Context, req PlanRequest) (Plan, error)
}
```

### 4.2 LLM Planner (`internal/planner/llm/openai.go`)

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

**支持的模型**:

| Provider | 默认模型 | 端点 |
|---------|---------|------|
| openai | gpt-4o-mini | https://api.openai.com/v1/chat/completions |
| deepseek | deepseek-chat | https://api.deepseek.com/v1/chat/completions |
| minmax | abab6.5s-chat | https://api.minimax.chat/v1/text/chatcompletion_v2 |
| glm | glm-4-flash | https://open.bigmodel.cn/api/paas/v4/chat/completions |
| gemini | gemini-2.0-flash | https://generativelanguage.googleapis.com/v1beta/models |

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
  │   - critical: rm -rf, :(){ :|:& };:, dd of=/dev/
  │   - high: sudo, chmod 777, curl|sh
  │   - medium: chmod, chown, apt remove
  │   - low: pip install, npm install
  │
  ├─ evaluatePath(workdir) → 匹配保护路径
  │   ConfirmProtectedPaths: [/etc, /usr, ~/.ssh, ...]
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

| 等级 | 说明 | 操作 |
|-----|------|------|
| `low` | 安全操作 | 默认允许 |
| `medium` | 需确认后执行 | 提示用户确认 |
| `high` | 需明确确认 | 提示用户确认 |
| `critical` | 拒绝执行 | 直接拒绝 |

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

### 6.2 智能上下文压缩

**压缩触发条件**:
- 超过 `MaxHistoryTokens` (60000 tokens) 时触发压缩
- 保留最近 4 条消息 + 生成摘要
- 冷却时间: `CompressionCooldown` (2 分钟)

**压缩策略**:
1. **修剪历史**: 优先删除旧的工具输出，保留关键决策点
2. **生成摘要**: 基于任务类型生成结构化摘要
   - 代码任务: 关注实现状态、文件、命令、决策、剩余工作
   - 一般任务: 关注目标、关键事实、决策、开放问题

### 6.3 Session Writer (`internal/session/writer.go`)

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
  ├── tool-output-*.txt     // 工具输出 (大型)
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

## 8. Skills 系统

### 8.1 Skill Loader (`internal/skill/loader.go`)

Morpheus 支持内置和自定义 Skills，采用 Lazy Load 机制。

```go
type Loader struct {
    skillsPaths []string
    skills      map[string]sdk.Skill
    loaded      bool
    lazyMode    bool
    mu          sync.RWMutex
}
```

**Lazy Load 机制**:
- 默认只加载内置 Skills
- 用户请求特定 skill 时自动加载用户自定义 skills
- `LoadBuiltinOnly()`: 仅加载内置 skills
- `LoadCustom()`: 按需加载用户自定义 skills

**加载流程**:
```
List() / Get(name) / Invoke()
    │
    ├─ lazyMode == true?
    │   └─ Yes: 调用 LoadCustom() 加载用户 skills
    │
    └─ 返回合并后的 skills
```

### 8.2 内置 Skills

Morppheus 内置 9 个常用 Skills：

| Skill | 描述 | 用途 |
|-------|------|------|
| `review` | 代码审查 | 审查变更、识别风险 |
| `test` | 测试推荐 | 推荐测试命令 |
| `docs` | 文档生成 | 起草/更新文档 |
| `refactor` | 重构建议 | 分析代码改进机会 |
| `debug` | 调试帮助 | 诊断问题、提供指导 |
| `security` | 安全审查 | 检查安全漏洞 |
| `git` | Git 指导 | Git 工作流建议 |
| `explain` | 代码解释 | 解释概念和代码 |
| `optimize` | 性能优化 | 分析性能瓶颈 |

### 8.3 自定义 Skill

用户可通过目录定义自定义 Skills，支持两个位置：

- `~/.config/morpheus/skills/` (用户级)
- `.morpheus/skills/` (项目级)

```
~/.config/morpheus/skills/
└── my-skill/
    ├── skill.yaml      # 清单配置
    └── prompt.md      # Prompt 模板
```

**skill.yaml 示例**:
```yaml
name: my-skill
description: Custom skill description
capabilities:
  - custom
expected_token_cost: 1000
```

**prompt.md 示例**:
```
Please help with the following task:
{{input}}
```

**Lazy Load**: 自定义 skills 采用懒加载机制，仅在用户首次调用时加载。

### 8.4 Skill 接口

```go
type Skill interface {
    Describe() SkillMetadata
    Warmup(ctx context.Context) error
    Invoke(ctx context.Context, input map[string]any) (map[string]any, error)
}

type SkillMetadata struct {
    Name              string
    Description       string
    Capabilities      []string
    ExpectedTokenCost int
}
```

### 8.5 Skill 使用

在对话中使用 `@skill` 触发：
```
@review 请审查这段代码
@test 为这个功能写测试
```

## 9. 配置系统

### 8.1 Config (`internal/config/config.go`)

```go
type Config struct {
    WorkspaceRoot string              // 工作目录
    Logging       LoggingConfig       // 日志配置
    Planner       PlannerConfig       // Planner 配置
    Server        ServerConfig       // 服务配置
    Session       SessionConfig       // 会话配置
    MCP           MCPConfig          // MCP 配置
    KnowledgeBase KnowledgeBaseConfig // 知识库
    Permissions   Permissions         // 权限配置
}
```

### 8.2 配置加载流程

```
Load(configPath)
  │
  ├─ viper.ReadInConfig() → 读取 YAML
  ├─ cfg.expandPaths() → 展开路径 (~/, 环境变量)
  ├─ cfg.loadAPIKeyFromEnv() → 从环境变量加载 API Key
  │   支持: BRUTECODE_API_KEY, OPENAI_API_KEY, DEEPSEEK_API_KEY, ...
  └─ cfg.Validate() → 验证配置
```

### 8.3 MCP 配置

```yaml
mcp:
  servers:
    - name: filesystem
      transport: stdio
      command: npx -y @modelcontextprotocol/server-filesystem .
    - name: remote
      transport: http
      url: https://example.com/mcp
      sse_url: https://example.com/mcp/sse
      auth_token: ${MCP_TOKEN}
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

### 10.2 添加 MCP 服务器

在 `config.yaml` 中配置:
```yaml
mcp:
  servers:
    - name: my-server
      transport: stdio
      command: npx -y my-mcp-server
```

### 10.3 添加新 Planner

1. 实现 `sdk.Planner` 接口
2. 在 `buildPlanner()` 中添加 case

### 10.4 添加插件钩子

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
│   ├── llm/               # LLM 兼容 planner
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
│   ├── lsp/                # LSP 工具
│   ├── git/                # Git 工具
│   └── mcp/                # MCP 协议支持
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
├── configstore/           # 配置目录
│   └── store.go           # 路径管理
└── cli/                   # CLI 命令
    ├── root.go            # 根命令
    ├── serve.go           # API 服务
    ├── repl.go            # REPL 模式
    └── model_config.go    # 模型配置
```

## 12. 技术栈

- **语言**: Go 1.25
- **LLM SDK**: OpenAI API compatible (支持 glm/minmax/deepseek/gemini)
- **日志**: zap
- **配置**: viper + YAML
- **Web 框架**: 标准库 net/http
- **CLI 框架**: cobra
- **协议**: MCP (Model Context Protocol)

## 13. 系统提示 (SOUL.md)

Morpheus 的系统提示定义了其核心价值观和行为准则:

- **立场**: 偏好清晰、系统、复利
- **思考方式**: 定义术语、分解问题、检验假设
- **沟通风格**: 清晰、直接、结构化、冷静
- **边界**: 不伪装确定性、不鼓励有害行为
