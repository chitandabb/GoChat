# GoChat 系统架构

## 文档状态

- 本文描述 GoChat 的**目标架构**:运行拓扑、组件职责、依赖装配方式与核心数据流。
- 当前实现与目标的主要差异:依赖装配仍为包级单例 + `init()`(现状见 [`../architecture.md`](../architecture.md));AI 助手模块尚不存在。差异在 M0 / M3 收敛。
- 已确认的架构决策:单实例部署、Kafka 作为可切换消息模式保留、依赖装配全量收口到 `internal/bootstrap`、Eino 以同进程模块运行。决策理由沉淀在 `../decisions/`。

## 架构目标

- 单实例交付一套完整 IM + AI 助手能力,部署面最小(一个 Go 进程 + MySQL + Redis,Kafka 可选)。
- 所有对象的构造、启动顺序、关闭顺序在 `internal/bootstrap` 中显式可见,无隐式 `init()` 装配。
- 业务身份来自认证上下文而非前端传参;WebSocket 与 HTTP 共享同一套鉴权。
- 消息链路的可靠性与降级行为可解释、可压测(详见 [`messaging.md`](messaging.md))。
- AI 能力作为普通模块接入,不改变 IM 核心链路的可用性:AI 故障最多导致助手不可用,不影响聊天。

## 系统上下文

```text
浏览器(Vue 3 SPA)
  │  HTTPS(REST /api/v1) + WSS(/wss) + SSE(AI 流式)
  ▼
GoChat 进程(Gin + 聊天服务器 + AI 模块)
  │
  ├── MySQL     事实存储(6 张业务表)
  ├── Redis     缓存 / 登录态 / 验证码类临时数据
  ├── Kafka     可选消息模式(messageMode=kafka 时)
  ├── 本地磁盘  静态资源(头像 / 文件),经 /static 暴露
  ├── 阿里云短信 验证码发送与校验(Dypnsapi)
  └── LLM 服务  模型 API(M3,经 Eino 调用,含搜索工具等外部 API)
```

## 运行拓扑

### 部署形态(已确认:单实例)

一个 Go 进程同时承担三种运行角色,不拆分服务:

| 角色 | 职责 |
| --- | --- |
| HTTP API | REST 接口、静态资源、SSE |
| Chat Server | WebSocket 长连接管理与消息路由(channel 或 Kafka 模式) |
| AI 模块 | Eino 编排、工具调用、SSE 输出(M3) |

- 本地开发:`go run ./cmd/gochat` + docker-compose 起 MySQL / Redis(/ Kafka)。
- 部署:单容器(Dockerfile)+ compose 编排依赖;HTTPS 证书由配置指定,HTTP 模式用于本地。
- 明确不做:多实例水平扩展、跨实例会话路由、消息分区。Kafka 模式保留为架构演示,不承诺多实例语义。

## 依赖装配(目标,M0)

### bootstrap 作为唯一装配点

```text
cmd/gochat/main.go
  └── internal/bootstrap
        1. 加载配置(config.Load,失败即退出)
        2. 构造基础设施客户端:zlog → MySQL(GORM) → Redis →(可选)Kafka
        3. 构造业务 service:显式传入 db / redis / config / smsProvider / logger
        4. 构造聊天服务器(按 messageMode 选 channel / kafka 实现)
        5. 构造 controller 并注册路由(Router Group,/api/v1)
        6. 返回 App{engine, chatServer, closers...}
main 持有 App:启动聊天服务器 goroutine → 启动 HTTP(S) → 监听信号 → 逆序关闭
```

### 装配规则

- 业务包禁止副作用 `init()`;所有包级单例(`dao.GormDB`、`https_server.GE`、`chat.ChatServer`、`gorm.XxxService`)在 M0 中移除。
- Service 结构体显式持有依赖;controller 显式持有 service;方向永远是外层注入内层。
- 接口只在真实外部边界引入(短信 provider、LLM client、未来对象存储),不为每个 struct 机械配接口。
- 关闭顺序与构造顺序相反:HTTP → 聊天服务器(排空连接)→ Kafka → Redis → MySQL → 日志刷盘。当前"退出时清空 Redis 全部 key"的行为在 M0 移除,改为按前缀清理属于本进程的易失数据。

