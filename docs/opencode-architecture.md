# OpenCode 架构设计文档

## 1. 项目概述

OpenCode 是一个开源的 AI 编程助手，采用 monorepo 结构，使用 Bun 作为包管理器和运行时，TypeScript 作为主要开发语言。项目包含 CLI 工具、Web 应用、桌面客户端和后端服务多个组件。

**技术栈总览：**

| 层级     | 技术选型                                |
| -------- | --------------------------------------- |
| 运行时   | Bun, Node.js                            |
| 前端框架 | SolidJS + SolidStart                    |
| 后端框架 | Hono                                    |
| 数据库   | SQLite (本地), PlanetScale MySQL (云端) |
| ORM      | Drizzle ORM                             |
| 依赖注入 | Effect (函数式 Effect 系统)             |
| AI 集成  | AI SDK (统一 AI provider 接口)          |
| 桌面端   | Tauri (Rust), Electron                  |
| 部署     | SST (Cloudflare Workers)                |

---

## 2. 当前架构分析

### 2.1 项目结构

```
packages/
├── opencode/           # 核心 CLI 应用
│   ├── src/
│   │   ├── agent/      # AI Agent 逻辑
│   │   ├── cli/        # CLI 命令
│   │   ├── effect/     # Effect DI 层
│   │   ├── provider/   # AI Provider 抽象
│   │   ├── session/    # 会话管理
│   │   ├── storage/    # 数据库访问
│   │   ├── server/     # Hono API 服务
│   │   ├── tool/       # 工具注册表
│   │   └── bus/        # 事件总线
│   └── package.json
├── app/                # Web 应用 (SolidJS)
├── console/             # 后端服务 (Hono)
├── desktop/            # Tauri 桌面应用
├── ui/                 # 共享 UI 组件
└── sdk/js/             # JavaScript SDK
```

### 2.2 核心架构模式

#### 2.2.1 Effect-based Dependency Injection

项目使用 `effect` 库实现函数式依赖注入：

```typescript
// packages/opencode/src/agent/agent.ts:72-401
export const layer = Layer.effect(
  Service,
  Effect.gen(function* () {
    const config = yield* Config.Service
    const auth = yield* Auth.Service
    // ...
  }),
)
```

**优点：**

- 编译时类型安全
- 组合式 Layer 设计
- 更好的错误处理

**缺点：**

- 学习曲线陡峭
- Effect 生态相对小众
- 调试困难

#### 2.2.2 Instance-based Multi-tenancy

每个项目目录对应一个 `Instance`，实现状态隔离：

```typescript
// packages/opencode/src/project/instance.ts
Instance.provide(() => {
  const state = InstanceState.make()
  // per-directory isolated state
})
```

#### 2.2.3 Event-driven Architecture

事件总线实现组件解耦：

```typescript
// packages/opencode/src/bus/index.ts
PubSub // 进程内发布订阅
GlobalBus // 跨进程事件
```

#### 2.2.4 Plugin Architecture

插件系统实现可扩展性：

```
packages/opencode/src/plugin/
├── codex.ts    # 插件运行器
├── index.ts    # 插件接口定义
├── loader.ts   # 动态加载
└── shared.ts   # 共享工具
```

---

## 3. 设计优点

### 3.1 架构层面

| 优点                  | 说明                                                                                      |
| --------------------- | ----------------------------------------------------------------------------------------- |
| **清晰的模块边界**    | `agent`, `session`, `provider`, `tool` 等模块职责明确，模块间依赖通过 Effect Service 声明 |
| **多客户端支持**      | 同一后端服务支持 CLI TUI、Web、Desktop 三种客户端，通过统一 API 协议                      |
| **Instance 隔离设计** | 每个项目目录独立的 InstanceState，避免状态污染，便于多项目管理                            |
| **事件驱动解耦**      | Bus 系统使组件间松耦合，支持实时更新                                                      |
| **Provider 抽象**     | 20+ AI provider 统一接口，便于切换和扩展                                                  |

### 3.2 技术选型层面

