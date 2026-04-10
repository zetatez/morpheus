# Morpheus 与 Opencode 最新设计差距分析报告

> 分析时间: 2026-07-08
> 基于 Opencode 源码 ../opencode (2026-07)
> 对比基准: docs/opencode-comparison.md (2026-04-10)

---

## 概述

现有对比文档 (opencode-comparison.md) 完成于 2026-04-10，此后 Opencode 经历了大量架构演进。
本报告聚焦于 **对比文档未覆盖的新增功能**，按模块分类列出差距和行动建议。

---

## 1. 🔴 P0 - 必须跟进

### 1.1 上下文压缩：尾部保留 (Tail Preservation)

| 项目 | 状态 |
|------|------|
| Opencode | 压缩时保留最近 N 个 turns（用户-助手配对）作为 verbatim 上下文，避免有损摘要 |
| Morpheus | 4层压缩管道，但始终全量压缩，没有尾部保留机制 |
| **建议** | 在 compactor.go 中实现 `tail_turns` / `preserve_recent_tokens` 配置，压缩时跳过最近 N 轮交换 |

**参考文件:** `opencode/packages/opencode/src/session/compaction.ts`

### 1.2 压缩作为一等公民 (CompactionPart)

| 项目 | 状态 |
|------|------|
| Opencode | 压缩状态通过 `CompactionPart` 存储到消息链中，包含 `tail_start_id`, `auto`, `overflow` 字段，压缩历史可追溯 |
| Morpheus | 压缩是纯外部操作，不可回溯 |
| **建议** | 消息模型中添加 CompactionPart 类型，存储压缩元数据 |

### 1.3 Overflow 重放机制

| 项目 | 状态 |
|------|------|
| Opencode | 溢出时自动重放用户消息（去掉 media），而非报错 |
| Morpheus | 溢出时触发压缩或报错，无重放机制 |
| **建议** | 实现 overflow 重放：压缩后自动重放用户原始输入 |

### 1.4 结构化摘要模板

| 项目 | 状态 |
|------|------|
| Opencode | 强制使用结构化 Markdown 摘要格式：Objective / Important Details / Work State / Next Move / Relevant Files |
| Morpheus | 松散的摘要 prompt，无固定格式 |
| **建议** | 采用结构化摘要模板，使摘要内容一致且可解析 |

---

## 2. 🟠 P1 - 高优先级

### 2.1 插件系统：外部包 + npm 支持

| 项目 | 状态 |
|------|------|
| Opencode | 完整的插件系统，支持 npm 包、本地文件、URL 三种来源；V2 插件 SDK 提供 domain-specific transform API；内置认证 Provider 插件 |
| Morpheus | 仅有内部 Go hook 注册模式，marketplace 草案但未实际使用 |
| **建议** | 1) 定义标准插件 manifest + entrypoint 规范；2) 支持 Go plugin 动态加载或 WASM 插件；3) 实现插件生命周期管理 |

### 2.2 自定义斜杠命令 (Slash Commands)

| 项目 | 状态 |
|------|------|
| Opencode | 三层命令来源：内置(init/review) + 配置 `command` 字段 + `.opencode/command/*.md` 文件；MCP Prompts 和 Skills 自动注册为命令 |
| Morpheus | 无命令系统 |
| **建议** | 实现 `/` 命令框架，支持从配置文件 + 目录扫描注册命令 |

### 2.3 多 Provider 配置

| 项目 | 状态 |
|------|------|
| Opencode | `provider: Record<string, ProviderConfig>` 支持多 Provider 配置，每模型可独立配置 timeout/endpoint/apiKey |
| Morpheus | 单一 `planner` 块，一次只能用一个 Provider |
| **建议** | 将 config.yaml 的 planner 改为多 Provider 结构，支持按 agent 选择 model |

### 2.4 Agent Markdown 定义文件

| 项目 | 状态 |
|------|------|
| Opencode | 支持 `.opencode/agent/*.md` / `.opencode/mode/*.md` 文件定义 agent（YAML frontmatter + prompt body），自动发现 |
| Morpheus | Agent 完全在 YAML 配置中定义 |
| **建议** | 支持从 `.morpheus/agent/*.md` 文件发现和加载 agent |

### 2.5 工具：apply_patch

| 项目 | 状态 |
|------|------|
| Opencode | `apply_patch` 工具：模型通过 unified diff 编辑文件（GPT 系列模型使用此工具而非 edit/write） |
| Morpheus | 仅有 edit/write 工具 |
| **建议** | 实现 apply_patch 工具，支持 unified diff 格式的编辑 |

### 2.6 工具输出截断管理

