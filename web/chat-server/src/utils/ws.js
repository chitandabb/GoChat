// ws.js 负责 WebSocket 连接的建立。
// 连接身份来自 Access Token（/wss?token=），见 docs/design/api.md。
let reconnectAttempts = 0
let reconnectTimer = null

function buildWsUrl(store) {
  const token = store.state.accessToken
  if (!token) {
    return ''
  }
  return store.state.wsUrl + '/wss?token=' + encodeURIComponent(token)
}

export function closeSocket(store) {
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (store.state.socket) {
    try {
      store.state.socket.close()
    } catch (e) {
      // ignore
    }
    store.state.socket = null
  }
}

// connectSocket 建立 WebSocket 连接，并做有限次自动重连（断线后用最新 token 重新握手）。
export function connectSocket(store, { autoReconnect = true } = {}) {
  const url = buildWsUrl(store)
  if (!url) {
    return
  }
  const socket = new WebSocket(url)
  store.state.socket = socket
  socket.onopen = () => {
    reconnectAttempts = 0
    console.log('WebSocket连接已打开')
  }
  socket.onmessage = (message) => {
    console.log('收到消息：', message.data)
  }
  socket.onclose = () => {
    console.log('WebSocket连接已关闭')
    if (autoReconnect && store.state.accessToken && reconnectAttempts < 3) {
      reconnectAttempts += 1
      reconnectTimer = setTimeout(() => {
        connectSocket(store, { autoReconnect: true })
      }, 1000 * reconnectAttempts)
    }
  }
  socket.onerror = () => {
    console.log('WebSocket连接发生错误')
  }
}
