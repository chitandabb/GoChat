<template>
  <div class="chat-wrap">
    <div
      class="chat-window"
      :style="{
        boxShadow: `var(${'--el-box-shadow-dark'})`,
      }"
    >
      <el-container class="chat-window-container">
        <el-aside class="aside-container">
          <NavigationModal />
          <component :is="sidebarComponent" />
        </el-aside>

        <el-container class="chat-container">
          <router-view v-slot="{ Component }">
            <keep-alive :include="cachedChatViews">
              <component :is="Component" />
            </keep-alive>
          </router-view>
        </el-container>
      </el-container>
    </div>
    <!-- 全局来电组件：任何聊天页面都能收到/发起通话 -->
    <CallOverlay />
  </div>
</template>

<script>
import { computed } from "vue";
import { useRoute } from "vue-router";
import NavigationModal from "@/components/NavigationModal.vue";
import ContactListModal from "@/components/ContactListModal.vue";
import ChatSessionSidebar from "@/components/ChatSessionSidebar.vue";
import CallOverlay from "@/components/CallOverlay.vue";

export default {
  name: "ChatLayout",
  components: {
    NavigationModal,
    ContactListModal,
    ChatSessionSidebar,
    CallOverlay,
  },
  setup() {
    const route = useRoute();

    const cachedChatViews = ["SessionList", "ContactList", "OwnInfo"];

    const sidebarComponent = computed(() =>
      route.name === "ContactList" || route.name === "OwnInfo"
        ? ContactListModal
        : ChatSessionSidebar
    );

    return {
      cachedChatViews,
      sidebarComponent,
    };
  },
};
</script>
