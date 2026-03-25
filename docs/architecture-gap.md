# BruteCode 架构差距分析报告

本文档对比 BruteCode 与当前业界领先 AI 编码助手（Claude Code、OpenCode、 Gemini-CLI）的架构实现，识别功能差距并提出改进建议。

## 1. 架构对比总览

| 维度 | BruteCode | Claude Code | OpenCode | Gemini-CLI |
|------|--------|------------|----------|------------|
| **架构模式** | 单 agent 循环 | 多 agent 协调 | 双 pipeline + 多 agent | ReAct loop + 并行 agent |
| **模型支持** | 4 (openai/glm/minmax/deepseek) | Claude only | 75+ | Gemini only |
| **持久化** | 文件 + JSON | SQLite | SQLite | 文件 |
| **上下文管理** | 基础消息压缩 | CLAUDE.md + 自动压缩 | Auto Compact 95% | GEMINI.md |
| **工具协议** | 自定义 | MCP (开放标准) | MCP | MCP + Skills |
| **插件系统** | 基础钩子 | Skills + Plugins | 30+ 自定义工具 | Skills |
| **权限控制** | 基础策略引擎 | 细粒度权限 | 工具级别权限 | 需确认机制 |
| **Agent 类型** | 单一 | 单一 + 子 agent | Plan/Build + 自定义 | 单一 |
| **多会话管理** | 基础 | 并行多会话 | 多会话 | 基础 |

---

## 2. 核心差距分析

### 2.1 多 Agent 协调架构

**现状 (BruteCode)**:
- 单一 agent 循环，所有任务在同一个上下文中执行
- `agent.run` 子 agent 只是简单的上下文隔离

**Claude Code**:
```
┌─────────────────────────────────────────────────────────┐
│                   Multi-Agent Architecture              │
├─────────────────────────────────────────────────────────┤
│  Coordinator Agent (Planning)                            │
│  ├─ Task decomposition                                   │
│  ├─ Agent selection & assignment                         │
│  └─ Result aggregation                                   │
│                                                         │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐       │
│  │ Implement   │ │ Explorer    │ │ Review      │       │
│  │ Agent       │ │ Agent       │ │ Agent       │       │
│  └─────────────┘ └─────────────┘ └─────────────┘       │
│       │              │              │                   │
│       └──────────────┼──────────────┘                   │
│                      ▼                                   │
│              Result Aggregation                          │
└─────────────────────────────────────────────────────────┘
```

**差距**:
- 缺少任务分解和协调层
- 无法并行执行独立子任务
- 缺乏专门化的 agent 类型（代码审查、架构设计等）

### 2.2 上下文管理

**现状 (BruteCode)**:
- `convo/manager.go`: 超过 32 条消息触发压缩
- 保留最近 32 条 + 摘要
- 无项目级持久上下文

**Claude Code**:
```
Context Assembly Layer:
┌─────────────────────────────────────────┐
│  1. System Prompt (cached, 92% reuse)   │
│  2. CLAUDE.md (project context, every turn)│
│  3. Tools (schema + definitions)        │
│  4. Conversation History (compactable)  │
│  5. Auto Memory (truncates after 200 lines)│
│  6. Skills/Plugins                      │
└─────────────────────────────────────────┘

CLAUDE.md priority: Only truly reliable memory
- Scanned at: ~/.claude/skills/ (global)
-              .claude/skills/ (project)
```

**OpenCode**:
- Auto Compact: 95% context window 时自动摘要
- 支持 ~.opencode/agents/ 自定义 agent

**差距**:
- 无项目级上下文文件机制 (CLAUDE.md/GEMINI.md)
- 压缩策略简单，无智能摘要
- 缺乏长期记忆机制

### 2.3 工具扩展协议 (MCP)

**现状 (BruteCode)**:
- 工具通过 `sdk.Tool` 接口注册
- 插件系统提供 4 种钩子 (message/system/toolBefore/toolAfter)
- 无外部服务集成协议

