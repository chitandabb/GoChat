// notify.js 全局提醒：未读角标数据（store.unreadMap）、标题栏未读数、
// 新消息提示音/通知、好友申请红点轮询。
// 由 App.vue 初始化一次，组件只读 store 里的计数，不各自轮询。
import axios from 'axios'
import router from '../router'
import store from '../store'
import { ElNotification } from 'element-plus'
import { on, sessionKeyOf } from './messageBus'

const BASE_TITLE = 'GoChat'
const NEW_CONTACT_POLL_MS = 30 * 1000

let inited = false
let pollTimer = null

export function totalUnread() {
  const map = store.state.unreadMap || {}
  return Object.values(map).reduce((sum, n) => sum + n, 0)
}

export function updateTitle() {
  const n = totalUnread()
  document.title = n > 0 ? `(${n}) ${BASE_TITLE}` : BASE_TITLE
}

// tryPlayBeep 用 WebAudio 合成短提示音；自动播放策略可能拦截，失败静默。
function tryPlayBeep() {
  try {
    const Ctx = window.AudioContext || window.webkitAudioContext
    if (!Ctx) {
      return
    }
    const ctx = new Ctx()
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.frequency.value = 660
    gain.gain.value = 0.04
    osc.connect(gain)
    gain.connect(ctx.destination)
    osc.start()
    osc.stop(ctx.currentTime + 0.15)
    osc.onended = () => ctx.close()
  } catch (e) {
    // ignore
  }
}

function messagePreview(message) {
  if (message.type === 2) {
    return `[文件] ${message.file_name || ''}`
  }
  return message.content || ''
}

function handleChatMessage(message) {
  const myId = store.state.userInfo.uuid
  if (!myId || message.send_id === myId) {
    // 自己消息的服务端回显不需要提醒
    return
  }
  const sessionKey = sessionKeyOf(message, myId)
  if (!sessionKey) {
    return
  }
  if (sessionKey === store.state.currentChatId) {
    // 正在看的会话不计数
    return
  }
  store.commit('addUnread', sessionKey)
  updateTitle()
  tryPlayBeep()
  ElNotification({
    title: message.send_name || '新消息',
    message: messagePreview(message),
    type: 'info',
    duration: 3500,
    onClick: () => {
      router.push('/chat/' + sessionKey)
    },
  })
}

async function pollNewContacts() {
  if (!store.state.accessToken || !store.state.userInfo.uuid) {
    return
  }
  try {
    const rsp = await axios.post(store.state.apiUrl + '/contact/getNewContactList', {
      owner_id: store.state.userInfo.uuid,
    })
    if (rsp.data && rsp.data.code === 0) {
      store.commit('setNewContactCount', (rsp.data.data || []).length)
    }
  } catch (e) {
    // 轮询失败静默，等下一轮
  }
}

// 处理完申请后可手动触发一次红点刷新
export { pollNewContacts }

// 登出时清空未读/红点并复位标题；轮询定时器常驻（未登录时 poll 内部自动跳过），
// 避免再次登录时因 initNotify 只执行一次而丢失轮询。
export function resetNotify() {
  store.commit('clearAllUnread')
  store.commit('setNewContactCount', 0)
  updateTitle()
}

export function initNotify() {
  if (inited) {
    return
  }
  inited = true
  on('chat-message', handleChatMessage)
  pollTimer = setInterval(pollNewContacts, NEW_CONTACT_POLL_MS)
  window.addEventListener('focus', pollNewContacts)
  pollNewContacts()
  updateTitle()
}
