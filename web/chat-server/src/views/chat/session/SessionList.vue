<template>
  <div class="chat-page-shell">
    <el-header class="chat-pane-header">
      <div class="chat-pane-header-copy">
        <h2 class="chat-name">消息</h2>
      </div>
      <div class="chat-pane-meta">未选择会话</div>
    </el-header>
    <el-main class="main-container">
      <div class="chat-screen empty-chat-state">
        <div class="empty-chat-state__simple">
          <h3>选择一个会话开始聊天</h3>
          <p>左侧会话列表已经预加载，切换时只更新右侧内容，不再整页闪一下。</p>
        </div>
      </div>
    </el-main>
  </div>
</template>

<script>
export default {
  name: "SessionList",
};
</script>

<style scoped>
.chat-page-shell {
  height: 100%;
  display: flex;
  flex-direction: column;
}
</style>
