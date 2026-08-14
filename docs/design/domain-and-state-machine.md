# GoChat 领域模型与状态机

## 文档状态

- 本文以**当前表结构为准**描述领域对象与状态语义,并明确各状态机允许的迁移。
- 已确认的建模决策:`user_contact.status` 单字段不改,按联系类型拆成两张状态机图文档化;`message.status` 保持两态,把"已发送"的语义定义清楚。好友 / 群成员拆表、送达 / 已读回执均记为远期方向,不进本期排期。
- 涉及的字段级改造(唯一约束、`password` 扩容)在 [`database.md`](database.md) 中定义。

## 建模原则

- 内部关联一律使用业务 `uuid`(带类型前缀:`U` 用户、`G` 群、`M` 消息),自增 `id` 只作物理主键,不对外暴露。
- 双向关系用两行有向记录表达:A 拉黑 B 时,A 侧记 `BLACK`,B 侧记 `BE_BLACK`;镜像状态永远成对出现。
- 状态字段只允许出现文档中列出的迁移;不在图中的组合视为非法,应在 service 层拒绝。
- 软删除(`deleted_at`)统一表示"记录作废",与业务状态(禁用、解散等)语义分开。

## 领域对象总览

| 对象 | 表 | 说明 |
| --- | --- | --- |
| UserInfo | `user_info` | 账号、资料、管理员标记、禁用状态 |
| UserContact | `user_contact` | 用户 ↔ 用户 / 群 的有向关系与状态 |
| ContactApply | `contact_apply` | 加好友 / 加群申请 |
| GroupInfo | `group_info` | 群资料、群主、成员 JSON、加群方式 |
| Session | `session` | 单向会话条目(发起人视角) |
| Message | `message` | 消息事实与投递状态 |

## 核心领域对象

### UserInfo

- 身份:`uuid`(唯一索引)、`telephone`(登录标识,目标:数据库唯一约束)。
- 角色:`is_admin` 两角色扁平标记;`status` 由管理员控制(正常 / 禁用)。
- `password` 当前为明文 `char(18)`;M1 改为 bcrypt 哈希并扩容,见 `database.md`。
- `last_online_at` / `last_offline_at` 字段已存在,写入链路在 M1 落地。

### UserContact

- 有向关系:`user_id` → `contact_id`,`contact_type` 区分用户 / 群。
- 一条关系的完整语义由两行镜像记录共同表达;群关系只有成员 → 群一个方向。
- 不变量:`contact_type` 决定 `status` 的合法子集(见状态机);违反即数据缺陷。
- **目标不变量(M0)**:一对 `(user_id, contact_id)` 至多一行,状态迁移复用同一行,关系行不软删。现状与此不符(通过申请新建行、群路径写软删),改造见 `database.md`。

### ContactApply

- `user_id` 申请人,`contact_id` 被申请对象(用户或群),`status` 记录处理结果。
- 重复申请不新建记录,刷新 `last_apply_at` 并重置为申请中。
- 加群方式为直接加入(`add_mode=DIRECT`)时不产生申请记录。

### GroupInfo

- `owner_id` 群主;`members` JSON 数组存全部成员 uuid,`member_cnt` 冗余计数。
- 不变量:群主必须在 `members` 中;`member_cnt` 与 `members` 长度一致;解散为终态。
- 成员拆关系表(角色 / 加入时间)记为远期方向,本期不做。

### Session

- 单向条目:`send_id` 的视角看 `receive_id`(用户或群),双方各持一条。
- `last_message` / `last_message_at` 为列表展示的冗余字段,以 `message` 表为事实来源。
- 能否打开会话取决于双方 `user_contact` 状态与对象禁用状态,规则见下文"会话可达性"。

### Message

- 消息一经落库即为事实;`session_id` + `send_id` / `receive_id` 定位归属。
- `type` 为文本 / 语音 / 文件 / 通话;通话消息只存信令(`av_data`),不存内容。
- `status` 表达投递状态,语义见状态机。

## 状态机

### UserContact — 好友关系(contact_type = USER)

合法状态:`NORMAL`、`BLACK`、`BE_BLACK`、`DELETE`、`BE_DELETE`

```text
(申请通过)      → NORMAL           双方各建一条 NORMAL
NORMAL     → BLACK            我拉黑对方(对方侧 → BE_BLACK)
BLACK      → NORMAL           我解除拉黑(对方侧 BE_BLACK → NORMAL)
NORMAL     → DELETE           我删除对方(对方侧 → BE_DELETE)
DELETE/BE_DELETE → (重新申请) 走 ContactApply,通过后回 NORMAL
```

- `SILENCE`、`QUIT_GROUP`、`KICK_OUT_GROUP` 对好友关系**非法**。
- 拉黑期间不可互发消息、不可重复申请(申请侧被记 `BLACK`)。

### UserContact — 群关系(contact_type = GROUP)

合法状态:`NORMAL`、`SILENCE`、`QUIT_GROUP`、`KICK_OUT_GROUP`

```text
(入群)        → NORMAL          直接加入或申请通过
NORMAL    → SILENCE          被禁言(解除后回 NORMAL)
NORMAL    → QUIT_GROUP       主动退群
NORMAL/SILENCE → KICK_OUT_GROUP  被踢出;群解散时全体成员记为被踢出
QUIT_GROUP/KICK_OUT_GROUP → (重新入群) 回 NORMAL
```

- `BLACK` / `BE_BLACK` / `DELETE` / `BE_DELETE` 对群关系**非法**。

### ContactApply

```text
PENDING → AGREE    对方通过,建立 UserContact
PENDING → REFUSE   对方拒绝
PENDING → BLACK    对方拉黑申请人
REFUSE/BLACK → PENDING  再次申请(刷新 last_apply_at;BLACK 状态下是否允许由对方关系状态决定)
```

### GroupInfo / UserInfo 状态

```text
NORMAL ↔ DISABLE     管理员禁用 / 启用(用户与群一致)
NORMAL → DISSOLVE    群主或管理员解散,终态(仅群)
管理员删除用户 = 软删除(deleted_at),不占用 status;用户自助注销暂无
```

### Message

```text
Unsent → Sent
```

- **语义约定(已确认)**:`Sent` 表示消息已成功写入接收方的 WebSocket 推送通道;`Unsent` 表示已落库但尚未推送(接收方离线或推送失败)。
- 可靠性不依赖状态机推进:消息**先落库后推送**,接收方上线后通过会话历史拉取兜底,`Unsent` 消息不会丢失。
- 送达回执(Delivered)与已读回执(Read)明确不做,理由:需要客户端 ACK 协议与前端改造,群聊已读需按成员记录,复杂度与"深度优先"定位冲突;记为远期方向。

## 会话可达性

打开用户 / 群会话时按序校验(对应 `业务逻辑.md` 规则 4):

1. 关系存在性:双方关系不处于删除 / 被删除、退群 / 被踢出(解散视同被踢出)。
2. 对象可用性:对方用户或群未被管理员禁用。
3. 发言能力:群会话中自己未被禁言(可打开、不可发言的降级展示)。

任何一步不满足,返回对应业务提示,不进入会话。
