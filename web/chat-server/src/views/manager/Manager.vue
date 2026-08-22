<template>
  <div class="chat-wrap">
    <div
      class="chat-window"
      :style="{
        boxShadow: `var(${'--el-box-shadow-dark'})`,
      }"
    >
      <el-container class="chat-window-container">
        <el-header class="manager-header">
          <div class="manager-header__brand">
            <div class="manager-header__logo">
              <el-icon><Setting /></el-icon>
            </div>
            <div>
              <div class="manager-header__title">GoChat 管理后台</div>
              <div class="manager-header__sub">ADMIN CONSOLE</div>
            </div>
          </div>
          <div class="manager-header__actions">
            <div class="manager-header__admin">
              <el-avatar :size="30" :src="adminAvatar" />
              <span>{{ adminName }}</span>
            </div>
            <el-button class="soft-action-btn" @click="backToChat">
              <el-icon class="manager-btn-icon"><ChatDotRound /></el-icon>返回聊天
            </el-button>
          </div>
        </el-header>

        <el-container>
          <el-aside class="manager-aside">
            <div class="manager-aside__label">管理中心</div>
            <el-menu
              class="manager-menu"
              :default-active="active"
              :default-openeds="['user', 'group']"
              @select="onMenuSelect"
            >
              <el-sub-menu index="user">
                <template #title>
                  <el-icon><User /></el-icon>
                  <span>用户</span>
                </template>
                <el-menu-item index="user-disable">启用 / 禁用</el-menu-item>
                <el-menu-item index="user-delete">删除用户</el-menu-item>
                <el-menu-item index="user-admin">设置管理员</el-menu-item>
              </el-sub-menu>
              <el-sub-menu index="group">
                <template #title>
                  <el-icon><ChatDotRound /></el-icon>
                  <span>群聊</span>
                </template>
                <el-menu-item index="group-disable">启用 / 禁用</el-menu-item>
                <el-menu-item index="group-delete">删除 / 解散群聊</el-menu-item>
              </el-sub-menu>
            </el-menu>
            <div class="manager-aside__foot">
              管理操作即时生效,删除 / 解散不可恢复,请谨慎操作。
            </div>
          </el-aside>

          <el-main class="manager-main">
            <div v-if="!section" class="manager-empty">
              <div class="manager-empty__icon">
                <el-icon><Setting /></el-icon>
              </div>
              <h3>管理中心</h3>
              <p>
                从左侧选择要管理的对象:用户的启用 / 禁用、删除与管理员设置,
                群聊的启用 / 禁用与解散。
              </p>
            </div>
            <template v-else>
              <div class="manager-section-head">
                <div>
                  <div class="manager-section-head__title">{{ section.title }}</div>
                  <div class="manager-section-head__desc">{{ section.desc }}</div>
                </div>
              </div>
              <div class="manager-card">
                <DisableUserModal
                  v-if="active === 'user-disable'"
                  :isVisible="true"
                ></DisableUserModal>
                <DeleteUserModal
                  v-if="active === 'user-delete'"
                  :isVisible="true"
                ></DeleteUserModal>
                <SetAdminModal
                  v-if="active === 'user-admin'"
                  :isVisible="true"
                ></SetAdminModal>
                <DisableGroupModal
                  v-if="active === 'group-disable'"
                  :isVisible="true"
                ></DisableGroupModal>
                <DeleteGroupModal
                  v-if="active === 'group-delete'"
                  :isVisible="true"
                ></DeleteGroupModal>
              </div>
            </template>
          </el-main>
        </el-container>
      </el-container>
    </div>
  </div>
</template>

<script>
import { reactive, toRefs, computed } from "vue";
import { useStore } from "vuex";
import { useRouter } from "vue-router";
import DisableUserModal from "@/components/DisableUserModal.vue";
import DeleteUserModal from "@/components/DeleteUserModal.vue";
import SetAdminModal from "@/components/SetAdminModal.vue";
import DeleteGroupModal from "@/components/DeleteGroupModal.vue";
import DisableGroupModal from "@/components/DisableGroupModal.vue";

export default {
  name: "Manager",
  components: {
    DisableUserModal,
    DeleteUserModal,
    SetAdminModal,
    DeleteGroupModal,
    DisableGroupModal,
  },
  setup() {
    const router = useRouter();
    const store = useStore();
    const data = reactive({
      active: "",
    });

    const sections = {
      "user-disable": {
        title: "用户 · 启用 / 禁用",
        desc: "禁用的账号立即无法登录与收发消息,可随时恢复",
      },
      "user-delete": {
        title: "用户 · 删除",
        desc: "删除账号并解除其好友 / 群聊关系,操作不可恢复",
      },
      "user-admin": {
        title: "用户 · 管理员设置",
        desc: "授予或收回管理员权限,管理员可进入本管理后台",
      },
      "group-disable": {
        title: "群聊 · 启用 / 禁用",
        desc: "禁用的群聊停止消息收发,群与成员关系保留",
      },
      "group-delete": {
        title: "群聊 · 删除 / 解散",
        desc: "解散群聊并移除全部成员关系,操作不可恢复",
      },
    };

    const section = computed(() => sections[data.active] || null);
    const onMenuSelect = (index) => {
      data.active = index;
    };

    const adminName = computed(
      () => store.state.userInfo?.nickname || "管理员"
    );
    const adminAvatar = computed(() => {
      const avatar = store.state.userInfo?.avatar || "";
      return avatar && !avatar.startsWith("http")
        ? store.state.backendUrl + avatar
        : avatar;
    });

    const backToChat = () => {
      router.push("/chat/sessionlist");
    };

    return {
      ...toRefs(data),
      section,
      onMenuSelect,
      adminName,
      adminAvatar,
      backToChat,
    };
  },
};
</script>
