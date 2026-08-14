# GoChat 架构现状

> 本文只记录**当前实现**的分层、依赖方向和工程约定,不描述目标设计。目标架构见 [`design/system-architecture.md`](design/system-architecture.md);两者的差异(依赖注入改造、路由分组等)在交付计划 M0 中收敛。

## 仓库布局

```text
cmd/gochat/            可执行入口(main)
api/v1/                HTTP controller 层 + 鉴权/管理员中间件
bench/                 自研压测客户端(conn/chat/group/slow/api)
internal/
  config/              TOML 配置加载 + 环境变量覆盖
  dao/                 GORM 初始化、连接池、AutoMigrate 与启动期数据修复
  dto/request/         请求 DTO(每接口一个文件)
  dto/respond/         响应 DTO
  https_server/        Gin 引擎创建、统一错误中间件与全部路由注册
  model/               GORM 模型(6 张业务表)
  service/
    gorm/              业务 service(含 GORM 查询与 Redis 缓存)
    auth/              双 Token 签发 / 校验 / 旋转 / 撤销 / 重放检测
    chat/              WebSocket 聊天服务器(channel 模式 / Kafka 模式共用分发语义)
    kafka/             Kafka 读写封装(分区键、acks、批量调优)
    redis/             Redis 客户端封装(SCAN 删除、TTL 抖动、Pipeline)
    sms/               阿里云短信验证码
    aes/               AES 工具(无任何调用方,死代码,M0 清理候选)
pkg/
  apperr/              统一业务错误与业务码
  constants/           常量(含 WS 连接治理参数)
  enum/                业务枚举(contact / group / message / user 等)
  ssl/                 HTTPS 重定向中间件
  util/                随机数等工具
  zlog/                Zap + lumberjack 日志封装
web/chat-server/       Vue 3 前端(Vue CLI + Element Plus + Vuex)
configs/               config.toml / config.dev.toml / config.docker.toml / config.kafka.toml
docs/notes/            压测报告等实测记录
```

## 分层与调用链

```text
https_server(路由+中间件) → api/v1(controller) → internal/service/gorm(业务) → internal/model + dao.GormDB
                                              ↘ internal/service/redis(缓存)
                                              ↘ internal/service/auth(双 Token 签发/校验/撤销)
/wss → api/v1/ws_controller(握手鉴权) → internal/service/chat(Server 或 KafkaChatServer) → dao / redis
bench/ → 自研压测客户端(conn/chat/group/slow/api 五场景)
```

- controller 只做 `BindJSON` → 调 service → 统一响应 `OK(c, data)` / `c.Error(err)`(全局错误中间件序列化)。
- service 返回 `(data, error)`,error 携带业务码(`pkg/apperr`);业务码映射 HTTP 400/401/403/404/500。
- 身份来源:controller 从鉴权中间件注入的 context 读取 `AuthUUID(c)`,请求体中的身份字段废弃。
- 消息链路:WebSocket 收到消息 → 上行有界缓冲 → 分发循环(落库 → 按在线表推送,非阻塞 + 慢客户端治理)。

## 当前工程约定(现状,非目标)

- **包级单例 + `init()` 装配**:`dao.GormDB`、`https_server.GE`、`chat.ChatServer`、`gorm.XxxService`、redis 客户端均为包级全局变量,在 `init()` 或首次使用时构造;`main` 不做显式依赖装配。目标状态(main 统一装配、显式依赖注入)见 M0 计划(当前推迟)。
- **路由分组 + 鉴权中间件**:`/api/v1` 分组(auth/user/admin/group/session/contact/message/chatroom),公开路由(登录/注册/短信/续期)外均挂鉴权中间件,`/admin` 额外挂管理员中间件;统一响应 `{code, message, data}` + 业务码(`pkg/apperr`)+ 全局错误中间件。
- **双 Token 认证**:短效 JWT(Access,15min,HS256)+ 不透明 Refresh(Redis 白名单,7d),续期旋转 + 重放检测;WS 握手以 `?token=` 鉴权。
- **配置**:TOML 为基座,`APP_ENV` / `CONFIG_FILE` 选择配置文件,`GOCHAT_*` 环境变量逐项覆盖(数据库、Redis、TLS、短信、JWT 密钥)。
- **双模式聊天服务器**:`kafkaConfig.messageMode` 取 `channel` 或 `kafka`;Kafka 模式消费循环把消息投递到与 channel 模式相同的 `Transmit` 通道,两模式共用同一套分发/推送/慢客户端治理语义。
- **消息管道**:先落库后推送;下行非阻塞推送 + 丢弃计数 + 慢客户端断开;心跳 Ping/Pong;上行两级有界缓冲(Transmit + 每连接 SendTo 由独立 Flush goroutine 转发);状态落库异步批量;消息 uuid 幂等。
- **缓存**:读回填(Cache-Aside,TTL ±20% 抖动)+ 写路径失效删除(消息列表不再读改写);全库删除走 SCAN;批量失效走 Pipeline;命中率与延迟实测见 `docs/notes/压测报告.md`。
- **日志**:业务与系统错误统一 `zlog.Error`,普通日志 `zlog.Debug`,输出走 Zap + 文件轮转;GORM SQL 日志生产静默。
- **优雅关闭**:`main` 监听 SIGINT / SIGTERM,依次关闭 Kafka、聊天服务器,并清理 Redis 全部 key。
- **测试**:`internal/service/auth` 有旋转/重放/撤销单测;消息管道与容量验证依赖 `bench/` 压测客户端(四场景,结果入仓 `docs/notes/压测报告.md`);业务层其余部分覆盖仍薄。

## 依赖方向规则

- `api/v1` 可依赖 `internal/service`、`internal/dto`;不得被下层反向依赖。
- `internal/service` 可依赖 `internal/model`、`internal/dao`、`pkg/*`;不得依赖 `api/v1` 与 `https_server`。
- `pkg/*` 不依赖 `internal/*`,保持可独立复用。
- 已知例外:`internal/service/chat` 直接操作 `dao.GormDB` 与 DTO,承担了部分 service 职责;该边界在 M2 消息管道改造时重新审视。