| 优点                         | 说明                                                |
| ---------------------------- | --------------------------------------------------- |
| **Hono 轻量高效**            | 相比 Express/Hono 更快的路由性能，内置 OpenAPI 支持 |
| **Drizzle TypeScript First** | 更好的类型推断，schema 即代码                       |
| **SolidJS 细粒度响应**       | 优于 React 的运行时性能，更简单的响应式模型         |
| **SQLite 本地存储**          | 零配置、嵌入式，CLI 工具理想选择                    |
| **SST 简化部署**             | 基于 Cloudflare Workers，降低云成本                 |

### 3.3 工程化层面

| 优点                   | 说明                                           |
| ---------------------- | ---------------------------------------------- |
| **Turborepo Monorepo** | 统一的构建pipeline，依赖共享                   |
| **共享 UI 组件库**     | `packages/ui` 统一设计系统                     |
| **类型共享**           | SDK 和核心包类型统一，避免重复定义             |
| **统一的代码风格**     | ESLint + Prettier + 单一 package manager (Bun) |

---

## 4. 设计缺点

### 4.1 架构缺陷

| 缺点                      | 影响                                       | 严重程度 |
| ------------------------- | ------------------------------------------ | -------- |
| **Effect 学习成本高**     | 新开发者上手困难，代码审查难度大           | 高       |
| **Instance 状态管理复杂** | ScopedCache + InstanceState 耦合，调试困难 | 中       |
| **Bus 事件丢失风险**      | 跨进程事件无持久化，断线重连丢失状态       | 中       |
| **Plugin 沙箱不完善**     | 动态加载插件缺乏隔离，可能影响主进程       | 中       |

### 4.2 技术债务

| 缺点                    | 说明                                             |
| ----------------------- | ------------------------------------------------ |
| **TypeScript any 滥用** | 部分模块存在 `as any` 类型绕过                   |
| **错误处理不一致**      | 部分地方 try/catch，部分地方 Effect.catch        |
| **数据库抽象泄漏**      | `db.bun.ts` vs `db.node.ts` 条件导入，抽象不彻底 |
| **AI SDK 厂商锁定**     | 依赖第三方 SDK，升级可能破坏接口                 |

### 4.3 性能问题

| 缺点                  | 说明                                    |
| --------------------- | --------------------------------------- |
| **SQLite 并发限制**   | 嵌入式数据库不适合高并发写入场景        |
| **会话消息存储**      | Message/Part 大表单次查询慢，无分页策略 |
| **Effect 运行时开销** | 函数式 Effect 调用栈深，性能损耗        |
| **Bundle Size**       | 多模块打包后体积较大，CLI 冷启动慢      |

### 4.4 可维护性问题

| 缺点               | 说明                                        |
| ------------------ | ------------------------------------------- |
| **单会话文件过大** | `agent.ts` 400+ 行，单文件职责过多          |
| **Route 集中注册** | 所有路由在 `instance.ts` 一个文件，1500+ 行 |
| **测试覆盖不足**   | 缺少单元测试，核心逻辑无 E2E                |
| **文档与代码脱节** | API 文档手动维护，易过时                    |

---

## 5. Go + TypeScript 重构设计方案

### 5.1 设计目标

1. **保留优点**：Instance 隔离、Provider 抽象、事件驱动
2. **改进缺点**：简化 Effect 复杂度、改善类型安全、提升性能
3. **保持兼容**：CLI、Web、Desktop 客户端接口不变

### 5.2 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                        Clients                               │
│  ┌─────────┐  ┌─────────────┐  ┌──────────┐  ┌───────────┐ │
│  │ CLI TUI │  │  Web App    │  │  Desktop │  │  VSCode   │ │
│  └────┬────┘  └──────┬──────┘  └────┬─────┘  └─────┬─────┘ │
└───────┼──────────────┼──────────────┼──────────────┼───────┘
        │              │              │              │
        └──────────────┴──────────────┴──────────────┘
                               │
                    ┌──────────▼──────────┐
                    │   Go Backend (gRPC) │
                    │                      │
                    │  ┌────────────────┐  │
                    │  │  API Gateway   │  │
                    │  └───────┬────────┘  │
                    │          │            │
                    │  ┌───────▼─────────┐  │
                    │  │  Service Layer │  │
                    │  │                 │  │
                    │  │  ┌───────────┐  │  │
                    │  │  │ Session   │  │  │
                    │  │  │ Project   │  │  │
                    │  │  │ Provider  │  │  │
                    │  │  │ Tool      │  │  │
                    │  │  │ Agent      │  │  │
                    │  │  └───────────┘  │  │
                    │  └─────────────────┘  │
                    │          │            │
                    │  ┌───────▼─────────┐  │
                    │  │  Repository     │  │
                    │  │  Layer (SQLite)  │  │
                    │  └──────────────────┘  │
                    └───────────────────────┘
