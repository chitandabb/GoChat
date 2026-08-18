// ws.js 负责 WebSocket 连接的建立、断线重连与消息路由。
// 连接身份来自 Access Token（/wss?token=），见 docs/design/api.md。
//
// 设计要点：
//   1. socket.onmessage 由本模块独占，解析后经 messageBus 分发，
//      组件不得再覆盖 store.state.socket.onmessage（重连会换新 socket 实例，覆盖会丢 handler）。
//   2. 断线后指数退避无限重连（登录态存在时），重连前先刷新 Access Token，
//      避免用 15 分钟过期的旧 token 握手被 401 拒绝。
//   3. 发送方走 sendChatMessage/sendRaw：连接断开时聊天消息进入待发队列，
//      重连成功后按序补发；通话信令不入队（过期信令没有意义）。
//   4. 服务端会主动断开慢客户端（见 docs/design/messaging.md），重连 + 各组件
//      监听 ws:connected 重拉数据即是丢消息的兜底闭环。
import { emit } from './messageBus'
import { refreshAccessToken } from './auth'

const MAX_BACKOFF_MS = 30 * 1000
const MAX_PENDING = 50
const STUCK_CONNECTING_MS = 15 * 1000

let reconnectAttempts = 0
let reconnectTimer = null
let manualClosed = false
let connectingSince = 0
let livenessTimer = null
let pendingQueue = []

function buildWsUrl(store) {
  const token = store.state.accessToken
  if (!token) {
    return ''
  }
  return store.state.wsUrl + '/wss?token=' + encodeURIComponent(token)
}

function setState(store, state) {
  store.commit('setConnectionState', state)
  emit('ws:state', state)
}

// 下行帧路由：服务端会发送纯文本帧（连接欢迎语、过载提示），不能假定都是 JSON。
function routeFrame(raw) {
  let message
  try {
    message = JSON.parse(raw)
  } catch (e) {
    console.log('[ws] 收到文本帧：', raw)
    return
  }
  if (!message || message.type === undefined) {
    console.log('[ws] 无法识别的帧：', message)
    return
  }
  if (message.type === 3) {
    emit('av-signal', message)
  } else {
    emit('chat-message', message)
  }
}

function flushPendingQueue(store) {
  if (!pendingQueue.length) {
    return
  }
  const queued = pendingQueue
  pendingQueue = []
  for (const payload of queued) {
    sendRaw(store, payload)
  }
}

function rawSend(store, payload) {
  const socket = store.state.socket
  if (socket && socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(payload))
    return true
  }
  return false
}

// sendRaw 立即发送，不排队；失败返回 false，调用方自行决定如何提示。
export function sendRaw(store, payload) {
  return rawSend(store, payload)
}

// sendChatMessage 发送聊天消息：连接断开时暂存队列，重连后按序补发。
// 返回 false 表示当前未直发成功（已入队），调用方可以给用户“重连后自动发送”提示。
export function sendChatMessage(store, payload) {
  if (rawSend(store, payload)) {
    return true
  }
  if (pendingQueue.length >= MAX_PENDING) {
    console.warn('[ws] 待发队列已满，丢弃消息')
    return false
  }
  pendingQueue.push(payload)
  return false
}

function clearReconnectTimer() {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
}

function teardown(store) {
  clearReconnectTimer()
  const socket = store.state.socket
  store.state.socket = null
  if (socket) {
    try {
      socket.close()
    } catch (e) {
      // ignore
    }
  }
}

function scheduleReconnect(store, delayMs) {
  clearReconnectTimer()
  setState(store, 'reconnecting')
  reconnectTimer = setTimeout(async () => {
    reconnectTimer = null
    if (manualClosed) {
      return
    }
    // Access Token 生命周期 15 分钟，重连前先续期（无 token 时也靠它恢复登录态），
    // 避免拿过期 token 握手被 401 拒绝
    try {
      await refreshAccessToken()
    } catch (e) {
      emit('auth:expired')
      setState(store, 'closed')
      return
    }
    if (manualClosed) {
      return
    }
    connectSocket(store, { autoReconnect: true })
  }, delayMs)
}

function ensureLivenessWatch(store) {
  if (livenessTimer) {
    return
  }
  livenessTimer = setInterval(() => {
    const socket = store.state.socket
    if (!socket) {
      return
    }
    // 半开连接兜底：握手长时间卡在 CONNECTING 就主动关闭触发重连
    if (socket.readyState === WebSocket.CONNECTING) {
      if (connectingSince && Date.now() - connectingSince > STUCK_CONNECTING_MS) {
        console.warn('[ws] 握手超时，重建连接')
        try {
          socket.close()
        } catch (e) {
          // ignore
        }
      }
    }
  }, 30 * 1000)
}

// closeSocket 主动断开（登出/换号），不触发自动重连。
export function closeSocket(store) {
  manualClosed = true
  pendingQueue = []
  teardown(store)
  setState(store, 'closed')
}

// connectSocket 建立 WebSocket 连接；断线后自动指数退避重连。
export function connectSocket(store, { autoReconnect = true } = {}) {
  const url = buildWsUrl(store)
  if (!url) {
    return
  }
  // 防止重复建连（如登录页与 App 先后各调一次）
  if (store.state.socket) {
    closeSocket(store)
  }
  manualClosed = false
  const socket = new WebSocket(url)
  connectingSince = Date.now()
  store.state.socket = socket
  socket.onopen = () => {
    if (store.state.socket !== socket) {
      return
    }
    reconnectAttempts = 0
    connectingSince = 0
    console.log('WebSocket连接已打开')
    setState(store, 'connected')
    emit('ws:connected')
    // 稍等握手稳定后补发断线期间排队的消息
    setTimeout(() => {
      if (!manualClosed) {
        flushPendingQueue(store)
      }
    }, 300)
  }
  socket.onmessage = (message) => {
    if (store.state.socket !== socket) {
      return
    }
    routeFrame(message.data)
  }
  socket.onclose = () => {
    // 陈旧连接（已被新连接替换/手动关闭）的回调直接忽略
    if (store.state.socket !== socket) {
      return
    }
    console.log('WebSocket连接已关闭')
    store.state.socket = null
    if (autoReconnect && !manualClosed) {
      const delay = Math.min(1000 * Math.pow(2, reconnectAttempts), MAX_BACKOFF_MS)
      reconnectAttempts += 1
      scheduleReconnect(store, delay)
    } else {
      setState(store, 'closed')
    }
  }
  socket.onerror = () => {
    console.log('WebSocket连接发生错误')
  }
  ensureLivenessWatch(store)
}

// 网络恢复时立即尝试重连，不等退避计时器
if (typeof window !== 'undefined' && window.addEventListener) {
  window.addEventListener('online', () => {
    const store = window.__gochatStore
    if (
      store &&
      !manualClosed &&
      store.state.accessToken &&
      (!store.state.socket || store.state.socket.readyState !== WebSocket.OPEN)
    ) {
      console.log('[ws] 网络已恢复，立即重连')
      reconnectAttempts = 0
      scheduleReconnect(store, 0)
    }
  })
}

// window online 监听需要拿到 store 引用；由 main.js 在启动时注入。
export function bindStore(store) {
  window.__gochatStore = store
}
