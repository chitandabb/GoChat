# GoChat 当前进度

> 本文只跟踪仓库的**当前实现状态**。目标里程碑、依赖关系与验收标准见 [`design/delivery-plan.md`](design/delivery-plan.md)。
> 验收依据：`internal/service/auth` 单测、HTTP 鉴权矩阵实测、[`notes/压测报告.md`](notes/压测报告.md)（2026-08-14）。

## 已实现(IM 主体功能)

- [x] `cmd/internal` 项目布局，TOML 配置 + `GOCHAT_*` 环境变量覆盖。
- [x] 账号：手机号密码注册登录、阿里云短信验证码登录、资料修改。
- [x] 联系人：申请 / 通过 / 拒绝 / 拉黑 / 删除，人与群关系统一存于 `user_contact`。
- [x] 群组：建群、申请加群、退群、踢人、解散(成员存 `group_info.members` JSON)。
- [x] 会话与消息：会话开关，文本 / 文件 / 图片 / 视频消息收发与落库。
- [x] 音视频通话信令(WebRTC offer / answer / candidate 经 WebSocket 转发)。
- [x] WebSocket 聊天服务器：channel 模式与 Kafka 模式共用同一套分发语义。
- [x] 管理后台：禁用 / 启用 / 删除用户与群、设置管理员。
- [x] Zap 结构化日志与文件轮转；SIGINT / SIGTERM 优雅关闭。
- [x] Docker 化部署(Dockerfile + docker-compose + `.env` 覆盖 + Kafka broker)。

## M0:工程化基础

- [x] 统一响应结构与错误码：`{code, message, data}` 契约 + `pkg/apperr` + 全局错误中间件（见 `design/api.md`）。
- [x] `/api/v1` 路由前缀与 Router Group 分组（auth/user/admin/group/session/contact/message/chatroom）。
- [ ] main 显式依赖装配，去除包级单例 `init()` 链（推迟，见 `delivery-plan.md` 裁剪说明）。
- [ ] golang-migrate 迁移链（当前为 AutoMigrate + 启动期数据修复，见 `delivery-plan.md` 裁剪说明）。

## M1:鉴权与账号安全【简历亮点 1：双 Token 认证与连接安全】

- [x] bcrypt 密码哈希 + 存量明文登录懒升级（数据库已无明文，见 `internal/service/gorm/user_info_service.go`）。
- [x] JWT 双 Token：15min Access + 7d Refresh（Redis 白名单）+ 续期旋转 + 重放检测全量撤销（单测 + HTTP 实测）。
- [x] 鉴权 / 管理员中间件；全部接口身份从 context 读取，请求体伪造 uuid 无效（实测）。
- [x] WebSocket 握手鉴权（`/wss?token=`，无 token 拒绝升级）；`client_id` 废弃；CheckOrigin 白名单。
- [x] 手机号唯一索引（启动期去重 + 迁移旧索引）。
- [x] 在线状态：登录写 `last_online_at`、断连写 `last_offline_at`。
- [x] 登录失败按手机号分钟级限流；管理员禁用用户 → 撤销 Refresh + 断开在线连接（实测）。

## M2:消息管道与性能【简历亮点 2、3：长连接治理与可靠投递】

- [x] 投递语义落地：先落库后推送；下行非阻塞推送 + 连续丢弃计数 + 慢客户端断开（0.3–0.5s 实测）。
- [x] 心跳：Ping/Pong + 读超时清理半开连接；连接回收权唯一（消除关闭竞态）。
- [x] 上行两级有界缓冲 + 独立 Flush 转发 goroutine（突发不丢）+ 拒绝回写走单写者。
- [x] 消息幂等：uuid 唯一索引 + 1062 重复投递忽略（Kafka 重复消费场景）。
- [x] 状态写放大治理：Sent 状态异步批量落库（群风暴送达率 24.8% → 100%）。
- [x] Kafka 模式修正：分区键 = receiveId（保序）、acks 可配置（默认 one）、topic 自动创建、compose broker。
- [x] Kafka 模式调优：生产者 BatchTimeout 10ms / 消费者 MaxWait 100ms / broker fetch.max.wait 50ms，P99 45.6ms。
- [x] 自研 Go 压测客户端 `bench/` 入仓 + 四场景压测报告（1k–3k 连接 / P99<50ms / 群风暴 100% / 慢客户端）。

## M2:缓存验证【简历亮点 4：Redis 缓存优化】

- [x] 消息列表缓存改 Cache-Aside（写路径失效删除，不再读改写；修复单向 key 脏读）。
- [x] 全仓 `KEYS` 替换为 `SCAN`（`redis_service.go`）；TTL ±20% 随机抖动防雪崩。
- [x] 批量失效走 Pipeline（群解散清理全体成员缓存）。
- [x] 连接池配置（MaxOpen=100 / MaxIdle=50 / Lifetime=30min）。
- [x] 实测：命中率 99.75%；接口 P95 4.8ms → 2.2ms；暖路径 DB 业务查询 0 次/请求。

## M3:AI 助手

- [ ] Eino Agent + Tool Use（联网搜索、文件总结、文本扩写）。
- [ ] SSE 流式输出与 IM 消息流集成。

## 已知技术债(不阻塞里程碑，记录在案)

- 业务层单测覆盖仍薄（auth 有单测；消息管道依赖压测与手工回归）。
- `user_contact` 同一对主体可能存在多行（M0 统一行生命周期未做，推迟）。
- `internal/service/aes` 无调用方（死代码，M0 清理候选，推迟）。
- 群成员存 JSON 字段，群功能复杂化后需拆关系表（远期）。
- 压测在 Windows + Docker Desktop（MySQL 容器内）进行，DB 写延迟 6.8ms 高于 Linux 裸机；
  吞吐上限（约 140 msg/s 串行落库）受此环境约束，延迟指标不受影响。
