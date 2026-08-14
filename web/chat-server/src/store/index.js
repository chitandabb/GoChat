import { createStore } from 'vuex'

function trimTrailingSlash(value) {
  return (value || '').replace(/\/+$/, '')
}

function resolveBrowserOrigin() {
  if (typeof window === 'undefined' || !window.location) {
    return 'http://localhost:8000'
  }
  return window.location.origin
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

function resolveBackendUrls() {
  const backendUrl = trimTrailingSlash(
    process.env.VUE_APP_API_BASE_URL || resolveBrowserOrigin()
  )
  const wsUrl = trimTrailingSlash(
    process.env.VUE_APP_WS_BASE_URL || toWebSocketOrigin(backendUrl)
  )

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
    }
  },
  actions: {
  },
  modules: {
  }
})
