<template>
  <div class="sessionlist-container">
    <div class="sessionlist-header">
      <el-input
        v-model="contactSearch"
        class="contact-search-input"
        placeholder="搜索会话"
        size="small"
        suffix-icon="Search"
        clearable
      />
    </div>
    <div class="contactlist-body">
      <div class="contactlist-user">
        <el-menu
          router
          :default-openeds="['users', 'groups']"
        >
          <el-sub-menu index="users">
            <template #title>
              <div class="sessionlist-title-row">
                <span class="sessionlist-title">用户</span>
                <span class="sessionlist-count">
                  {{ filteredUserSessionList.length }}
                </span>
              </div>
            </template>
            <el-menu-item
              v-for="user in filteredUserSessionList"
              :key="user.user_id"
              :class="{ 'is-active': store.state.currentChatId === user.user_id }"
              @click="handleToChatUser(user)"
            >
              <div class="session-item">
                <el-badge
                  :value="unreadOf(user.user_id)"
                  :hidden="unreadOf(user.user_id) <= 0"
                  :max="99"
                  class="session-unread"
                >
                  <img :src="user.avatar" class="sessionlist-avatar" />
                </el-badge>
                <div class="session-item-main">
                  <div class="session-item-name">
                    {{ user.user_name }}
                    <span v-if="user.shadow" class="session-item-tag">新</span>
                  </div>
                  <div class="session-item-preview">
                    {{ user.lastMessage || "暂无消息" }}
                  </div>
                </div>
              </div>
            </el-menu-item>
            <el-menu-item
              v-if="!filteredUserSessionList.length"
              disabled
              class="menu-empty-item"
            >
              没有匹配的用户会话
            </el-menu-item>
          </el-sub-menu>

          <el-sub-menu index="groups">
            <template #title>
              <div class="sessionlist-title-row">
                <span class="sessionlist-title">群聊</span>
                <span class="sessionlist-count">
                  {{ filteredGroupSessionList.length }}
                </span>
              </div>
            </template>
            <el-menu-item
              v-for="group in filteredGroupSessionList"
              :key="group.group_id"
              :class="{ 'is-active': store.state.currentChatId === group.group_id }"
              @click="handleToChatGroup(group)"
            >
              <div class="session-item">
                <el-badge
                  :value="unreadOf(group.group_id)"
                  :hidden="unreadOf(group.group_id) <= 0"
                  :max="99"
                  class="session-unread"
                >
                  <img :src="group.avatar" class="sessionlist-avatar" />
                </el-badge>
                <div class="session-item-main">
                  <div class="session-item-name">{{ group.group_name }}</div>
                  <div class="session-item-preview">
                    {{ group.lastMessage || "暂无消息" }}
                  </div>
                </div>
              </div>
            </el-menu-item>
            <el-menu-item
              v-if="!filteredGroupSessionList.length"
              disabled
              class="menu-empty-item"
            >
              没有匹配的群聊会话
            </el-menu-item>
          </el-sub-menu>
        </el-menu>
      </div>
    </div>
  </div>
</template>

<script>
import { computed, onBeforeUnmount, onMounted, reactive, toRefs } from "vue";
import { useRouter } from "vue-router";
import { useStore } from "vuex";
import axios from "axios";
import { on, sessionKeyOf } from "@/utils/messageBus";

