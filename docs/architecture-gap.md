# Morpheus 架构差距分析报告

## 1. 三者架构对比总览

 | 维度           | Morpheus           | Claude Code      | OpenCode           |
 | ------         | ----------         | -------------    | ----------         |
 | **开源协议**   | MIT                | 专有             | Apache 2.0         |
 | **代码规模**   | ~42K Go+TS         | 512K TypeScript  | ~130K TypeScript   |
 | **架构模式**   | Go后端+TS前端      | TS全栈 (Bun)     | TS全栈 (Bun)       |
 | **运行时**     | Go 1.25            | Bun              | Bun                |
 | **模型支持**   | 15+ providers      | Claude only      | 75+ providers      |
 | **持久化**     | SQLite + 文件      | SQLite           | SQLite             |
 | **上下文窗口** | 依赖模型           | 1M (Sonnet)      | 依赖模型           |
 | **压缩策略**   | 60K token 阈值     | 多层自适应压缩   | 95% 压缩           |
 | **工具协议**   | MCP (完整)         | MCP (开放标准)   | MCP                |
 | **Agent架构**  | Coordinator + DAG  | Subagent + Teams | Primary + Subagent |
 | **Skills系统** | 9内置 + Lazy Load  | Bundled Skills   | Skills             |
 | **权限控制**   | 4级风险 + 策略引擎 | Hook Chain + 6层 | Pattern-based      |
 | **TUI**        | OpenTUI (Solid.js) | 原生 Ink TUI     | 原生 TUI           |
 | **平台**       | macOS/Linux        | 全平台           | 全平台             |
 | **测试覆盖**   | ~6% (7个测试文件)  | 较高             | 较高               |

---

## 2. Claude Code 深度分析（源码泄露）

### 2.1 五层架构

```
┌─────────────────────────────────────────────────────────┐
│  入口层 (Entrypoints)                                   │
│  CLI、桌面端、网页、IDE插件、SDK 统一路由               │
├─────────────────────────────────────────────────────────┤
│  运行层 (Runtime)                                       │
│  TAOR 循环 (Think→Act→Observe→Repeat) + Hook 系统       │
├─────────────────────────────────────────────────────────┤
│  引擎层 (Engine) — 46,000 行                            │
│  QueryEngine 单例：数百个提示碎片动态拼装               │
│  安全守则 5,677 token                                   │
├─────────────────────────────────────────────────────────┤
│  工具与能力层 (Tools & Caps)                            │
│  40 个隔离工具单元，工具基类 29,000 行                  │
├─────────────────────────────────────────────────────────┤
│  基础设施层 (Infrastructure)                            │
│  14 个缓存断点、远程 kill switch (GrowthBook)           │
└─────────────────────────────────────────────────────────┘
```

### 2.2 Claude Code 核心机制

#### Agentic Loop 五步流水线

```
Snip → 微压缩 → 上下文折叠 → 自动压缩 → 组装请求
```

**关键优化**：
- **流式工具执行**：模型输出时并行准备工具调用
- **Fallback 机制**：主模型失败用 tombstone 消息清理孤儿 partial thinking blocks
- **用户中断处理**：为未完成 tool_use block 补 error 类型 tool_result

#### 多层上下文压缩

| 层级 | 触发条件 | 特点 |
|------|----------|------|
| **微压缩** | 工具结果太长 | 就地缩减，用户透明 |
| **自动压缩** | token > 窗口-20K | 阈值 = 窗口-20K (p99.99输出17K) |
| **会话记忆压缩** | 跨压缩边界 | 保留关键约束 |
| **上下文折叠** | 实验性 | 折叠优先于压缩 |

**CLAUDE.md 特殊地位**：四层加载（组织→用户→项目→本地），不受压缩影响。

#### 六层权限防线（useCanUseTool.tsx）

```
1. 项目/用户配置白名单
        ↓
2. 自动模式分类器（yoloClassifier.ts）
        ↓
3. 协调者门控
        ↓
4. Swarm 工作者门控
        ↓
5. Bash 安全分类器（23 条规则）
        ↓
6. 用户确认
```

**cch Attestation**：Bun/Zig 层计算哈希认证，JS 层无法伪造，`DISABLE_TELEMETRY=1` 仍无法关闭。

#### 三层记忆系统

| 层级 | 类比 | 机制 |
|------|------|------|
| **Semantic** | 长期语义记忆 | 高信号内容写入，矛盾剔除，RAG检索 |
| **Episodic** | 情景记忆 | 时间索引，按需检索 |
| **Working** | 工作记忆 | 超限用指针代替内容 |