**Claude Code**:
```
MCP (Model Context Protocol):
┌─────────────────────────────────────────────────┐
│                  MCP Host                       │
├─────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐              │
│  │ stdio/HTTP  │  │ Plugin      │              │
│  │ Transport   │  │ Registry    │              │
│  └─────────────┘  └─────────────┘              │
│         │                │                      │
│         ▼                ▼                      │
│  ┌─────────────────────────────────────────┐   │
│  │        Tool Discovery                   │   │
│  │  - Read, Edit, Write, Bash, Glob       │   │
│  │  - Custom tools from MCP servers       │   │
│  └─────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘

JSON-RPC 2.0 over stdio/HTTP
```

**OpenCode**:
- 内置 40+ LSP 服务器
- 丰富的工具生态 (bash, edit, webfetch, agent, sourcegraph 等)
- 可配置工具权限

**差距**:
- 缺少 MCP 协议支持
- 工具生态不够丰富
- 无 LSP 集成
- 工具发现机制薄弱

### 2.4 Agent 系统

**现状 (BruteCode)**:
- 单一 agent 类型
- 工具权限无差异化

**OpenCode**:
```
Agent System:
┌──────────────────────────────────────────────────┐
│  Primary Agents (可切换)                          │
│  ├─ Build: 所有工具可用 (edit, bash, write)      │
│  └─ Plan: 只读模式 (无 edit/write 权限)          │
│                                                  │
│  Subagents (可自定义)                            │
│  ├─ review: 代码审查                             │
│  ├─ docs-writer: 文档生成                        │
│  └─ 自定义 agent                                 │
└──────────────────────────────────────────────────┘

配置方式:
- JSON: opencode.json agents 配置
- Markdown: .opencode/agents/*.md
```

**Claude Code**:
- Skills: 命令级扩展 (如 `/test`, `/review`)
- Agent tool: 子任务隔离执行

**差距**:
- 无内置 agent 类型区分
- 无自定义 agent 机制
- 无权限级别差异化

### 2.5 持久化与多会话

**现状 (BruteCode)**:
- 基于文件系统的会话存储
- JSON 元数据 + Markdown 对话
- 无并行会话支持

**OpenCode**:
```
SQLite Schema:
- conversations: 会话主表
- messages: 消息记录
- sessions: 会话管理
- 工作目录: .opencode/

支持:
- 多会话并行
- 会话搜索
- 会话导出
```

**Claude Code**:
- 完善的多会话管理
- 项目级持久状态

**差距**:
- 无 SQLite 持久化
- 无会话搜索/恢复
- 无并行多会话

### 2.6 权限与安全

**现状 (BruteCode)**:
- 基础策略引擎 (risk factors + protected paths)
- 4 级风险评估 (low/medium/high/critical)

**Claude Code**:
```
Permission System:
┌────────────────────────────────────────┐
│  Hook Chain:                           │
│  PreToolUse → Permissions → Execute  │
│            → PostToolUse               │
│                                        │
│  Permissions:                         │
│  - Allow/Deny per tool                │
│  - Confirmation for sensitive ops     │
│  - Rate limiting                      │
└────────────────────────────────────────┘
```

**OpenCode**:
- 工具级别权限控制
- Plan mode 只读
- Bash 命令限制

**差距**:
- 权限控制粒度不够
- 无交互式确认机制
- 无命令白名单/黑名单

### 2.7 Skills/Commands 系统

**现状 (BruteCode)**:
- 基础 skill 加载器
- 手动 @mention 触发

**Claude Code**:
```
Skills System:
- 位置: ~/.claude/skills/ (global)
-        .claude/skills/ (project)
- 触发: /command 格式
- 自动加载描述和参数

Built-in Skills:
- /test: 生成测试
- /review: 代码审查
- /debug: 调试辅助
- /commit: git 提交
```

**Gemini-CLI**:
```
Skills:
- 自动技能发现
- 按需加载
- 自定义技能支持
```

**差距**:
- 无内置命令式技能
- 无技能自动发现
- 缺乏预定义工作流

---

## 3. 架构改进建议

### 3.1 优先级 High: 核心架构

#### 3.1.1 引入 MCP 协议支持

```go
// 建议实现
type MCPServer struct {
    transport Transport // stdio/HTTP
    registry  *registry.Registry
}

type Transport interface {
    Connect(ctx context.Context) error
    Send(ctx context.Context, req JSONRPCRequest) (JSONRPCResponse, error)
}
```