```

### 5.3 Go 后端设计

#### 5.3.1 项目结构

```
backend/
├── cmd/
│   └── server/
│       └── main.go           # 入口
├── internal/
│   ├── api/
│   │   ├── grpc/              # gRPC 服务定义
│   │   ├── http/             # HTTP 适配器 (REST)
│   │   └── middleware/        # 中间件
│   ├── domain/
│   │   ├── session/          # 会话领域
│   │   ├── project/          # 项目领域
│   │   ├── provider/         # AI Provider 领域
│   │   └── agent/            # Agent 领域
│   ├── service/              # 应用服务
│   ├── repository/           # 数据访问
│   └── infrastructure/
│       ├── sqlite/           # SQLite 实现
│       ├── ai/               # AI Provider 实现
│       └── bus/              # 事件总线
├── pkg/
│   └── types/                # 共享类型定义
├── proto/
│   └── opencode/v1/          # Protobuf 定义
└── go.mod
```

#### 5.3.2 核心模块设计

**依赖注入 - 使用 Uber FX**

```go
// internal/main.go
func main() {
    fx.New(
        fx.Provide(NewServer),
        fx.Provide(NewSessionService),
        fx.Provide(NewProjectRepository),
        fx.Provide(NewProviderRegistry),
        fx.Invoke(startServer),
    ).Run()
}
```

相比 Effect 的优势：

- 标准 Go 生态
- 编译时注入图验证
- 更好的 IDE 支持

**Service Layer**

```go
// internal/service/session.go
type SessionService interface {
    Create(ctx context.Context, req *CreateSessionRequest) (*Session, error)
    SendMessage(ctx context.Context, sessionID string, msg *Message) (*StreamResult, error)
    List(ctx context.Context, projectID string) ([]*Session, error)
}

type sessionService struct {
    repo      Repository
    provider  ProviderRegistry
    agent     AgentExecutor
    bus       EventBus
}
```

**Repository Pattern**

```go
// internal/repository/session.go
type SessionRepository interface {
    Create(ctx context.Context, s *Session) error
    GetByID(ctx context.Context, id string) (*Session, error)
    Update(ctx context.Context, s *Session) error
    ListByProject(ctx context.Context, projectID string) ([]*Session, error)
}

type sqliteSessionRepo struct {
    db *sql.DB
}
```

**Event Bus**

```go
// internal/infrastructure/bus/bus.go
type EventBus interface {
    Publish(ctx context.Context, topic string, event Event) error
    Subscribe(topic string, handler EventHandler) (<-chan Event, error)
    Unsubscribe(topic string) error
}

// 使用 Redis Pub/Sub 实现跨进程
type redisBus struct {
    client *redis.Client
}
```

#### 5.3.3 gRPC API 设计

```protobuf
// proto/opencode/v1/session.proto
syntax = "proto3";

package opencode.v1;

service SessionService {
    rpc Create(CreateSessionRequest) returns (CreateSessionResponse);
    rpc SendMessage(StreamMessageRequest) returns (stream StreamResponse);
    rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
    rpc GetSession(GetSessionRequest) returns (GetSessionResponse);
}

service AgentService {
    rpc Execute(ExecuteRequest) returns (stream ExecuteResponse);
    rpc Stop(StopRequest) returns (StopResponse);
}
```

#### 5.3.4 数据库 Schema (Drizzle 迁移到原生 SQL)

```sql
-- internal/repository/migrations/001_initial.sql

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    model TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE parts (
    id TEXT PRIMARY KEY,
    message_id TEXT NOT NULL REFERENCES messages(id),
    type TEXT NOT NULL,
    content TEXT NOT NULL,
    index INTEGER NOT NULL
);

