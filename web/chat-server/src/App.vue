<template>
  <router-view />
</template>

<script>
import { onMounted } from "vue";
import { useStore } from "vuex";
import axios from "axios";
import { logout } from "./utils/auth";
import { connectSocket } from "./utils/ws";
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

    onMounted(async () => {
      // 无 Access Token（含静默续期失败）时不建立连接，路由守卫负责跳登录页
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
      connectSocket(store);
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
