# 002. JWT 双 Token 登录态(而非 Session Cookie)

## 决策

登录态采用 JWT 双 Token:短效 Access Token(JWT,无状态,`Authorization` header)+ 长效 Refresh Token(不透明随机串,HttpOnly Cookie + Redis 白名单,可撤销、旋转续期)。WebSocket 握手以 Query 携带 Access Token,升级前校验。

## 理由

- 备选是 Session Cookie + CSRF(MESGuard 的选择,同样成熟)。GoChat 选 JWT 双 Token 的关键差异:
  - WebSocket 长连接鉴权用自包含 token 更直接,不需要在升级请求上处理 Cookie 域与 CSRF;
  - Access 无状态使高频接口鉴权不依赖 Redis(Redis 故障时读路径仍可用);
  - 与既定的对外叙事一致,"无状态性能 vs 白名单可撤销"的权衡本身是设计的一部分。
- 可撤销性靠 Refresh 白名单 + Access 短效期兜底:禁用 / 登出的最坏生效窗口为 Access 有效期(15 分钟),在线连接由服务端事件主动断开补偿。

## 后果

- 前端登录态管理重写:Access 内存持有、Refresh 走 Cookie,`localStorage userInfo` 降级为展示缓存。
- 旋转重放检测视为凭据泄露,撤销该用户全部 Refresh。
- WS 的 query token 有日志泄露面,缓解:短效期、访问日志不记 `/wss` query、全链路 wss;一次性 ticket 方案记为可选强化,不进本期。
- 续期与登出在 Redis 不可用时暂停服务,不降级放行。