| 项目 | 状态 |
|------|------|
| Opencode | `truncate` 服务 + `truncation-dir` 管理模型可见的工具输出大小；`tool_output.max_lines` / `max_bytes` 配置 |
| Morpheus | 无工具输出截断管理 |
| **建议** | 实现工具输出截断机制，将过长输出写入文件而非直接内联 |

---

## 3. 🟡 P2 - 中优先级

### 3.1 权限持久化 (SQLite)

| 项目 | 状态 |
|------|------|
| Opencode V2 | "Always allow" 规则持久化到 SQLite，跨 session 生效；action+resource 双通配符匹配 |
| Morpheus | 权限仅在内存中，session 重启后丢失 |
| **建议** | 将 approved 规则持久化到 SQLite session 数据库 |

### 3.2 快照 + Diff 追踪

| 项目 | 状态 |
|------|------|
| Opencode | Step-start 预捕捉文件状态，Step-finish 计算 diff，自动生成结构化摘要 |
| Morpheus | 无快照功能（仅有 git stash checkpoint） |
| **建议** | 实现轻量文件快照系统，在工具调用前后 capture/diff |

### 3.3 Skills: 远程注册表 + 内置Skill

| 项目 | 状态 |
|------|------|
| Opencode | `skills.urls` + `skills.paths` 配置；远程注册表 (index.json) 带版本缓存；内置 `customize-opencode` skill |
| Morpheus | 仅本地文件系统扫描 |
| **建议** | 支持从 URL 拉取 skill，实现版本化缓存 |

### 3.4 ACP (Agent Client Protocol)

| 项目 | 状态 |
|------|------|
| Opencode | 完整的远程 Agent 协议，支持外部客户端连接、session 管理、权限转发、工具调用 |
| Morpheus | 无远程 Agent 协议 |
| **建议** | 基于 MCP 或独立协议实现远程 agent 访问能力 |

### 3.5 Shell Arity 权限分类

| 项目 | 状态 |
|------|------|
| Opencode | 160+ 命令的 arity 字典，用于智能识别 shell 命令并评估权限（如 `docker run` 识别为 docker 命令而非 run 目录） |
| Morpheus | 基于正则的模式匹配 |
| **建议** | 实现 shell 命令解析器，按命令前缀分级评估权限 |

### 3.6 模型定义 + 成本追踪

| 项目 | 状态 |
|------|------|
| Opencode | 完整模型定义：capabilities/costs/limits/modalities/variants；per-message 成本追踪 |
| Morpheus | 仅有 provider_id + model_id 字段 |
| **建议** | 添加模型能力/成本定义，实现 token 用量和成本追踪 |

---

## 4. 🟢 P3 - 低优先级 / 架构差异

### 4.1 CodeMode 沙箱

| 项目 | 状态 |
|------|------|
| Opencode | 受限 JS 解释器，模型可用 `execute` 工具编写脚本协调多工具调用 |
| Morpheus | 无等效功能 |
| **注意** | Go 实现难度大；建议等需求明确后再考虑。可调研 WASM 沙箱方案。 |

### 4.2 Effect 框架 / 依赖注入

| 项目 | 状态 |
|------|------|
| Opencode | Effect.ts 的 Context/Layer/Scope 系统构成完整 DI |
| Morpheus | Go 中无直接等效 |
| **注意** | 这是语言层面的架构差异。morpheus 不需要照搬 Effect.ts，但可借鉴其按需组合的 Service 模式。建议调研 Go 的 dig/wire 或 泛型-based DI。 |

### 4.3 事件总线

| 项目 | 状态 |
|------|------|
| Opencode | `bus.ts` 全局事件发布/订阅 |
| Morpheus | 简单 callback 模式 |
| **注意** | morpheus 的 callback 模式基本够用；如需解耦可考虑 Go channel + pub/sub 模式实现。 |

### 4.4 Instance 多租户

| 项目 | 状态 |
|------|------|
| Opencode | `InstanceState` 每个项目目录隔离状态 |
| Morpheus | 单进程全局状态 |
| **建议** | 如果有多项目需求，可引入 Instance 概念隔离项目状态 |

### 4.5 Agent 生成

| 项目 | 状态 |
|------|------|
| Opencode | LLM 根据描述自动生成 Agent 配置 |
| Morpheus | 无 |
| **建议** | 低优先级；等核心功能对齐后再考虑 |

### 4.6 WebSearch / CodeSearch 工具

| 项目 | 状态 |
|------|------|
| Opencode | websearch 和 codesearch 工具 |
| Morpheus | 已有 webfetch；websearch 和 codesearch 缺失 |
| **建议** | morpheus `internal/tools/` 已有 webfetch，可补充 websearch。CodeSearch 需语义搜索能力（向量嵌入），工作量较大。 |

