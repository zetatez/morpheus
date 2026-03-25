# Claude Code 架构分析

> 来源：2026年3月31日 npm 包误含 `.js.map` 文件，暴露 51 万行 TypeScript 源码。
> 原文：虎嗅《Claude Code突然被泄露51万行源代码，暴露了AI Agent的完整设计哲学》

## 规模概览

- **1902 个源文件，512,685 行 TypeScript**
- 运行时：Bun
- 终端 UI：React + Ink
- 协议层：MCP + LSP
- 内置约 40 个工具、85 个斜杠命令

## 一、整体架构

### 1.1 五层架构

```
┌─────────────────────────────────────────────────────────┐
│  入口层 (Entrypoints)                                   │
│  CLI、桌面端、网页、IDE插件、SDK 统一路由               │
├─────────────────────────────────────────────────────────┤
│  运行层 (Runtime)                                       │
│  REPL 循环、TAOR 状态机、Hook 系统                      │
│  Think → Act → Observe → Repeat                         │
├─────────────────────────────────────────────────────────┤
│  引擎层 (Engine) — 46,000 行                            │
│  QueryEngine 单例：动态提示词拼装、提示缓存、流式响应   │
│  ┗━ 数百个提示碎片动态拼装，安全守则 5,677 token        │
├─────────────────────────────────────────────────────────┤
│  工具与能力层 (Tools & Caps)                            │
│  40 个隔离工具单元，工具基类 29,000 行                  │
├─────────────────────────────────────────────────────────┤
│  基础设施层 (Infrastructure)                            │
│  14 个缓存断点、远程 kill switch (GrowthBook)           │
└─────────────────────────────────────────────────────────┘
```

### 1.2 模块分布

| 目录 | 代码量 | 职责 |
|------|--------|------|
| `utils/` | 180K 行 | 权限系统、bash 安全、模型管理、会话存储 |
| `components/` | 81K 行 | 140+ Ink UI 组件 |
| `services/` | 53K 行 | API 封装、MCP 客户端、OAuth、遥测 |
| `tools/` | 50K 行 | 40+ Agent 工具实现 |
| `commands/` | 26K 行 | 50+ 斜杠命令 |
| `hooks/` | — | 权限检查钩子 |
| `bridge/` | — | IDE 双向通信 |
| `buddy/` | — | 虚拟宠物系统 |
| `coordinator/` | — | 多 Agent 协调器 |
| `memdir/` | — | 记忆目录 |
| `autoDream/` | — | 记忆巩固服务 |

### 1.3 核心设计原则

- **关注点分离**：query.ts 只管循环编排，工具只管逻辑，权限系统只管判断
- **递归 Agent**：AgentTool 递归调用 query.ts，子 Agent 拥有独立上下文窗口和工具集
- **动态提示词拼装**：非单一 system prompt，而是数百个提示碎片按需注入

---

## 二、Agentic Loop — 五步流水线

### 2.1 预处理流水线（query.ts）

```
Snip → 微压缩 → 上下文折叠 → 自动压缩 → 组装请求
```

- **Snip**：裁剪 token
- **微压缩**：工具结果就地缩减，保留与任务相关的部分
- **上下文折叠**：刻意放在 autocompact 之前——如果折叠能降 token 到阈值以下，autocompact 不触发
- **自动压缩**：触发阈值 = 上下文窗口大小 - 20K token（p99.99 compact summary 输出 17,387 token）
- **组装请求**：拼装后调用 Claude API

### 2.2 流式优化

**StreamingToolExecutor**：模型还在输出时就并行准备工具调用（不等完整输出）。

**Fallback 机制**：主模型失败时切换 fallback model，用 tombstone 消息清理孤儿 partial thinking blocks（防签名错误）。

**用户中断处理**：为每个未完成 tool_use block 补 error 类型 tool_result，保持协议一致性。

---

## 三、上下文压缩策略

### 3.1 四层压缩

| 层级 | 触发条件 | 特点 |
|------|----------|------|
| **微压缩** | 工具结果太长时 | 就地缩减，用户透明 |
| **自动压缩** | token 接近窗口 - 20K | 保留 20K 给压缩摘要输出 |
| **会话记忆压缩** | 跨压缩边界 | 保留关键约束（如第三轮的指令） |
| **上下文折叠** | 实验性 feature flag | 折叠优先于压缩 |

### 3.2 CLAUDE.md 的特殊地位

CLAUDE.md 四层加载（组织→用户→项目→本地），每次会话开始时全量加载，每次 API 请求都出现，**不受压缩影响**。

> 官方建议"把持久规则放在 CLAUDE.md 里"——这是架构约束，不是偏好。

---

## 四、三层 Agent 架构

### 4.1 内置 Agent

