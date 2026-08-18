<template>
  <router-view />
</template>

<script>
import { onMounted, watch } from "vue";
import { useStore } from "vuex";
import axios from "axios";
import { ElMessage } from "element-plus";
import { logout } from "./utils/auth";
import { connectSocket } from "./utils/ws";
import { on } from "./utils/messageBus";
import { initNotify, resetNotify } from "./utils/notify";
export default {
  name: "App",
  setup() {
    const store = useStore();

    // 拉取最新用户信息（身份来自 Access Token，不传 uuid）
    const loadUserInfo = async () => {
      try {
        const rsp = await axios.post(store.state.apiUrl + "/user/getUserInfo", {});
        if (rsp.data.code == 0) {
          if (rsp.data.data && !rsp.data.data.avatar.startsWith("http")) {
            rsp.data.data.avatar = store.state.backendUrl + rsp.data.data.avatar;
          }
          store.commit("setUserInfo", rsp.data.data);
          return true;
        }
        return false;
      } catch (error) {
        console.log(error);
        return false;
      }
    };

    // 登录态就绪后：建立 WS 连接 + 启动全局提醒（覆盖刷新恢复和登录页登录两种路径）
    watch(
      () => store.getters.isLoggedIn,
      (loggedIn) => {
        if (loggedIn) {
          initNotify();
          if (!store.state.socket) {
            connectSocket(store);
          }
        } else {
          resetNotify();
        }
      },
      { immediate: true }
    );

    // WS 重连前刷新登录态失败：Refresh Cookie 已失效，回登录页
    on("auth:expired", async () => {
      ElMessage.error("登录已过期，请重新登录");
      await logout({ silent: true });
      if (store.getters.isLoggedIn) {
        store.commit("clearAccessToken");
        store.commit("cleanUserInfo");
      }
    });

    onMounted(async () => {
      // 无 Access Token（含静默续期失败）时不拉用户信息，路由守卫负责跳登录页
      if (!store.state.accessToken) {
        return;
      }
      const ok = await loadUserInfo();
      if (!ok) {
        return;
      }
      if (store.state.userInfo.status == 1) {
        // 账号被封禁：主动登出
        await logout({ silent: true });
        ElMessage.error("账号被封禁，退出登录");
        return;
      }
    });
  },
};
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box; /* 推荐使用，以确保布局计算的一致性 */
}
</style>
