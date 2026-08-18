// messageBus 是 WS 消息的全局分发中心。
// 连接层（ws.js）独占 socket.onmessage，把解析后的消息按事件分发到这里，
// 组件通过 on/emit 订阅，避免“组件直接覆盖 socket.onmessage”导致的
// 重连丢 handler、非当前会话消息被丢弃等问题。
//
// 约定的事件：
//   chat-message        —— 下行聊天消息（type 0/2），payload 为消息对象
//   av-signal           —— 音视频信令（type 3），payload 为消息对象（av_data 未解析）
//   ws:connected        —— WS 连接建立（含重连成功），组件借此做数据补偿（重拉历史/会话列表）
//   ws:state            —— 连接状态变化，payload: 'connected' | 'reconnecting' | 'closed'
//   auth:expired        —— 重连前刷新登录态失败，需要回登录页
//   session-list-changed—— 本端会话集合变化（openSession/删除会话/退群等），会话侧栏据此重拉

const listeners = new Map()

export function on(event, handler) {
  if (!listeners.has(event)) {
    listeners.set(event, new Set())
  }
  listeners.get(event).add(handler)
  // 返回解绑函数，配合 onBeforeUnmount 使用
  return () => off(event, handler)
}

export function off(event, handler) {
  const set = listeners.get(event)
  if (set) {
    set.delete(handler)
  }
}

export function emit(event, payload) {
  const set = listeners.get(event)
  if (!set) {
    return
  }
  for (const handler of Array.from(set)) {
    try {
      handler(payload)
    } catch (error) {
      console.error(`[messageBus] 事件 ${event} 处理异常:`, error)
    }
  }
}

// sessionKeyOf 把一条下行消息归到它所属的会话：
//   群聊消息     -> 群 id（receive_id 以 G 开头，收发双方同键）
//   我发出的回显 -> 对端用户 id
//   别人发给我的 -> 发送者用户 id
// 会话侧栏、未读计数、当前聊天窗口共用这一个推导，保证口径一致。
export function sessionKeyOf(message, myUserId) {
  if (!message || !message.receive_id) {
    return ''
  }
  if (message.receive_id[0] === 'G') {
    return message.receive_id
  }
  if (message.send_id === myUserId) {
    return message.receive_id
  }
  return message.send_id
}
