# Morpheus 项目优化建议

基于 OpenCode 架构设计文档对比和代码审查，本文档列出待优化的地方供决策。

---

## 一、代码质量优化

### 1.1 超大文件拆分 (High Priority)

**问题**: `internal/app/runtime.go` 达 4258 行，职责过于集中。

| 文件 | 行数 | 问题 |
|------|------|------|
| `internal/app/runtime.go` | 4258 | 包含 Runtime 结构体 + 大量方法，职责过多 |
| `cli/src/app.tsx` | ~2500行 | 前端组件过大 |
| `cli/src/app.jsx` | ~2800行 | 同上 |

**建议拆分方案**:

```
internal/app/
├── runtime.go           # 结构体定义 + 构造函数 (保留 ~500行)
├── runtime_session.go   # 会话相关方法
├── runtime_checkpoints.go # Checkpoint 相关
├── runtime_api.go       # API 处理方法
├── runtime_mcp.go       # MCP 会话管理
└── runtime_tools.go     # 工具注册相关
```

**影响**:
- ✅ 提高可维护性
- ✅ 便于独立测试
- ⚠️ 需要注意包内循环依赖问题

---

### 1.2 单元测试覆盖 (High Priority)

**问题**: 项目没有任何 `*_test.go` 文件，核心逻辑无测试保障。

**当前状态**:
```bash
$ find . -name "*_test.go"
# 无结果
```

**建议优先级**:

| 模块 | 测试类型 | 优先级 |
|------|----------|--------|
| `internal/policy/engine.go` | 策略引擎测试 | High |
| `internal/exec/orchestrator.go` | 执行器测试 | High |
| `internal/convo/manager.go` | 对话管理测试 | Medium |
| `internal/app/coordinator.go` | 协调器测试 | Medium |
| `internal/config/validation.go` | 配置验证测试 | Medium |

**建议使用**:
- `testing` 标准库
- `testify/assert` 断言库
- `testify/mock` mock 框架

---

### 1.3 代码规范问题 (Medium Priority)

**go vet 发现的问题**:

```
internal/app/run_state.go:482:2: unreachable code
```

**建议**: 清理 unreachable code，提升代码质量。

---

## 二、架构优化

### 2.1 依赖注入框架 (Medium Priority)

**当前实现**: 手动依赖注入，通过 `RuntimeBuilder` + `Lazy[T]` 实现。

**OpenCode 方案**: 使用 `effect` 库实现函数式 DI。

**Morpheus 现状** (`internal/app/lazy.go`):
```go
type Lazy[T any] struct {
    init func() T
    val  T
    once sync.Once
}
```

**可选改进**:
1. **保持现状** - 当前手动 DI 已工作，无需引入复杂性
2. **引入 Uber FX** - 标准化 Go 生态 DI框架，编译时注入图验证
3. **引入 effect** - 函数式 DI，类似 OpenCode

**建议**: 保持当前手动 DI，直到有明确需求再重构。

---

### 2.2 错误处理统一 (Medium Priority)

**当前状态**: 部分地方 `try/catch`，部分地方 `error` 返回值。

**建议**:
- 统一使用 `pkg/sdk/result.go` 中的 `Result[T]` 类型
- 已在 `orchestrator` 使用，需要推广到全项目

---

### 2.3 事件总线增强 (Low Priority)

**当前实现**: `sync.Map` 存储事件处理器。

**OpenCode 方案**: `PubSub` (进程内) + `GlobalBus` (跨进程)。

**建议**:
- 考虑引入消息队列支持跨进程事件
- 当前实现足够简单，非优先级

---

## 三、功能缺失 (对应 architecture-gap.md)

### 3.1 项目上下文文件 .morpheus.md (High Priority)

**竞品实现**:
- Claude Code: `CLAUDE.md`
- OpenCode: `AGENTS.md`
- Gemini CLI: `GEMINI.md`

**当前状态**: `MorpheusConfig.ContextFile` 配置存在但未实现。

**建议**:
1. 支持 `.morpheus.md` (项目级)
2. 支持 `~/.morpheus.md` (全局级)
3. 自动加载到系统提示