## 组件职责

### Gin HTTP API

路由分组、参数绑定、统一响应与错误码、鉴权 / 管理员中间件(M1)、SSE 端点(M3)。契约见 [`api.md`](api.md)。

### Chat Server(双模式)

- 两种实现共享同一套消息处理语义:接收 → 落库 → 路由推送。
- channel 模式:进程内 `Login / Logout / Transmit` 通道 + select 循环,在线表为内存 map。
- Kafka 模式:消息经 Kafka topic 中转后回到同一处理逻辑,用于演示解耦形态。
- 反压、降级与投递语义是 M2 的核心交付,规则统一定义在 [`messaging.md`](messaging.md),两种模式必须同时满足。

### MySQL

唯一事实来源:账号、关系、群、会话、消息全部先落库。缓存、内存在线表、推送通道都不是事实来源。表设计与迁移约定见 [`database.md`](database.md)。

### Redis

- 高频读缓存(Cache-Aside + TTL + `{业务}_{实体ID}` 前缀批量失效)。
- M1 起承载登录态(refresh token / 会话凭据)与在线状态辅助数据。
- Redis 不可用时的预期行为:读写穿透到 MySQL,接口变慢但不失败;登录态类数据例外,其可用性策略在 `api.md` 鉴权节定义。

### 阿里云短信(Dypnsapi)

验证码发送与校验都在云端完成,本服务只发起调用;以 provider 接口注入,便于本地 mock。

### 静态资源

头像与文件走本地磁盘 + `/static` 路由;对象存储(MinIO)为远期方向,不进本期。

## Eino 与模型服务边界(已确认:同进程模块)

- `internal/service/ai` 作为普通模块由 bootstrap 装配,不设独立进程或第二可执行入口。
- 边界规则:
  - AI 模块**只读**调用现有 service 获取上下文(如会话消息),不直接操作 IM 的表;
  - 模型 API Key 等凭据只存在于配置,经 bootstrap 注入;
  - 每次调用受超时与并发上限约束,AI 请求失败不得影响消息主链路;
  - 输出经 SSE 返回,最终落库为普通消息,复用既有消息链路。
- 编排、工具清单、模型选型在 [`ai-assistant.md`](ai-assistant.md) 中定义。

## 核心数据流

### HTTP 业务请求

```text
浏览器 → Gin 中间件链(请求日志 → 鉴权 → 管理员校验[管理接口])
      → controller(绑定 + 校验) → service(业务 + 事务) → MySQL / Redis
      → 统一响应
```

### 消息收发(channel 模式)

```text
发送方 WS → Chat Server 校验(会话可达性)→ 消息落库(Unsent)
         → 查在线表:在线 → 写入接收方发送通道 → 推送成功置 Sent
                     离线 → 保持 Unsent,接收方上线后由会话历史拉取兜底
```

### AI 助手请求(M3)

```text
用户在会话中触发 → /api/v1 AI 端点(鉴权)→ ai service(Eino 编排 + 工具调用)
                → SSE 流式返回浏览器 → 完成后落库为会话消息
```

## 失败边界

| 故障 | 预期行为 |
| --- | --- |
| Redis 不可用 | 缓存穿透回源 MySQL;登录态按 `api.md` 定义的策略处理 |
| Kafka 不可用(kafka 模式) | 消息模式启动失败即退出;运行中断连按 `messaging.md` 重试策略 |
| LLM / 搜索 API 不可用 | AI 助手返回明确错误,IM 主链路不受影响 |
| 进程退出 | 逆序优雅关闭;未推送消息保持 Unsent,依赖拉取兜底,不丢事实 |
| MySQL 不可用 | 服务不可用;不做降级写入,避免出现第二事实来源 |
