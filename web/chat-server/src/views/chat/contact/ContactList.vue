<template>
  <div class="chat-page-shell">
    <el-header class="chat-pane-header">
      <div class="chat-pane-header-copy">
        <h2 class="chat-name">通讯录</h2>
      </div>
      <div class="chat-pane-meta">管理联系人与群聊</div>
    </el-header>
    <el-main class="main-container">
      <div class="chat-screen panel-showcase">
        <div class="empty-chat-state__simple">
          <h3>从左侧查看联系人和群聊</h3>
          <p>添加好友、创建群聊和处理申请入口都保留在左边，右侧不再堆无意义的输入区。</p>
        </div>
      </div>
    </el-main>
  </div>
</template>

<script>
export default {
  name: "ContactList",
};
</script>

<style scoped>
.chat-page-shell {
  height: 100%;
  display: flex;
  flex-direction: column;
}
</style>
