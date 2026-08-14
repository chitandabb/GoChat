import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import store from './store'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import '@/assets/css/theme.css'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import axios from 'axios'
import { refreshAccessToken } from './utils/auth'
// 引入'https://webrtc.github.io/adapter/adapter-latest.js'
// import 'https://webrtc.github.io/adapter/adapter-latest.js'
// import '@/assets/css/font.css'
import '@/assets/css/chat.css'

// Refresh Cookie 需要跨域携带（前端 8080 -> 后端 8000），统一开启 credentials。
axios.defaults.withCredentials = true

// 请求拦截器：自动附带 Access Token。
axios.interceptors.request.use((config) => {
  const token = store.state.accessToken
  if (token && !config._isRefresh) {
    config.headers = config.headers || {}
    config.headers.Authorization = 'Bearer ' + token
  }
  return config
})

// 响应拦截器：
//   1. 业务失败响应（HTTP 非 2xx 但携带 {code,message,data}）归一化为正常 resolve，
//      使既有调用点的 rsp.data.code 判定逻辑保持可用；
//   2. 401（Access 过期）时单飞刷新 Refresh Token 后重放原请求；刷新失败则清登录态回登录页。
axios.interceptors.response.use(
  (response) => response,
  (error) => {
    const { response, config } = error
    if (
      response &&
      response.data &&
      typeof response.data === 'object' &&
      'code' in response.data
    ) {
      if (response.status === 401 && !config._retried && !config._isRefresh) {
        config._retried = true
        return refreshAccessToken()
          .then((newToken) => {
            config.headers = config.headers || {}
            config.headers.Authorization = 'Bearer ' + newToken
            return axios(config)
          })
          .catch(() => {
            store.commit('clearAccessToken')
            store.commit('cleanUserInfo')
            if (router.currentRoute.value.path !== '/login') {
              router.push('/login')
            }
            return Promise.resolve(response)
          })
      }
      return Promise.resolve(response)
    }
    return Promise.reject(error)
  }
)

const app = createApp(App)
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

// 启动前先尝试用 Refresh Cookie 静默恢复会话（刷新页面后保持登录态），
// 恢复成功后再挂载，避免路由守卫误判为未登录。
async function bootstrap() {
  if (!store.state.accessToken) {
    try {
      await refreshAccessToken()
    } catch (e) {
      // 无有效会话，走正常登录流程
    }
  }
}

bootstrap().then(() => {
  app.use(store).use(router).use(ElementPlus).mount('#app')
})
