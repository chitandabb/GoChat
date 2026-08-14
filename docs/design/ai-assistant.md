# GoChat AI 助手设计(Eino)

## 文档状态

- 本文是 M3 的目标设计,当前仓库无任何 AI 代码。
- 已确认的决策:助手以**机器人联系人**形态呈现;LLM 走 **OpenAI 兼容接口,首选 StepFun(阶跃星辰)**,凭据与模型名配置化(待补充);运行形态为同进程模块(见 [`system-architecture.md`](system-architecture.md))。
- 能力范围与简历叙事对齐:联网搜索、文件总结、文本扩写;SSE 流式输出并落库进入会话消息流。

## 产品形态(已确认:机器人联系人)

- 系统内置一个机器人账号(迁移 seed 一行 `user_info`,固定 `uuid`,如 `U_AI_ASSISTANT_01`,禁止登录、禁止被管理操作)。
- 新用户注册时自动与机器人建立 `user_contact` 双向 NORMAL 关系并可直接开会话;存量用户由同一迁移补齐。
- 用户在机器人会话中像普通聊天一样提问;会话、消息全部复用既有表结构,机器人回复是一条普通消息(`send_id` = 机器人 uuid)。

## 请求链路

机器人会话不走 WebSocket,收发都经 AI 端点(其余会话不受影响):

```text
用户在机器人会话发送 → POST /api/v1/ai/chat(统一鉴权)
  1. 用户消息落库(复用 message 表,Sent)
  2. 取该会话最近 N 条消息构造上下文(token 上限截断)
  3. Eino ReAct Agent 编排:模型 ⇄ 工具(搜索/文件/扩写)
  4. 过程与增量经 SSE 流式返回
  5. 完整回复落库为机器人消息,会话 last_message 更新
```

- 选择理由:避免 WS 与 SSE 双通道协调;SSE 天然适配 token 级流式;失败边界干净——AI 端点挂掉不影响 WS 聊天。
- 前端:机器人会话的发送改调 AI 端点并渲染 SSE;历史消息仍走既有会话历史接口。

## SSE 契约

- `POST /api/v1/ai/chat`,请求体:`{ "sessionId": "...", "content": "...", "messageUuid?": "引用的文件消息" }`。
- 响应 `text/event-stream`,事件类型:

| event | data | 说明 |
| --- | --- | --- |
| `delta` | 文本增量 | 逐段渲染 |
| `tool` | `{name, status}` | 工具调用开始 / 结束,前端展示"正在搜索…" |
| `done` | `{messageUuid}` | 完成,附落库后的消息 uuid |
| `error` | `{code, message}` | 业务码沿用 api.md 错误码表 |

- 连接中断:服务端检测到客户端断开即取消本次编排(context cancel);已产生的部分回复不落库,用户可重发。

## Eino 编排

- 结构:`ChatModel(openai 兼容)` + `ReAct Agent` + 工具集;不引入多 Agent、不做 RAG(与项目一差异化,保持轻量)。
- 工具清单(白名单,禁止模型调用清单外能力):

| 工具 | 实现 | 说明 |
| --- | --- | --- |
| `web_search` | Serper API | 联网搜索,返回摘要与来源 |
| `summarize_file` | 会话内文件消息 → 文本提取 → 模型总结 | 仅允许读本会话内、鉴权用户可见的文件;大小与类型白名单 |
| `expand_text` | Prompt 模板 | 文本扩写 / 润色,纯模型能力 |

- 上下文:最近 N 条会话消息(N 与 token 上限配置化);不跨会话取数,不读取其他用户数据。
- AI 模块只读调用现有 service 获取消息与文件,不直接写 IM 表;唯一的写入是经消息 service 落库回复。

## 模型接入(已确认:OpenAI 兼容,首选 StepFun)

```toml
[aiConfig]
baseURL   = "https://api.stepfun.com/v1"   # OpenAI 兼容端点,可切 DeepSeek/Qwen 等
apiKey    = ""                              # 环境变量 GOCHAT_AI_API_KEY 覆盖,待提供
model     = ""                              # 待提供
serperKey = ""                              # GOCHAT_AI_SERPER_KEY,待提供
timeoutSeconds  = 60
maxConcurrent   = 4
contextMessages = 20
```

- 经 Eino 的 OpenAI 兼容 ChatModel 组件接入,provider 切换只改配置不改代码;凭据只经 bootstrap 注入,不出现在日志。
- 待办:StepFun 的具体模型名与密钥由使用者提供后填入部署配置。

## 约束与失败边界

- 并发上限 `maxConcurrent`(信号量),超出返回"助手忙"业务错误;单次编排超时 `timeoutSeconds`,超时终止并返回错误事件。
- 模型 / 搜索 API 故障:SSE 返回 `error` 事件,IM 主链路零影响;不做自动重试(用户可重发)。
- 内容边界:助手输出仅落库为普通消息,不触发任何管理动作;工具白名单硬编码,不接受模型自定义工具调用。

## 验收要点(细化见 delivery-plan)

- 三个工具各有可复现演示用例;SSE 断流、超时、并发超限路径均被真实触发验证。
- 机器人会话在 AI 服务完全不可用时:历史可读、发送得到明确错误提示、其余会话收发正常。
