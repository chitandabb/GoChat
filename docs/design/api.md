# GoChat HTTP API、鉴权与 WebSocket 契约

## 文档状态

- 本文定义 M0 的响应 / 路由契约与 M1 的鉴权体系,是**目标设计**;当前实现为裸路径 + `ret` 约定 + 无鉴权。
- 已确认的决策:JWT 双 Token 登录态;路由只加 `/api/v1` 前缀分组、保留动作型路径;WebSocket 以 Query 携带 Access Token 鉴权。
- 全量接口清单以 [`../gochat-openapi3.json`](../gochat-openapi3.json) 为准,本文不重复罗列。
- AI 端点(SSE)契约在 [`ai-assistant.md`](ai-assistant.md) 中定义,遵循本文的响应与鉴权规则。

## 设计原则

- 身份只来自认证上下文:controller 从中间件注入的 claims 取 `uuid` / `is_admin`,请求体中的 `owner_id`、`uuid` 等身份字段一律废弃,不再信任前端传参。
- HTTP 状态码表达传输与鉴权层结果,业务码表达业务结果,两层职责不混用。
- controller 不手写 `gin.H`;所有出口经统一响应封装,所有错误经统一错误类型。
- 前后端同仓、无外部调用方:路径切换一次到位,不保留旧裸路径兼容层。

## 版本与路由分组(M0)

```text
/api/v1
  /user      账号、资料、注销
  /auth      登录、短信验证码、token 续期、登出(新增分组)
  /contact   联系人、申请、黑名单
  /group     群组
  /session   会话
  /message   消息、文件上传
  /chatroom  聊天室侧边栏
  /admin     管理后台(禁用/启用/删除、设管理员)
/wss         WebSocket 升级端点(不进 /api/v1)
/static      静态资源(不进 /api/v1)
```

- 路径保留动作型风格(如 `/api/v1/group/createGroup`),全面 RESTful 化记为远期,不在本期做。
- 中间件分层:全局(日志、Recovery、CORS)→ `/api/v1` 鉴权中间件(白名单:登录、注册、短信发送)→ `/admin` 管理员中间件。

## 统一响应(M0)

```json
// 成功
{ "code": 0, "message": "ok", "data": { ... } }
// 失败
{ "code": 40101, "message": "登录已过期", "data": null }
```

| HTTP | 场景 | 业务码段 |
| --- | --- | --- |
| 200 | 业务成功 | `0` |
| 400 | 参数绑定 / 校验失败、业务规则拒绝 | `40001` 参数错误;`400xx` 业务错误 |
| 401 | 未认证、token 过期 / 无效 | `401xx` |
| 403 | 已认证但权限不足(非管理员、被禁用) | `403xx` |
| 404 | 资源不存在 | `40400` |
| 500 | 系统错误(对外不暴露细节) | `50000` |

- 取代现状:service 层 `ret` 0/-1/-2 约定与 `业务逻辑.md` 第 3、5 条废止;service 改为返回 `(data, error)`,error 为携带业务码的统一错误类型,由全局中间件序列化。
- `BindJSON` 失败统一返回 `40001` 参数错误,不再落入系统错误(修正现状)。
- 错误消息文案集中定义在错误码表,业务代码不写散落文案。

## 鉴权体系(M1,已确认:JWT 双 Token)

### Token 设计

| | Access Token | Refresh Token |
| --- | --- | --- |
| 形态 | JWT(HS256) | 不透明随机串 |
| 有效期 | 15 分钟 | 7 天 |
| 载荷 | `uuid`、`is_admin`、`exp`、`jti` | — |
| 存放(前端) | 内存,`Authorization: Bearer` | HttpOnly + Secure + `SameSite=Strict` Cookie,`Path=/api/v1/auth` |
| 存放(后端) | 无状态,不落库 | Redis 白名单,可撤销 |

- Redis 键:`auth_refresh_{uuid}_{tokenId}`,TTL 与有效期一致;值记录签发时间与设备信息(预留)。
- CSRF 缓解:Refresh Cookie 限定 `SameSite=Strict` 且 `Path` 只覆盖 `/api/v1/auth`,续期端点不接受跨站携带;其余接口凭 header 中的 Access Token,天然不受 CSRF 影响。
- 权衡叙事:Access 无状态换性能,Refresh 白名单换可撤销;禁用用户 / 登出场景靠撤销 Refresh + Access 短效自然过期收敛(最坏 15 分钟窗口)。

### 流程

- **登录**(密码 / 短信):校验通过 → 签发双 Token → 写 Redis 白名单 → 更新 `last_online_at`。
- **续期** `POST /api/v1/auth/refresh`:校验 Cookie 中 Refresh 在白名单 → 旋转(旧的删除、新的写入)→ 返回新双 Token。旋转检测到已用过的旧 token 视为泄露,撤销该用户全部 Refresh。
- **登出**:删除该 Refresh;前端丢弃 Access。
- **管理员禁用用户**:撤销该用户全部 `auth_refresh_{uuid}_*`。
- 密码登录失败限流:同一手机号连续失败按分钟级退避(Redis 计数),防撞库;短信发送沿用现有间隔限制。

### 中间件

1. 鉴权中间件:解析 Bearer Access Token → 校验签名与过期 → claims 注入 context;失败返回 401。
2. 管理员中间件:从 context 取 `is_admin`,非管理员返回 403;同时校验用户未被禁用。
3. 存量接口改造:所有从请求体读身份字段的位置改为从 context 读取(M1 内完成,一次到位)。

### Redis 不可用时

Access 校验不依赖 Redis,读接口不受影响;续期与登出暂不可用(返回 503 语义的系统错误),不做降级放行。

## WebSocket 契约(M1 改造,已确认:Query 携带 Access Token)

```text
wss://host/wss?token=<access_token>
```

- 升级前校验 token:无效 / 过期直接拒绝升级(HTTP 401),不建立连接;`client_id` 参数废弃,连接身份以 claims 中的 `uuid` 为准。
- 缓解措施(可复述的防护设计):Access 短效期 15 分钟限制了 query 泄露的时间窗口;访问日志不记录 `/wss` 的 query;全链路 wss 加密。
- 连接期间不重复校验过期:长连接的生命周期由连接本身管理,断线重连时用新 Access 重新握手;服务端收到用户被禁用 / 全量登出事件时主动断开该连接。
- 断开时更新 `last_offline_at`。
- 消息帧格式沿用现状(JSON,`type` 区分文本 / 文件 / 通话信令),投递语义见 [`messaging.md`](messaging.md)。

## 兼容与迁移

- M0:后端一次性切到 `/api/v1`,前端 axios `baseURL` 与 WS 地址同步修改;不保留旧路径。
- M1:登录接口返回结构变更(双 Token),前端登录态管理从 localStorage `userInfo` 改为内存 Access + Cookie Refresh;`userInfo` 仅作展示缓存,不再作为身份凭据。
