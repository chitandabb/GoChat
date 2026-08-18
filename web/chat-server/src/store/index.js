import { createStore } from 'vuex'

function trimTrailingSlash(value) {
  return (value || '').replace(/\/+$/, '')
}

function toWebSocketOrigin(value) {
  try {
    const url = new URL(value)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    return trimTrailingSlash(url.origin)
  } catch (error) {
    return ''
  }
}

// resolveBackendUrls 解析后端 API / WS 地址。
// 优先使用 VUE_APP_API_BASE_URL / VUE_APP_WS_BASE_URL；
// 未配置（production 构建）时按当前页面的 hostname 推导：
//   页面  http(s)://<host>:8080  → 后端 http(s)://<host>:8000（与后端默认端口一致，
//   同时兼容局域网/手机访问——页面从哪个 IP 打开，API 就打哪个 IP 的 8000）。
function resolveBackendUrls() {
  const explicitApi = trimTrailingSlash(process.env.VUE_APP_API_BASE_URL || '')
  const explicitWs = trimTrailingSlash(process.env.VUE_APP_WS_BASE_URL || '')

  let backendUrl
  if (explicitApi) {
    backendUrl = explicitApi
  } else if (typeof window !== 'undefined' && window.location) {
    const protocol = window.location.protocol === 'https:' ? 'https:' : 'http:'
    backendUrl = `${protocol}//${window.location.hostname}:8000`
  } else {
    backendUrl = 'http://localhost:8000'
  }

  const wsUrl = explicitWs || toWebSocketOrigin(backendUrl)

  return {
    backendUrl,
    wsUrl,
  }
}

const { backendUrl, wsUrl } = resolveBackendUrls()

// apiUrl 是后端 API 的统一前缀入口。
// backendUrl 仍指后端源站（静态资源 /static/** 不经过 /api/v1）。
const apiUrl = trimTrailingSlash(backendUrl) + '/api/v1'

export default createStore({
  state: {
    backendUrl,
    apiUrl,
    wsUrl,
    // Access Token 只存内存（安全要求，见 docs/design/api.md），刷新页面后通过
    // Refresh Cookie 静默续期恢复；userInfo 仅作展示缓存，不再是身份凭据。
    accessToken: '',
    userInfo: (sessionStorage.getItem('userInfo') && JSON.parse(sessionStorage.getItem('userInfo'))) || {},
    socket: null,
    // WS 连接状态：connected / reconnecting / closed，供 UI 展示连接条
    connectionState: 'closed',
    // 未读计数：key 为会话对端（用户 id 或群 id），value 为未读条数
    unreadMap: {},
    // 当前正在查看的聊天会话 id（用户 id 或群 id），用于未读豁免
    currentChatId: '',
    // 待处理的好友/加群申请数（红点）
    newContactCount: 0,
  },
  getters: {
    isLoggedIn: (state) => !!state.accessToken,
  },
  mutations: {
    setAccessToken(state, token) {
      state.accessToken = token || ''
    },
    clearAccessToken(state) {
      state.accessToken = ''
    },
    setUserInfo(state, userInfo) {
      state.userInfo = userInfo;
      sessionStorage.setItem('userInfo', JSON.stringify(userInfo));
    },
    cleanUserInfo(state) {
      state.userInfo = {};
      sessionStorage.removeItem('userInfo');
      state.unreadMap = {};
      state.currentChatId = '';
      state.newContactCount = 0;
    },
    setConnectionState(state, value) {
      state.connectionState = value || 'closed';
    },
    setCurrentChatId(state, chatId) {
      state.currentChatId = chatId || '';
    },
    addUnread(state, sessionKey) {
      if (!sessionKey) {
        return;
      }
      state.unreadMap = {
        ...state.unreadMap,
        [sessionKey]: (state.unreadMap[sessionKey] || 0) + 1,
      };
    },
    clearUnread(state, sessionKey) {
      if (!(sessionKey in state.unreadMap)) {
        return;
      }
      const next = { ...state.unreadMap };
      delete next[sessionKey];
      state.unreadMap = next;
    },
    clearAllUnread(state) {
      state.unreadMap = {};
    },
    setNewContactCount(state, count) {
      state.newContactCount = count || 0;
    },
  },
  actions: {
  },
  modules: {
  }
})
