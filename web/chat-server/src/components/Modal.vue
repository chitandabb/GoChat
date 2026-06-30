<template>

  <div class="modal-overlay" v-if="isVisible">
    <div class="modal-content">
      <slot name="header">
      </slot>
      <slot name="body">
      </slot>
      <slot name="footer">
      </slot>
    </div>
  </div>
</template>

<script>
export default {
  name: "Modal",
  props: {
    isVisible: {
      type: Boolean,
      default: false,
    },
  },
};
</script>

<style scoped>
.modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  background: rgba(17, 23, 20, 0.24);
  backdrop-filter: blur(8px);
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 2000;
}

.modal-content {
  width: min(500px, calc(100vw - 32px));
  max-height: calc(100vh - 40px);
  min-height: 300px;
  overflow: hidden;
  border-radius: 18px;
  border: 1px solid var(--go-border);
  background: rgba(255, 255, 255, 0.98);
  box-shadow: var(--go-shadow);
  display: flex;
  flex-direction: column;
  align-items: center;
  animation: modal-rise 0.22s ease-out;
}

@keyframes modal-rise {
  from {
    opacity: 0;
    transform: translateY(10px) scale(0.98);
  }

  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}
</style>
