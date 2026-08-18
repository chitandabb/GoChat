<template>
  <div class="navigation-bar">
    <div class="up-bar">
      <button class="avatar-btn" @click="handleToOwnInfo">
        <el-avatar :src="userInfo.avatar" />
      </button>
    </div>
    <div class="middle-bar">
      <el-tooltip
        effect="customized"
        content="会话聊天"
        placement="left"
        hide-after="0"
        enterable="false"
      >
        <button
          class="icon-btn"
          :class="{ 'is-active': isRouteActive('/chat/sessionlist') || isRouteActive('/chat/') }"
          @click="handleToSessionList"
        >
          <el-icon>
            <ChatRound />
          </el-icon>
        </button>
      </el-tooltip>
      <el-tooltip
        effect="customized"
        content="通讯录管理"
        placement="left"
        hide-after="0"
        enterable="false"
      >
        <button
          class="icon-btn"
          :class="{ 'is-active': isRouteActive('/chat/contactlist') }"
          @click="handleToContactList"
        >
          <el-badge
            :value="newContactCount"
            :hidden="newContactCount <= 0"
            :max="99"
          >
            <el-icon>
              <User />
            </el-icon>
          </el-badge>
        </button>
      </el-tooltip>
    </div>
    <div class="down-bar">
      <el-tooltip
        effect="customized"
        content="设置"
        placement="left"
        hide-after="0"
        enterable="false"
      >
        <el-dropdown trigger="click" placement="right">
          <button class="icon-btn">
            <el-icon>
              <Setting />
            </el-icon>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item
                v-if="userInfo.is_admin == 1"
                @click="handleToManager"
              >
                管理员模式
              </el-dropdown-item>
              <el-dropdown-item @click="logout">退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </el-tooltip>
      <el-tooltip
        effect="customized"
        content="我的主页"
        placement="left"
        hide-after="0"
        enterable="false"
      >
        <button
          class="icon-btn"
          :class="{ 'is-active': isRouteActive('/chat/owninfo') }"
          @click="handleToOwnInfo"
        >
          <el-icon><HomeFilled /></el-icon>
        </button>
      </el-tooltip>
    </div>
  </div>
</template>

<script>
import { useRoute, useRouter } from "vue-router";
import { useStore } from "vuex";
import { computed, reactive, toRefs } from "vue";
import { logout as logoutUtil } from "@/utils/auth";
export default {
  name: "NavigationModal",
  setup() {
    const router = useRouter();
    const route = useRoute();
    const store = useStore();
    const data = reactive({
      userInfo: store.state.userInfo,
    });
    const newContactCount = computed(() => store.state.newContactCount);
    const isRouteActive = (pathPrefix) => {
      if (pathPrefix === "/chat/") {
        return route.path.startsWith("/chat/") && route.path !== "/chat/contactlist" && route.path !== "/chat/owninfo";
      }
      return route.path.startsWith(pathPrefix);
    };

    const handleToContactList = () => {
      router.push("/chat/contactlist");
    };

    const handleToSessionList = () => {
      router.push("/chat/sessionlist");
    };

    const handleToManager = () => {
      router.push("/manager");
    };
    // 完整登出：撤销 Refresh Token、断开 WS、清理本地登录态
    const logout = () => {
      logoutUtil({ silent: false });
    };
    const handleToOwnInfo = () => {
      router.push("/chat/owninfo");
    };
    return {
      ...toRefs(data),
      router,
      newContactCount,
      handleToContactList,
      handleToSessionList,
      handleToOwnInfo,
      logout,
      handleToManager,
      isRouteActive,
    };
  },
};
</script>