**Reflection 机制**：任务成功率 60% → 85%，代价是多一轮 API 调用。

### 2.3 Claude Code 独特功能

| 功能 | 描述 |
|------|------|
| **KAIROS** | 持续后台代理模式（feature flag），每几秒主动检查，24h不间断 |
| **Undercover Mode** | 内部员工提开源PR时自动激活，剥离所有AI标识 |
| **反蒸馏** | `ANTI_DISTILLATION_CC=1` 时注入假工具定义 |
| **Auto-Dream** | 每24h或5次会话后fork子代理整合记忆 |
| **Capybara** | 内部模型代号，v8 false-claims rate 29-30% |
| **Tengu** | 内部项目代号，所有analytics event以`tengu_`开头 |
| **Buddy System** | 虚拟宠物，hash(userId)确定性生成 |

### 2.4 Claude Code 启发

**核心洞见**：6400行 AI 交互代码，50万行工程基础设施。

- **代码约束 > prompt 约束**：工具注册层面硬约束比 prompt 指导可靠
- **安全是纵深**：每层独立失败，任何一层发现问题就停
- **架构服务于成本**：14个缓存断点、20K预留token——每次缓存失效都要花真实的钱

---

## 3. OpenCode 架构分析

### 3.1 技术栈

| 层级 | 技术选型 |
|------|----------|
| 运行时 | Bun, Node.js |
| 前端框架 | SolidJS + SolidStart |
| 后端框架 | Hono |
| 数据库 | SQLite (本地), PlanetScale MySQL (云端) |
| ORM | Drizzle ORM |
| 依赖注入 | Effect (函数式 Effect 系统) |
| AI 集成 | AI SDK (统一 AI provider 接口) |
| 桌面端 | Tauri (Rust), Electron |

### 3.2 核心架构模式

#### Effect-based Dependency Injection

```typescript
export const layer = Layer.effect(
  Service,
  Effect.gen(function* () {
    const config = yield* Config.Service
    const auth = yield* Auth.Service
  }),
)
```

**优点**：编译时类型安全、组合式 Layer 设计、更好的错误处理
**缺点**：学习曲线陡峭、Effect 生态相对小众、调试困难

#### Instance-based Multi-tenancy

每个项目目录对应一个 `Instance`，实现状态隔离：

```typescript
Instance.provide(() => {
  const state = InstanceState.make()
  // per-directory isolated state
})
```

### 3.3 OpenCode 独特功能

| 功能 | 描述 |
|------|------|
| **Zen 服务** | 优化和基准测试的精选模型 |
| **多会话并行** | 并行运行多个 Agent |
| **分享链接** | 分享会话链接供调试 |
| **Event-sourced Syncing** | 事件源同步 |
| **Desktop App** | macOS/Windows/Linux + IDE 扩展 |

---

## 4. Morpheus 能力矩阵

### 4.1 已实现功能 ✅

| 功能 | 实现细节 |
|------|----------|
| **Agentic Loop** | 5步预处理流水线（输入→意图分类→消息构建→LLM调用→工具执行） |
| **意图分类路由** | simple_chat / lightweight_answer / fresh_info / tool_agent |
| **多Agent协调** | Coordinator + DAG调度，最大6任务/3并发 |
| **内置Agent Profiles** | 9种：implementer, explorer, reviewer, architect, tester, devops, data, security, docs |
| **MCP协议** | stdio/HTTP/SSE三种传输，完整工具发现和资源订阅 |
| **Skills系统** | 9内置 + 8种路径懒加载，支持allowed-tools限制 |
| **自定义Subagent** | SUBAGENT.md YAML定义，工具权限配置 |
| **Policy Engine** | 4级风险评估（low/medium/high/critical）+ 保护路径 |
| **Git Checkpoint** | git stash快照，回滚机制 |
| **上下文压缩** | 60K token阈值，两阶段（prune + compress） |
| **Session存储** | SQLite WAL + 文件双后端，事件溯源 |
| **Memory管理** | short-term (4KB) + long-term (12KB) |
| **Plugin Hooks** | Message/System/ToolBefore/ToolAfter 四种钩子 |
| **Todo跟踪** | 内置todo.write工具 |
| **结构化输出** | JSON schema解析 |
| **SSE流式** | /repl/stream端点 |
| **多模型支持** | 15+ provider (OpenAI/DeepSeek/MiniMax/GLM/Gemini/Anthropic/...) |
| **远程控制** | WebSocket远程连接（server.remote.enabled） |
| **热更新配置** | fsnotify + 定时轮询 |