export default {
  name: "ChatSessionSidebar",
  setup() {
    const router = useRouter();
    const store = useStore();
    const data = reactive({
      contactSearch: "",
      ownListReq: {
        owner_id: "",
      },
      userSessionList: [],
      groupSessionList: [],
    });

    const unreadOf = (sessionKey) => store.state.unreadMap[sessionKey] || 0;

    const normalizeAvatarList = (list = []) =>
      list.map((item) => {
        if (item.avatar && !item.avatar.startsWith("http")) {
          return {
            ...item,
            avatar: store.state.backendUrl + item.avatar,
          };
        }
        return item;
      });

    const matchesSearch = (values) => {
      const keyword = data.contactSearch.trim().toLowerCase();
      if (!keyword) {
        return true;
      }
      return values.some((value) =>
        String(value || "")
          .toLowerCase()
          .includes(keyword)
      );
    };

    const filteredUserSessionList = computed(() =>
      data.userSessionList.filter((user) =>
        matchesSearch([user.user_name, user.user_id])
      )
    );

    const filteredGroupSessionList = computed(() =>
      data.groupSessionList.filter((group) =>
        matchesSearch([group.group_name, group.group_id])
      )
    );

    const fetchUserSessionList = async () => {
      data.ownListReq.owner_id = store.state.userInfo.uuid;
      const rsp = await axios.post(
        store.state.apiUrl + "/session/getUserSessionList",
        data.ownListReq
      );
      const fresh = normalizeAvatarList(rsp.data.data || []);
      // 重拉时保留本地维护的最近消息预览（后端会话列表无该字段）
      data.userSessionList = fresh.map((item) => {
        const old = data.userSessionList.find(
          (u) => u.user_id === item.user_id
        );
        return old && old.lastMessage ? { ...item, lastMessage: old.lastMessage } : item;
      });
    };

    const fetchGroupSessionList = async () => {
      data.ownListReq.owner_id = store.state.userInfo.uuid;
      const rsp = await axios.post(
        store.state.apiUrl + "/session/getGroupSessionList",
        data.ownListReq
      );
      const fresh = normalizeAvatarList(rsp.data.data || []);
      data.groupSessionList = fresh.map((item) => {
        const old = data.groupSessionList.find(
          (g) => g.group_id === item.group_id
        );
        return old && old.lastMessage ? { ...item, lastMessage: old.lastMessage } : item;
      });
    };

    let refreshTimer = null;
    let intervalTimer = null;
    const preloadSessionLists = async () => {
      try {
        await Promise.all([fetchUserSessionList(), fetchGroupSessionList()]);
      } catch (error) {
        console.error(error);
      }
    };
    // 多个事件连续触发时合并成一次重拉
    const scheduleRefresh = () => {
      if (refreshTimer) {
        clearTimeout(refreshTimer);
      }
      refreshTimer = setTimeout(preloadSessionLists, 300);
    };
    // 兜底自动刷新：60s 定时 + 页面重新可见时刷新，避免长期挂机漏掉新会话
    const handleVisibilityChange = () => {
      if (document.visibilityState === "visible") {
        scheduleRefresh();
      }
    };
    const startAutoRefresh = () => {
      stopAutoRefresh();
      document.addEventListener("visibilitychange", handleVisibilityChange);
      window.addEventListener("focus", handleVisibilityChange);
      intervalTimer = setInterval(() => {
        if (document.visibilityState === "visible") {
          scheduleRefresh();
        }
      }, 60 * 1000);
    };
    const stopAutoRefresh = () => {
      if (intervalTimer) {
        clearInterval(intervalTimer);
        intervalTimer = null;
      }
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      window.removeEventListener("focus", handleVisibilityChange);
    };

    // 消息预览文案
    const previewOf = (message) => {
      if (message.type === 2) {
        const fileType = message.file_type || "";
        if (fileType.startsWith("image/")) {
          return "[图片]";
        }
        if (fileType.startsWith("video/")) {
          return "[视频]";
        }
        if (fileType.startsWith("audio/")) {
          return "[语音]";
        }
        return "[文件] " + (message.file_name || "");
      }
      return message.content || "";
    };

    // 实时收消息：更新最近消息、置顶排序；单聊未知发送者补“影子会话”
    const handleChatMessage = (message) => {
      const myId = store.state.userInfo.uuid;
      if (!myId) {
        return;
      }
      const sessionKey = sessionKeyOf(message, myId);
      if (!sessionKey) {
        return;
      }
      const isMine = message.send_id === myId;
      const preview = previewOf(message);
      if (sessionKey[0] === "G") {
        const index = data.groupSessionList.findIndex(
          (g) => g.group_id === sessionKey
        );
        if (index === -1) {
          return;
        }
        const item = data.groupSessionList.splice(index, 1)[0];
        item.lastMessage = (isMine ? "" : message.send_name + "：") + preview;
        data.groupSessionList.unshift(item);
        return;
      }
      const index = data.userSessionList.findIndex(
        (u) => u.user_id === sessionKey
      );
      if (index >= 0) {
        const item = data.userSessionList.splice(index, 1)[0];
        item.lastMessage = preview;
        data.userSessionList.unshift(item);
        return;
      }
      if (isMine) {
        // 自己发出的回显，理论上会话已存在；缺失时等下一次重拉兜底
        return;
      }
      let avatar = message.send_avatar || "";
      if (avatar && !avatar.startsWith("http")) {
        avatar = store.state.backendUrl + avatar;
      }
      data.userSessionList.unshift({
        user_id: sessionKey,
        user_name: message.send_name || sessionKey,
        avatar,
        lastMessage: preview,
        // 影子会话：后端会话在进入聊天页 openSession 后才真正创建
        shadow: true,
      });
    };

    const handleToChatUser = (user) => {
      router.push("/chat/" + user.user_id);
    };

    const handleToChatGroup = (group) => {
      router.push("/chat/" + group.group_id);
    };

    const offChatMessage = on("chat-message", handleChatMessage);
    const offWsConnected = on("ws:connected", scheduleRefresh);
    const offSessionChanged = on("session-list-changed", scheduleRefresh);

    onMounted(() => {
      preloadSessionLists();
      startAutoRefresh();
    });

    onBeforeUnmount(() => {
      offChatMessage();
      offWsConnected();
      offSessionChanged();
      stopAutoRefresh();
      if (refreshTimer) {
        clearTimeout(refreshTimer);
        refreshTimer = null;
      }
    });

    return {
      ...toRefs(data),
      store,
      filteredUserSessionList,
      filteredGroupSessionList,
      unreadOf,
      handleToChatUser,
      handleToChatGroup,
      preloadSessionLists,
    };
  },
};
</script>

