# GoChat 文档

本目录是项目文档的唯一存放地。

| 文档 | 用途 |
| --- | --- |
| [架构现状](architecture.md) | 当前分层、依赖方向、目录约定与工程现状 |
| [当前进度](roadmap.md) | 已实现清单与各里程碑待办;验收未过不得打勾 |
| [产品定位与业务流程](design/product-and-workflow.md) | 产品边界、用户角色、业务模块、分阶段交付骨架 |
| [领域模型与状态机](design/domain-and-state-machine.md) | 领域对象、关系状态机、消息状态语义、会话可达性 |
| [系统架构](design/system-architecture.md) | 运行拓扑、bootstrap 装配、组件职责、Eino 边界、失败边界 |
| [数据库设计](design/database.md) | 表职责、迁移方案与清单、事务边界、Redis 键约定 |
| [API 与鉴权](design/api.md) | 统一响应、路由分组、JWT 双 Token、WebSocket 契约 |
| [消息管道](design/messaging.md) | 投递语义、上/下行反压与降级、压测验收口径 |
| [AI 助手](design/ai-assistant.md) | 机器人形态、SSE 契约、Eino 编排、模型接入 |
| [交付计划](design/delivery-plan.md) | M0-M3 工作内容、目标日期、完成标准、变更管理 |
| [ADR 001](decisions/001-single-instance-monolith-and-bootstrap-di.md) | 为什么单实例单体 + bootstrap 手动依赖注入 |
| [ADR 002](decisions/002-jwt-dual-token-auth.md) | 为什么 JWT 双 Token 而非 Session Cookie |
| [ADR 003](decisions/003-message-delivery-semantics.md) | 为什么先落库后推送、两态状态机、丢弃降级 |

接口全量清单:[gochat-openapi3.json](gochat-openapi3.json)。

## 文档规则

- 仓库根 `README.md` 只作项目入口,不承载设计内容。
- 目标设计与当前实现严格分开:每篇设计文档的"文档状态"节声明未实现能力;`roadmap.md` 只在验收通过后打勾。
- 长期有效的争议决策沉淀到 `decisions/`;改变已有决策先改 ADR,再改设计,最后动代码。
- 同一命令、状态或设计解释不得在多处重复维护。

## 历史文档

- [notes/后续优化方案.md](notes/后续优化方案.md):早期优化清单。已纳入设计体系的条目以 design/ 为准;未纳入条目(RBAC、Keycloak、MinIO、举报审核等)在此保留作远期备忘。
- [业务逻辑.md](业务逻辑.md):早期约定。其中 `ret` / 响应约定将被 [design/api.md](design/api.md) 取代(M0 生效),会话可达性规则已并入领域文档。
- [notes/个人开发者 Golang接入 短信验证码 服务.md](notes/个人开发者%20Golang接入%20短信验证码%20服务.md):短信接入实录,保留作实现参考。
