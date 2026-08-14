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
              @click="handleToChatUser(user)"
            >
              <img :src="user.avatar" class="sessionlist-avatar" />
              {{ user.user_name }}
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
              @click="handleToChatGroup(group)"
            >
              <img :src="group.avatar" class="sessionlist-avatar" />
              {{ group.group_name }}
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
import { computed, onMounted, reactive, toRefs } from "vue";
import { useRouter } from "vue-router";
import { useStore } from "vuex";
import axios from "axios";

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
      data.userSessionList = normalizeAvatarList(rsp.data.data || []);
    };

    const fetchGroupSessionList = async () => {
      data.ownListReq.owner_id = store.state.userInfo.uuid;
      const rsp = await axios.post(
        store.state.apiUrl + "/session/getGroupSessionList",
        data.ownListReq
      );
      data.groupSessionList = normalizeAvatarList(rsp.data.data || []);
    };

    const preloadSessionLists = async () => {
      try {
        await Promise.all([fetchUserSessionList(), fetchGroupSessionList()]);
      } catch (error) {
        console.error(error);
      }
    };

    const handleToChatUser = (user) => {
      router.push("/chat/" + user.user_id);
    };

    const handleToChatGroup = (group) => {
      router.push("/chat/" + group.group_id);
    };

    onMounted(() => {
      preloadSessionLists();
    });

    return {
      ...toRefs(data),
      filteredUserSessionList,
      filteredGroupSessionList,
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
  height: 44px;
  margin-bottom: 4px;
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

.sessionlist-avatar {
  width: 32px;
  height: 32px;
  margin-right: 12px;
  border-radius: 10px;
  object-fit: cover;
}

:deep(.el-menu-item.menu-empty-item) {
  justify-content: flex-start;
  color: var(--go-text-soft);
  cursor: default;
}
</style>
