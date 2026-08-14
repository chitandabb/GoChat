// auth.js 封装登录态管理：
//   - refreshAccessToken：Refresh Token 续期（单飞：并发 401 只发一次刷新请求）；
//   - logout：撤销 Refresh + 通知服务端断开 WS + 清理本地登录态。
import axios from 'axios'
import router from '../router'
import store from '../store'
import { closeSocket } from './ws'

let refreshPromise = null

// refreshAccessToken 用 HttpOnly Cookie 里的 Refresh Token 换新 Access Token。
// 并发场景下只发一次刷新请求（single-flight）。
export function refreshAccessToken() {
  if (!refreshPromise) {
    refreshPromise = axios
      .post(store.state.apiUrl + '/auth/refresh', null, {
        withCredentials: true,
        _isRefresh: true,
      })
      .then((rsp) => {
        if (rsp.data && rsp.data.code === 0 && rsp.data.data && rsp.data.data.access_token) {
          store.commit('setAccessToken', rsp.data.data.access_token)
          return rsp.data.data.access_token
        }
        throw new Error('refresh failed')
      })
      .catch((err) => {
        store.commit('clearAccessToken')
        throw err
      })
      .finally(() => {
        refreshPromise = null
      })
  }
  return refreshPromise
}

// logout 主动登出：撤销 Refresh Token、断开 WS、清理本地登录态并回到登录页。
export async function logout({ silent = false } = {}) {
  try {
    await axios.post(store.state.apiUrl + '/auth/logout', null, { withCredentials: true })
  } catch (e) {
    // 登出是尽力而为，即使服务端撤销失败也继续清理本地态
  }
  try {
    await axios.post(store.state.apiUrl + '/user/wsLogout', {})
  } catch (e) {
    // ignore
  }
  closeSocket(store)
  store.commit('clearAccessToken')
  store.commit('cleanUserInfo')
  if (!silent) {
    router.push('/login')
  }
}

export default { refreshAccessToken, logout }