- **explore/plan**：工具白名单硬约束（禁用所有写操作工具）
- **general-purpose**：通用搜索和执行
- **Verification Agent**：
  - `background: true`（异步不阻塞）
  - `disallowedTools` 工具注册层面硬约束（非 prompt）
  - `criticalSystemReminder` 每轮重注入
  - 触发条件：3+ 文件修改或后端/基础设施变更
  - 输出格式：`VERDICT: PASS/FAIL/PARTIAL`（程序化解析）

### 4.2 Fork Agent

继承父 Agent 完整对话上下文和 system prompt，共享 prompt cache。关键约束：

1. **不能偷看**：输出写 `output_file`，主 Agent 只收到完成通知
2. **不能换模型**：`model: 'inherit'`，换模型 = cache 作废
3. **异步通知**：不阻塞主 Agent

### 4.3 Coordinator Mode

```
Claude 实例 → TeamCreateTool/SendMessageTool → 多个 worker agent
                                         ↓
                              独立 git worktree 隔离执行
```

---

## 五、安全架构 — 六层纵深防御

### 5.1 权限检查链路（useCanUseTool.tsx）

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

**设计哲学**：每层独立失败，不是最后一关才守门。

### 5.2 Bash 安全三层

| 层级 | 机制 | 防御内容 |
|------|------|----------|
| **静态检查** | 23 种模式匹配 | Zsh `=cmd` 扩展、zmodload 危险模块、Unicode 零宽空格、IFS 注入 |
| **语义分析** | tree-sitter AST | 理解命令意图，pathValidation + readOnlyValidation |
| **AI 分类器** | 独立 LLM 调用 | 未知命令判断，fallback = ask |

**cch Attestation**：Bun/Zig 层计算哈希认证，JS 层无法伪造。`DISABLE_TELEMETRY=1` 仍无法关闭。

### 5.3 信息控制三件套

| 机制 | 触发条件 | 效果 |
|------|----------|------|
| **Undercover Mode** | 非内部仓库操作 | 剥离所有 AI 标识、模型身份，禁止内部 repo 名/代号 |
| **反蒸馏** | `ANTI_DISTILLATION_CC=1` | 注入假工具定义 + 推理链加密签名摘要 |
| **原生客户端认证** | 始终 | 防伪造请求，Bun/Zig 层实现 |

---

## 六、记忆系统

### 6.1 三层记忆（对应认知科学）

| 层级 | 类比 | 机制 |
|------|------|------|
| **Semantic** | 长期语义记忆 | 高信号内容写入，矛盾剔除，RAG 检索 |
| **Episodic** | 情景记忆 | 时间索引，按需检索 |
| **Working** | 工作记忆 | 超限用指针代替内容 |

### 6.2 Auto-Dream

每隔 24h 或 5 次会话后，fork 子代理整合记忆。系统提示：

> "你正在做梦。反思你的记忆，合成持久知识，清理噪声。"

### 6.3 MEMORY.md 双重截断

200 行和 25KB，取先到者。有人曾在 200 行内塞了 197KB（超长单行索引）。

---

## 七、隐藏彩蛋

### 7.1 Capybara — 内部模型代号

- 源码多处出现，v8 false-claims rate 29-30%，v4 仅 16.7%
- 字符串用 `String.fromCharCode` 编码防泄露检测

### 7.2 KAIROS — 永不停歇模式

feature flag 关着但逻辑已写完：
- 每几秒主动检查值得做的事
- GitHub webhook 订阅 + cron 定时
- 24h 不间断运行
- SleepTool：`Each wake-up costs an API call, but the prompt cache expires after 5 minutes of inactivity`

### 7.3 Tengu — 内部项目代号

所有 analytics event 以 `tengu_` 开头：tengu_api_error、tengu_auto_compact_succeeded……

### 7.4 情绪检测

用正则表达式检测 "wtf""damn it""useless"，识别用户挫败状态。不用 LLM，省钱。

---

## 八、行业影响

### 8.1 技术影响

- 架构可复制，但对抗性测试积累不可复制
- 1 个月内 Agent 产品执行成功率行业基线将普遍提升
- 差异化最终回归模型权重

### 8.2 商业影响

| 方面 | 影响 |
|------|------|
| DMCA 下架 | 清理房间重写合法，无法完全阻止 |
| claw-code | 24h 完成 Python 重写，2h 获 5 万 star |
| IPO | 双向：客户疑虑 vs 技术背书（生产级复杂度） |
| 开源选项 | Anthropic 护城河在模型权重，非代码 |

---

## 九、工程启示

> **6400 行 AI 交互代码，50 万行工程基础设施。**

核心洞见：
1. **AI 是冰山一角**：流式并行预取、多层压缩、纵深安全、异步 Agent、记忆巩固……对用户完全不可见
2. **代码约束 > prompt 约束**：工具注册层面硬约束比 prompt 指导可靠得多
3. **安全是纵深，不是单点**：每层独立失败，任何一层发现问题就停
4. **架构服务于成本**：14 个缓存断点、20K 预留 token、prompt cache 策略——每一次缓存失效都要花真实的钱