---

### 3.2 Checkpoints 自动快照 (High Priority)

**竞品实现**:
- Claude Code: 自动 Checkpoints
- Gemini CLI: Checkpoints

**当前状态**: `runtime.go` 中有 `checkpoints sync.Map`，但不完整。

**建议**:
1. 实现自动代码状态快照
2. 支持回滚到指定 checkpoint
3. 冷却时间: 每次工具执行后

---

### 3.3 上下文压缩优化 (Medium Priority)

**当前实现**:
```go
// 固定 60k tokens 阈值
if tokenCount > 60000 {
    compress()
}
```

**竞品实现**:
- OpenCode: Auto Compact 95%
- Claude Code: 自适应压缩

**建议**:
- 基于 token 计数而非固定阈值
- 保留关键决策点
- 生成结构化摘要

---

### 3.4 Agent Teams (Medium Priority)

**竞品实现**:
- Claude Code: Agent Teams (多会话协调)

**当前状态**: 有 `agent_team.go`，但功能可能不完整。

**建议**:
1. 多会话协调，共享任务
2. 子 Agent 并行运行
3. 共享上下文管理

---

### 3.5 远程控制协议 (Medium Priority)

**竞品实现**:
- Claude Code: Remote Control

**当前状态**: `remote_ws.go` 存在，已实现 WebSocket 远程连接。

**建议**:
- 完善 WebSocket 远程控制
- 认证和会话管理

---

### 3.6 忽略规则文件 (Low Priority)

**竞品实现**:
- Gemini CLI: `.geminiignore`
- OpenCode: 自动 LSP 发现

**建议**:
- 支持 `.morpheusignore` 文件
- 配置忽略文件/目录模式

---

## 四、工程化优化

### 4.1 CLI 前端代码拆分 (Medium Priority)

**问题**: `cli/src/app.tsx` 和 `cli/src/app.jsx` 过大 (~2500-2800 行)。

**建议拆分**:
```
cli/src/
├── components/
│   ├── Chat.tsx
│   ├── Terminal.tsx
│   ├── Sidebar.tsx
│   └── Settings.tsx
├── hooks/
│   └── useApi.ts
└── app.tsx  # 主入口，仅组合
```

---

### 4.2 持续集成 (Low Priority)

**建议添加**:
1. GitHub Actions CI
2. `golangci-lint` 代码检查
3. 单元测试自动化
4. 跨平台构建

---

### 4.3 API 文档 (Low Priority)

**当前状态**: 手动维护 REST API 文档。

**建议**:
- 使用 Swagger/OpenAPI 注解自动生成
- 或使用 `swag` 工具从代码注释生成

---

## 五、优先级决策表

 | 优化项            | 工作量   | 优先级   | 选择   |
 | --------          | -------- | -------- | ------ |
 | 超大文件拆分      | 中       | High     | [x]    |
 | 单元测试覆盖      | 高       | High     | [ ]    |
 | go vet 问题修复   | 低       | Medium   | [x]    |
 | .morpheus.md 支持 | 中       | High     | [x]    |
 | Checkpoints 机制  | 中       | High     | [x]    |
 | 上下文压缩优化    | 中       | Medium   | [x]    |
 | Agent Teams 增强  | 高       | Medium   | [x]    |
 | 远程控制完善      | 中       | Medium   | [x]    |
 | CLI 前端拆分      | 中       | Medium   | [x]    |
 | 忽略规则文件      | 低       | Low      | [x]    |
 | 持续集成          | 中       | Low      | [x]    |
 | API 文档生成      | 低       | Low      | [x]    |

---

## 六、不建议优化的项

以下项基于 OpenCode 架构对比，**不建议**立即优化：

1. **依赖注入框架重构** - 当前手动 DI 工作正常，引入新框架增加复杂性
2. **事件总线跨进程** - 当前实现足够简单
3. **全量重构为微服务** - 当前单体架构合理
4. **替换 SQLite** - SQLite 适合 CLI 工具场景

---

请勾选您希望实施的优化项，我可以提供具体实施方案。