CREATE INDEX idx_messages_session ON messages(session_id);
CREATE INDEX idx_parts_message ON parts(message_id);
```

### 5.4 TypeScript 前端设计

#### 5.4.1 项目结构

```
web/
├── src/
│   ├── app/                    # Next.js App Router
│   │   ├── (auth)/
│   │   │   └── login/
│   │   ├── (dashboard)/
│   │   │   ├── projects/
│   │   │   └── sessions/
│   │   ├── layout.tsx
│   │   └── page.tsx
│   ├── components/
│   │   ├── ui/                 # 基础 UI 组件
│   │   ├── chat/               # 聊天相关组件
│   │   ├── terminal/           # 终端组件
│   │   └── providers/          # Context Providers
│   ├── hooks/                  # 自定义 Hooks
│   ├── lib/
│   │   ├── api/                # API 客户端 (gRPC-web)
│   │   └── stores/             # 状态管理 (Zustand)
│   ├── types/                  # 共享类型
│   └── styles/
├── public/
├── package.json
└── next.config.js
```

#### 5.4.2 状态管理 - Zustand

```typescript
// src/lib/stores/session.ts
import { create } from "zustand"
import { createClient } from "@/lib/api/client"

interface Message {
  id: string
  role: "user" | "assistant"
  content: string
  parts: Part[]
}

interface SessionState {
  sessions: Map<string, Session>
  activeSession: string | null
  messages: Map<string, Message[]>

  createSession: (projectId: string) => Promise<string>
  sendMessage: (sessionId: string, content: string) => Promise<void>
  loadSessions: (projectId: string) => Promise<void>
}

export const useSessionStore = create<SessionState>((set, get) => ({
  sessions: new Map(),
  activeSession: null,
  messages: new Map(),

  createSession: async (projectId) => {
    const client = createClient()
    const response = await client.session.create({
      projectId,
      model: "claude-3-5-sonnet",
    })
    set((state) => {
      state.sessions.set(response.id, response)
      state.activeSession = response.id
      state.messages.set(response.id, [])
    })
    return response.id
  },

  sendMessage: async (sessionId, content) => {
    const client = createClient()
    const stream = client.session.sendMessage({
      sessionId,
      message: { role: "user", content },
    })

    // 乐观更新
    const msgId = crypto.randomUUID()
    set((state) => {
      const msgs = state.messages.get(sessionId) ?? []
      msgs.push({ id: msgId, role: "user", content, parts: [] })
      state.messages.set(sessionId, msgs)
    })

    // 处理流式响应
    for await (const chunk of stream) {
      if (chunk.delta) {
        set((state) => {
          const msgs = state.messages.get(sessionId)
          const last = msgs?.[msgs.length - 1]
          if (last?.role === "assistant") {
            last.content += chunk.delta
          } else {
            msgs?.push({
              id: crypto.randomUUID(),
              role: "assistant",
              content: chunk.delta,
              parts: [],
            })
          }
        })
      }
    }
  },
}))
```

#### 5.4.3 API 客户端 - gRPC-web

```typescript
// src/lib/api/client.ts
import { createClient } from "@/lib/api/grpc-client"

const client = createClient({
  baseUrl: process.env.NEXT_PUBLIC_API_URL,
})

export { client }

// src/lib/api/grpc-client/index.ts
import { SessionService } from "@opencode/api/proto/opencode/v1/session_connect"

export function createClient(config: { baseUrl: string }) {
  return createPromiseClient(
    SessionService,
    createTransport({
      baseUrl: config.baseUrl,
      protocol: "grpc-web",
    }),
  )
}
```

#### 5.4.4 Provider 组件

```typescript
// src/components/providers/index.tsx
'use client'

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <SessionProvider>
        <ProjectProvider>
          <NotificationProvider>
            {children}
          </NotificationProvider>
        </ProjectProvider>
      </SessionProvider>
    </QueryClientProvider>
  )
}