---

## 5. Opencode 已有但 Morpheus 可兼容的功能

### 5.1 已兼容

| 功能 | 文件 | 状态 |
|------|------|------|
| Opencode skill 目录发现 | `internal/skill/loader.go:360-384` | ✅ 已实现 |
| Opencode MCP 目录发现 | `internal/tools/mcp/mcp.go:133` | ✅ 已实现 |
| Opencode 工作流集成 | `.github/workflows/opencode.yml` | ✅ 已实现 |

### 5.2 可新增兼容

| 功能 | 建议 |
|------|------|
| 扫描 `.opencode/agent/` 加载 agent | 代码量小，可快速实现 |
| 扫描 `.opencode/command/` 加载命令 | 代码量小，可快速实现 |
| 扫描 `.opencode/plugins/` 加载设置 | 需调研 Go 动态加载方案 |

---

## 6. 文件级映射 (新增/更新)

| Morpheus 文件 | Opencode 对应文件 | 说明 |
|--------------|-------------------|------|
| `internal/app/compactor.go` | `session/compaction.ts` | 需添加尾部保留 + 结构化摘要 |
| `internal/app/processor.go` | `session/processor.ts` | 需添加 overflow 重放 |
| `internal/session/` | `session/overflow.ts`, `session/retry.ts` | 需添加溢出检测 + 重试策略 |
| `internal/plugin/` | `plugin/loader.ts`, `core/src/plugin/` | 插件系统需大规模重构 |
| `internal/app/permission_tracker.go` | `core/src/permission.ts` | 需添加 SQLite 持久化 |
| `internal/tools/` | `tool/apply_patch.ts`, `tool/truncate.ts` | 新工具实现 |
| `internal/app/memory.go` | 无直接对应 | morpheus 的 3层记忆保留优势，可保留 |
| `internal/command/` (新建) | `command/` | 斜杠命令系统 |
| `internal/tools/websearch/` | `tool/websearch.ts` | 网络搜索工具 |

---

## 7. 建议实施路线图

```
Phase 1 (P0) - 立即执行 (1-2周)
├── 尾部保留 + 结构化摘要模板 → compactor.go
├── CompactionPart 消息类型 → 消息模型
└── Overflow 重放 → processor.go

Phase 2 (P1) - 短期 (2-4周)
├── 斜杠命令系统 → internal/command/
├── apply_patch 工具 → internal/tools/
├── Agent Markdown 加载 → internal/app/
└── 工具输出截断管理 → internal/tools/truncate/

Phase 3 (P2) - 中期 (1-2月)
├── 权限 SQLite 持久化 → internal/app/permission_tracker.go
├── 多 Provider 配置 → internal/config/
├── 快照 + Diff 追踪 → internal/app/snapshot/
├── 模型成本定义 → internal/config/
└── Shell arity 权限 → internal/tools/cmd/

Phase 4 (P3) - 长期
├── 远程技能注册表 → internal/skill/
├── 插件系统升级 → internal/plugin/
├── ACP 协议 → internal/acp/
├── Instance 多租户 → internal/app/
├── WebSearch 工具 → internal/tools/
└── CodeMode 沙箱 → internal/codemode/
```

---

## 8. 核心差异总结

| 维度 | Opencode 2026-07 | Morpheus 当前 | 差距评估 |
|------|-----------------|---------------|----------|
| 上下文压缩 | 尾部保留 + CompactionPart + Overflow 重放 | 4层全量压缩 | **大** |
| 插件系统 | npm 包 + V2 SDK + Auth 插件 | 内部 hook 注册 | **大** |
| 斜杠命令 | 内置+配置+文件+MCP+Skill | 无 | **大** |
| 权限系统 | SQLite 持久化 + action/resource | 内存 + pattern | **中** |
| 多 Provider | Record 多 Provider | 单 planner | **中** |
| 工具集 | ~41 个工具 | ~13 个工具 | **中** |
| Agent 定义 | 配置 + Markdown 文件 | 仅配置 | **中** |
| ACP 协议 | 完整远程 Agent 协议 | 无 | **中** |
| 快照/Diff | 自动文件状态追踪 | git stash 手动 | **中** |
| MCP | 官方 SDK + OAuth + Prompts | 自实现 + 基础 | **中** |
| Effect DI | Effect.ts 完整 DI | 构造函数注入 | **语言差异** |
| 3层记忆 | 无（用 compaction 替代） | 有（Morpheus 优势） | **保留** |
| DAG 协调 | 无 | 有（Morpheus 优势） | **保留** |
| 意图分类路由 | 无 | 有（Morpheus 优势） | **保留** |

---

_文档版本: 1.0_
_最后更新: 2026-07-08_