### 4.2 Morpheus 独占优势

| 功能 | 优势 |
|------|------|
| **多Agent协调（DAG）** | 任务分解+拓扑排序+并行执行，比Claude Code更灵活 |
| **意图分类路由** | 减少不必要的工具调用，降低token消耗 |
| **Git Checkpoint** | 完整的代码状态快照，比Claude Code的checkpoints更底层 |
| **内置Agent Profiles** | 9种预定义角色，开箱即用 |
| **多路径Skills加载** | 兼容OpenCode/Claude生态 |
| **Subagent文件定义** | 支持SUBAGENT.md声明式配置 |
| **Plugin Hook系统** | 四种钩子支持无侵入增强 |

### 4.3 待实现/差距

| 功能 | Morpheus现状 | Claude Code实现 |
|------|-------------|-----------------|
| **项目上下文文件** | ✅ 4层加载 (.morpheus.md) | .claude.md 四层加载 |
| **全局上下文文件** | ✅ ~/.config/morpheus/.morpheus.md | ~/.claude.md |
| **多层记忆系统** | ✅ 3层 (Working/Episodic/Semantic) | Semantic/Episodic/Working 3层 |
| **Reflection机制** | ✅ 自省提示注入 | 自省环节，成功率+25% |
| **细粒度Bash安全** | ✅ 23种规则 + Zsh扩展 + IFS注入 | 23种规则 + 语义分析 |
| **原生客户端认证** | ❌ 无 | cch Attestation (Bun/Zig) |
| **卧底模式** | ❌ 无 | Undercover Mode |
| **反蒸馏机制** | ❌ 无 | Anti-distillation |
| **KAIROS持续运行** | ❌ 无 | 24h后台代理 |
| **VS Code扩展** | ❌ 无 | Beta原生集成 |
| **插件市场** | ❌ 无 | 完整生态 |
| **Remote Control协议** | ⚠️ 基础 | 完整远程控制 |
| **Checkpoints API** | ⚠️ git stash | 完整快照+回滚UI |
| **Background Agents** | ✅ 支持 background=true 参数 | 并行不阻塞 |

---

## 5. 架构差异详解

### 5.1 上下文管理对比

```
Claude Code:                    Morpheus:
┌─────────────────────┐        ┌─────────────────────┐
│ CLAUDE.md (4层)     │        │ .morpheus.md (4层)   │
│ Auto Memory         │        │ Working/Episodic/     │
│ Context Collapse    │        │ Semantic Memory       │
│ Micro Compact       │        │ Token阈值压缩       │
│ Reflection          │        │ ✅ 已实现            │
└─────────────────────┘        └─────────────────────┘
```

**差距已缩小**：
- ✅ .morpheus.md 4层加载（system/user/project/local）
- ✅ 3层记忆系统已实现
- ✅ Reflection 自省机制已实现
- ⚠️ 自动记忆提取（Auto-Dream）待实现

### 5.2 Agent架构对比

```
Claude Code:                    Morpheus:
┌─────────────────────┐        ┌─────────────────────┐
│ Subagent (递归)     │        │ ✅ 递归嵌套(3层)     │
│ Agent Teams         │        │ Team Messaging      │
│ Background Agent    │        │ ✅ background参数    │
│ Fork (信息隔离)     │        │ (规划中)            │
│ Verification Agent  │        │ (无等效)            │
└─────────────────────┘        └─────────────────────┘
```

**差距已缩小**：
- ✅ Subagent 递归嵌套已实现（最大深度3层）
- ✅ Background Agent 已支持（background=true）
- ⚠️ Fork 信息隔离机制待实现
- ⚠️ Verification Agent 待实现

### 5.3 安全架构对比

```
Claude Code:                    Morpheus:
┌─────────────────────┐        ┌─────────────────────┐
│ 6层权限防线         │        │ 4级风险评估         │
│ cch Attestation     │        │ (无原生认证)        │
│ Undercover Mode     │        │ (无)                │
│ Anti-distillation   │        │ (无)                │
│ 23条Bash规则       │        │ ✅ 28条规则         │
│ 语义AST分析         │        │ ✅ Zsh/IFS检测      │
│ AI分类器            │        │ (无)                │
└─────────────────────┘        └─────────────────────┘
```

**差距已缩小**：
- ✅ 28条 Bash 安全规则已实现
- ✅ Zsh 扩展检测已实现
- ✅ IFS 注入检测已实现
- ⚠️ 原生客户端认证待实现
- ⚠️卧底模式/反蒸馏待实现
- ⚠️ AI分类器待实现