#### 3.1.2 实现多 Agent 协调

```go
// 建议架构
type Coordinator struct {
    agents    map[string]Agent
    registry  *ToolRegistry
}

type Agent interface {
    ID() string
    Execute(ctx context.Context, task Task) (Result, error)
    Capabilities() []string
}
```

### 3.2 优先级 High: 上下文管理

#### 3.2.1 引入项目上下文文件

- 支持 `.brute.md` (项目级)
- 支持 `~/.brute.md` (全局级)
- 每次请求注入

#### 3.2.2 智能上下文压缩

- 基于 token 计数而非消息数
- 保留关键决策点
- 生成结构化摘要

### 3.3 优先级 Medium: Agent 系统

#### 3.3.1 内置 Agent 类型

```
- coder: 完整开发能力
- plan: 只读分析模式  
- review: 代码审查
```

#### 3.3.2 自定义 Agent 机制

- 支持 YAML/JSON 配置
- 支持 Markdown prompt
- 支持工具权限配置

### 3.4 优先级 Medium: 持久化

#### 3.4.1 迁移至 SQLite

```go
// 建议 schema
type Session struct {
    ID        string
    ProjectID string
    Messages  []Message
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### 3.5 优先级 Low: 工具生态

#### 3.5.1 集成 LSP

- 添加 LSP tool
- 复用 OpenCode 的 40+ LSP 服务器发现机制

#### 3.5.2 扩展工具集

- 添加 sourcegraph 搜索
- 添加 git 操作增强
- 添加数据库工具

---

## 4. 差距总结图

```
                    Claude Code / OpenCode / Gemini-CLI
                                │
         ┌──────────────────────┼──────────────────────┐
         │                      │                      │
    ┌────▼────┐            ┌─────▼─────┐         ┌────▼────┐
    │Multi-   │            │  MCP      │         │Skills   │
    │Agent    │            │Protocol   │         │System   │
    └────┬────┘            └─────┬─────┘         └────┬────┘
         │                        │                      │
         │    ┌───────────────────┘                      │
         │    │                                          │
    ┌────▼────▼────┐         ┌────────────┐         ┌────▼────┐
    │   BruteCode     │         │   Gap 1    │         │   Gap 2 │
    │ Current      │────────▶│ (MCP/Skills)│────────▶│(Multi-   │
    │ Architecture │         │             │         │ Agent)   │
    └─────────────┘         └────────────┘         └──────────┘
           │
           │    ┌───────────────────┐
           │    │   Gap 3           │
           └───▶│(Context/Session)  │
                └───────────────────┘
```

---

## 5. 实施路线图

### Phase 1: 核心补齐 (1-2 个月)

1. **MCP 协议集成**
   - 实现 MCP 服务器端
   - 支持 stdio 传输
   - 迁移现有工具至 MCP 格式

2. **项目上下文机制**
   - 支持 `.brute.md` 加载
   - 每次请求注入上下文

### Phase 2: Agent 增强 (2-3 个月)

3. **多 Agent 协调**
   - 实现 Coordinator
   - 支持任务分解
   - 支持并行执行

4. **内置 Agent 类型**
   - Plan (只读) / Coder (全功能)
   - 自定义 Agent 配置

### Phase 3: 生态完善 (3-6 个月)

5. **SQLite 持久化**
   - 会话迁移至 SQLite
   - 实现多会话管理

6. **LSP 集成**
   - 添加 LSP tool
   - 实现代码诊断

7. **Skills 系统**
   - 内置常用 skills
   - 自动发现机制

---

## 6. 结论

BruteCode 当前架构实现了基础的单 agent 编码助手功能，但与业界领先方案存在显著差距。主要体现在：

1. **协议层面**: 缺少 MCP 开放标准，限制了工具生态扩展
2. **架构层面**: 缺乏多 agent 协调和任务规划能力
3. **体验层面**: 上下文管理和持久化机制薄弱

建议优先实现 MCP 协议支持和项目上下文机制，这两个改进能在较短时间内显著提升用户体验和生态兼容性。