<style scoped>
.sessionlist-container {
  flex: 1;
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: 18px 14px 16px;
  background: transparent;
}

.sessionlist-header {
  display: flex;
  margin-bottom: 12px;
}

.contact-search-input {
  width: 100%;
}

.contact-search-input :deep(.el-input__wrapper) {
  background: rgba(255, 255, 255, 0.72);
}

.contactlist-body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  padding-right: 2px;
}

:deep(.el-menu) {
  width: 100%;
  border-right: none;
  background: transparent;
}

:deep(.el-sub-menu__title) {
  height: 42px;
  margin-bottom: 6px;
  padding-left: 12px;
  border-radius: 14px;
  color: var(--go-text);
  font-weight: 600;
}

:deep(.el-menu-item) {
  height: 58px;
  margin-bottom: 4px;
  padding: 0 10px 0 12px !important;
  border-radius: 14px;
  background: transparent;
  color: var(--go-text);
  box-shadow: none;
  transition:
    background-color 0.18s ease,
    color 0.18s ease,
    transform 0.18s ease;
}

:deep(.el-sub-menu__title:hover),
:deep(.el-menu-item:hover),
:deep(.el-menu-item.is-active) {
  background: #eaf1ec;
  color: var(--go-text-strong);
}

.sessionlist-title {
  font-size: 14px;
  font-weight: 600;
}

.sessionlist-title-row {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.sessionlist-count {
  min-width: 22px;
  height: 22px;
  padding: 0 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: rgba(7, 193, 96, 0.08);
  color: var(--go-accent-strong);
  font-size: 12px;
  font-weight: 700;
}

.session-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
}

.sessionlist-avatar {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  object-fit: cover;
  display: block;
}

.session-item-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  text-align: left;
}

.session-item-name {
  color: var(--go-text-strong);
  font-size: 14px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-item-tag {
  margin-left: 6px;
  padding: 0 5px;
  border-radius: 6px;
  background: rgba(245, 108, 108, 0.12);
  color: #f56c6c;
  font-size: 11px;
  font-weight: 700;
}

.session-item-preview {
  color: var(--go-text-muted);
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

:deep(.el-menu-item.menu-empty-item) {
  justify-content: flex-start;
  color: var(--go-text-soft);
  cursor: default;
}
</style>