### 5.4 工程规模对比

| 指标 | Claude Code | Morpheus | 差距 |
|------|-------------|----------|------|
| **代码总量** | 512K 行 TS | ~50K 行 Go | 10x |
| **工具实现** | 40个 | 15个 | 2.7x |
| **UI组件** | 140+ | TUI | - |
| **提示词管理** | 5677 token 安全守则 | 单一System Prompt | - |
| **缓存断点** | 14个 | 无 | - |
| **记忆系统** | 3层+巩固 | 2层 | - |

---

## 6. 改进建议

### P0 - 高优先级

#### 6.1 项目上下文文件 (.morpheus.md)

```yaml
# config.yaml
morpheus:
  context_file: .morpheus.md  # 项目级
  # 全局级: ~/.morpheus.md
```

**加载顺序**：组织级 → 用户级 → 项目级 → 本地级

**约束**：不受压缩影响，每次API请求都出现

#### 6.2 多层记忆系统

```go
type MemorySystem struct {
    semantic    *SemanticMemory  // 持久知识，RAG检索
    episodic    *EpisodicMemory // 对话历史，时间索引
    working     *WorkingMemory   // 当前上下文，指针替代
}

// Reflection: 每轮Act完成后自省
func (m *MemorySystem) Reflection(ctx context.Context) error
```

#### 6.3 增强Bash安全

```go
// internal/security/bash_audit.go
type BashAuditor struct {
    staticRules    []StaticRule   // 23种模式匹配
    semanticParser *ASTParser     // tree-sitter AST
    classifier     *AIClassifier  // LLM分类器 (yolo mode)
}
```

### P1 - 中优先级

#### 6.4 Subagent增强

- 支持递归嵌套（当前只支持一层）
- 实现Fork信息隔离机制
- Background Agent支持

#### 6.5 Checkpoint API

```go
// API扩展
POST /api/v1/checkpoints      // 创建快照
GET  /api/v1/checkpoints     // 列出快照
POST /api/v1/checkpoints/:id/rollback  // 回滚
```

#### 6.6 卧底模式

```go
// internal/undercover/undercover.go
type UndercoverMode struct {
    enabled     bool
    stripIdentity   bool
    stripModelID    bool
    blockInternalInfo bool
}
```

### P2 - 低优先级

#### 6.7 插件市场

- 打包格式定义
- 签名验证
- 社区分享平台

#### 6.8 VS Code扩展

- LSP深度集成
- 遥测和debug适配

---

## 7. 总结

### 7.1 Morpheus定位

Morpheus 是一个**工程导向**的AI编程助手：
- Go后端高性能，模块清晰
- 重视扩展性（Plugin/Skills/Subagent）
- 多模型支持灵活
- DAG协调器比Claude Code更强大

### 7.2 核心差距

| 维度 | Morpheus | Claude Code |
|------|----------|-------------|
| **上下文管理** | 简单token阈值 | CLAUDE.md + 多层压缩 + 记忆巩固 |
| **Agent深度** | DAG协调 | 递归嵌套 + Fork隔离 + Background |
| **安全体系** | 策略引擎 | 6层防线 + 原生认证 + 反蒸馏 |
| **工程规模** | ~50K行 | 512K行 |
| **生态** | 起步 | 完整插件市场 |

### 7.3 竞争优势

1. **DAG协调器**：比Claude Code的subagent更灵活的任务分解
2. **多模型**：不锁定Claude，支持75+ provider
3. **开源可控**：MIT协议，可深度定制
4. **模块化**：Go清晰架构，易维护和扩展

### 7.4 改进路线图

```
Phase 1 (P0): 上下文管理 ✅
├─ ✅ .morpheus.md 4层加载
├─ ✅ 多层记忆系统 (Working/Episodic/Semantic)
└─ ✅ Reflection 自省机制

Phase 2 (P1): Agent增强 ✅
├─ ✅ Subagent递归嵌套 (最大3层)
├─ ⚠️ Fork信息隔离
└─ ✅ Background Agent (background=true)

Phase 3 (P2): 安全增强 ✅
├─ ✅ 28条 Bash 安全规则
├─ ⚠️ 原生客户端认证
└─ ⚠️ 卧底模式

Phase 4 (P3): 生态
├─ ⚠️ 插件市场
└─ ⚠️ VS Code扩展
```

---

_文档版本: 3.2_
_最后更新: 2026-04-09_
