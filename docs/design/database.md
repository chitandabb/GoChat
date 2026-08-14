# GoChat MySQL 数据库设计

## 文档状态

- 本文以当前 6 张业务表为基线,定义表职责、逻辑关系、事务约定,以及 M0 / M1 需要落地的结构变更。
- 已确认的决策:引入**版本化迁移工具(golang-migrate)**管理结构变更;`AutoMigrate` 仅限本地空库初始化。邮箱唯一约束本期不做(邮箱不参与认证),记为远期。
- 状态字段的取值与迁移规则见 [`domain-and-state-machine.md`](domain-and-state-machine.md),此处不重复。

## 设计决策

### 数据库职责

MySQL 是唯一事实来源。Redis 缓存、内存在线表、推送通道中的数据都可以随时丢弃重建;任何业务判断最终以 MySQL 为准。

### 主键与业务标识

- 自增 `id` 为物理主键,不对外暴露、不参与业务关联。
- 业务关联一律使用带前缀 `uuid`(`U` 用户 / `G` 群 / `M` 消息,`char(20)`,唯一索引)。

### 软删除

- 除 `message` 外各表使用 GORM `deleted_at` 软删除,语义为"记录作废";业务状态(禁用 / 解散 / 拉黑)一律走 `status` 字段,不复用软删除。
- `message` 不做删除:消息是不可变事实。

### 迁移方案(已确认)

- `migrations/` 目录存放编号迁移:`000001_xxx.up.sql` / `000001_xxx.down.sql`,由 golang-migrate 执行,版本状态记录在 `schema_migrations` 表。
- 规则:
  - 每个迁移可独立执行、可回滚;涉及数据清洗的,清洗语句在加约束**之前**执行并在同一迁移文件内;
  - `AutoMigrate` 只允许用于本地空库快速初始化,CI / 部署环境一律走迁移链;
  - 模型 struct tag 与迁移必须同步修改,以迁移为准。

## 逻辑关系

```text
user_info 1 ──── n user_contact n ──── 1 group_info(contact_type=GROUP 时)
    │                  │(镜像成对,contact_type=USER 时两侧都是用户)
    │                  
    ├──── n contact_apply(user_id 申请人,contact_id 用户或群)
    ├──── n session(send_id 视角,receive_id 为用户或群)
    └──── n message(send_id;receive_id 为用户或群;session_id 归属会话)
```

外键约束不落库(与现状一致,由 service 层保证),换取写入灵活性;引用完整性问题通过状态机校验与软删除规避。

## 表设计

> 各表字段以 `internal/model/*.go` 为准,此处只记录职责、关键索引与目标变更。

### user_info

账号事实:身份(`uuid`)、登录标识(`telephone`)、资料、`is_admin`、`status`、在线时间戳。

| 项 | 现状 | 目标(M1) |
| --- | --- | --- |
| `telephone` | 普通索引 + 业务层查重 | **唯一索引**(迁移前清洗重复数据) |
| `password` | `char(18)` 明文 | `varchar(72)` 存 bcrypt 哈希 |
| `email` | 普通字段 | 不变;唯一约束记远期(不参与认证) |
| 默认头像 | 硬编码外部 CDN URL | 改为本地静态资源,默认值收敛到配置 |
| `last_online_at` / `last_offline_at` | 无写入链路 | 连接建立 / 断开时写入 |

### user_contact

有向关系表:`(user_id, contact_id)` 联合定位一条关系,`contact_type` + `status` 表达语义(合法组合见状态机文档)。

- 索引:`user_id`、`contact_id` 各自单列索引(现状保留)。
- **现状冲突(自查发现)**:当前代码"通过申请"每次**新建**关系行(不复用旧行),删除好友只改 `status`,群解散/踢出路径还会写 `deleted_at`——因此表里可能已存在同 `(user_id, contact_id)` 多行,直接加唯一索引会迁移失败或让通过申请报唯一键冲突。
- 目标(M0,需代码与迁移配套):
  1. 行生命周期统一:一对主体**至多一行**,关系建立改为 upsert(复用旧行、状态回 `NORMAL`),废除对关系行的软删除(全部语义走 `status`);
  2. `000002` 迁移:清理历史重复行与软删行(每键保留最新一行,`deleted_at` 置空)后,加 `(user_id, contact_id)` 联合唯一索引。
  3. 顺序约束:代码改造与迁移在同一里程碑内先后落地,先改代码再跑迁移。

### contact_apply

申请事实:`uuid` 唯一;`(user_id, contact_id)` 一条有效申请,重复申请刷新 `last_apply_at`。

### group_info

群事实:`uuid` 唯一;`members` JSON 存成员 uuid 数组,`member_cnt` 冗余计数,一致性由 service 层在同一事务内维护。拆群成员关系表为远期方向。

### session

会话条目:`uuid` 唯一;`(send_id, receive_id)` 单向一条。`last_message` / `last_message_at` 为展示冗余,事实以 `message` 为准。

### message

消息事实:`uuid` 唯一;`session_id`、`send_id`、`receive_id` 索引支撑会话历史与收件箱查询。`status` 语义见状态机文档;通话消息只存 `av_data` 信令。

## 目标变更清单(按里程碑)

### M0

1. 引入 golang-migrate,基线迁移 `000001_baseline`:将现有 6 张表结构固化为初始版本(空库可从零建出与现状一致的库)。
2. `000002_user_contact_unique`:清理重复关系行后,加 `(user_id, contact_id)` 联合唯一索引。

### M1

3. `000003_telephone_unique`:清洗重复手机号(保留最新活跃账号,其余标记软删除)后,`telephone` 加唯一索引。
4. `000004_password_bcrypt`:`password` 扩为 `varchar(72)`;存量明文在用户下次登录时透明升级为哈希(兼容期内先比对哈希、失败再比对明文并就地重写),兼容窗口结束后强制重置未升级账号。

## 事务边界

多表写必须在 `db.Transaction` 中完成,当前代码未系统使用事务,M0 起按以下清单执行:

| 业务 | 同一事务内的写 |
| --- | --- |
| 申请通过(好友) | 更新 `contact_apply.status` + 双方 `user_contact` 两行 |
| 申请通过(加群)/ 直接入群 | 申请状态 + 成员 `user_contact` + `group_info.members` / `member_cnt` |
| 退群 / 踢人 | 成员 `user_contact` + `group_info.members` / `member_cnt` |
| 解散群 | `group_info.status` + 全体成员 `user_contact` |
| 消息落库 | `message` 插入 + `session.last_message(_at)` 更新 |

规则:事务内不做网络调用(Redis 失效、WS 推送都放在事务提交之后);缓存失效失败只记日志,靠 TTL 兜底。

## Redis 键约定

- 缓存键:`{业务}_{实体ID}`(现状),批量失效按前缀删除;所有缓存键必须带 TTL。
- M1 新增登录态类键(refresh token 等)的命名与生存期在 [`api.md`](api.md) 鉴权节定义。
- 进程退出不再清空全库 key(见 system-architecture 装配节),只允许清理带本进程约定前缀的易失数据。