function SessionProvider({ children }: { children: React.ReactNode }) {
  const store = useSessionStore()

  useEffect(() => {
    // 订阅后端事件
    const es = new EventSource('/api/events/sessions')
    es.onmessage = (e) => {
      const event = JSON.parse(e.data)
      if (event.type === 'message_delta') {
        store.appendMessage(event.sessionId, event.delta)
      }
    }
    return () => es.close()
  }, [])

  return <SessionContext.Provider value={store}>{children}</SessionContext.Provider>
}
```

### 5.5 关键设计决策

#### 5.5.1 前后端通信

| 方案        | 优点                   | 缺点                     | 适用场景        |
| ----------- | ---------------------- | ------------------------ | --------------- |
| **gRPC**    | 高性能、强类型、流支持 | 浏览器支持有限、调试复杂 | 高性能内部服务  |
| **REST**    | 简单、广泛支持、易调试 | 类型安全弱、无原生流     | 公开 API        |
| **GraphQL** | 按需获取、类型安全     | 复杂度高、缓存策略复杂   | 数据密集型      |
| **tRPC**    | E2E 类型安全           | 仅 TypeScript            | 全栈 TypeScript |

**推荐：REST + Server-Sent Events**

- 简单易调试，适合 AI 流式响应
- Sse 实现与当前架构兼容
- 逐步迁移，无需一次性重写

#### 5.5.2 类型共享策略

```yaml
# 共享类型定义位置
protobuf/           # gRPC proto 定义 (TypeScript 和 Go 共享)
├── opencode/
│   └── v1/
│       ├── session.proto
│       └── project.proto

# 生成命令
protoc --go_out=. --go-grpc_out=. proto/opencode/v1/*.proto
protoc --ts_out=. --grpc-web_out=. proto/opencode/v1/*.proto
```

#### 5.5.3 迁移策略

**Phase 1: 后端核心迁移**

1. 用 Go 重写 Session/Project Service
2. 保持 Hono HTTP 接口不变
3. SQLite 逐步迁移

**Phase 2: 前端解耦**

1. 新建 Next.js 应用
2. API 客户端切换到 Go 后端
3. 逐步迁移页面

**Phase 3: 插件系统**

1. 保持 Plugin 接口兼容
2. Go 实现插件运行器
3. 迁移 Provider 实现

### 5.6 性能优化

| 优化点     | Go 实现               | TypeScript 实现    |
| ---------- | --------------------- | ------------------ |
| **并发**   | Goroutine + channel   | Web Workers        |
| **数据库** | 连接池 + Prepare      | 暂存 + 批量写入    |
| **缓存**   | In-memory LRU + Redis | React Query 缓存   |
| **流式**   | gRPC streaming        | Server-Sent Events |

### 5.7 监控与可观测性

```go
// Go: OpenTelemetry 集成
import "go.opentelemetry.io/otel"

func NewSessionService(cfg Config) (*SessionService, error) {
    tracer := otel.Tracer("session-service")

    return &sessionService{}, nil
}

// TypeScript: 前端 Sentry
import * as Sentry from '@sentry/nextjs'
```

---

## 6. 总结

### 6.1 架构评分

| 维度     | 当前分数 | 目标分数 |
| -------- | -------- | -------- |
| 可维护性 | 6/10     | 8/10     |
| 性能     | 7/10     | 9/10     |
| 可扩展性 | 8/10     | 9/10     |
| 开发体验 | 6/10     | 8/10     |
| 类型安全 | 7/10     | 9/10     |

### 6.2 关键建议

1. **立即行动**：
   - 拆分大型模块 (agent.ts, instance.ts)
   - 统一错误处理模式
   - 增加单元测试覆盖

2. **重构优先级**：
   - 低优先级：Effect → 标准 DI
   - 中优先级：Route 拆分
   - 高优先级：会话分页和索引优化

3. **Go 重写要点**：
   - 保持 Instance 隔离设计
   - 使用 Uber FX 替代 Effect
   - REST + SSE 保持客户端兼容
   - Protobuf 定义共享类型

---

_文档版本: 1.0_
_最后更新: 2026-04-02_
