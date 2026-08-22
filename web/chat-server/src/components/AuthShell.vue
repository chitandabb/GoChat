<template>
  <div class="auth-shell">
    <section class="auth-shell__brand-panel">
      <div class="auth-shell__ambient auth-shell__ambient--one"></div>
      <div class="auth-shell__ambient auth-shell__ambient--two"></div>
      <div class="auth-shell__ambient auth-shell__ambient--three"></div>

      <div class="auth-shell__brand-content">
        <div class="auth-shell__badge">{{ eyebrow }}</div>
        <h1 class="auth-shell__headline">{{ titleText }}</h1>
        <p class="auth-shell__tagline">{{ subtitleText }}</p>

        <div
          v-if="previewItems.length"
          class="auth-shell__feature-strip"
        >
          <span
            v-for="feature in previewItems"
            :key="feature"
            class="auth-shell__feature-chip"
          >
            {{ feature }}
          </span>
        </div>
      </div>
    </section>

    <section class="auth-shell__form-panel">
      <div class="auth-shell__form-card">
        <slot />
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed } from "vue";

const props = defineProps({
  eyebrow: {
    type: String,
    default: "Secure Access",
  },
  title: {
    type: String,
    default: "",
  },
  subtitle: {
    type: String,
    default: "",
  },
  features: {
    type: Array,
    default: () => [],
  },
});

const defaultFeatures = [
  "即时消息同步",
  "轻量群聊协作",
  "熟悉的桌面微信节奏",
];

const titleText = computed(() => props.title || "GoChat");
const subtitleText = computed(
  () => props.subtitle || "连接你我，沟通无界"
);
const previewItems = computed(() =>
  props.features && props.features.length ? props.features : defaultFeatures
);
</script>

<style scoped lang="scss">
.auth-shell {
  width: 100%;
  height: 100%;
  display: flex;
  /* 窗口级滚动已全局锁定,极矮视口下登录卡片在壳内滚动兜底 */
  overflow-y: auto;
  background: linear-gradient(180deg, #ffffff 0%, #f7faf8 100%);
}

.auth-shell__brand-panel {
  position: relative;
  flex: 0 0 54%;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  padding: 48px 40px;
  background:
    radial-gradient(circle at 20% 18%, rgba(255, 255, 255, 0.18), transparent 28%),
    radial-gradient(circle at 84% 80%, rgba(255, 255, 255, 0.16), transparent 26%),
    linear-gradient(160deg, #07c160 0%, #0a9f63 52%, #0f7d58 100%);
}

.auth-shell__brand-content {
  position: relative;
  z-index: 2;
  width: min(520px, 80%);
  color: #ffffff;
  animation: auth-shell-slide-up 0.55s ease both;
}

.auth-shell__badge {
  display: inline-flex;
  align-items: center;
  min-height: 34px;
  padding: 0 14px;
  margin-bottom: 22px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.2);
  background: rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(10px);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.auth-shell__headline {
  margin: 0;
  font-size: clamp(34px, 4.4vw, 58px);
  line-height: 1.1;
  font-weight: 700;
  letter-spacing: -0.035em;
}

.auth-shell__tagline {
  max-width: 420px;
  margin: 18px 0 0;
  color: rgba(255, 255, 255, 0.82);
  font-size: 16px;
  line-height: 1.85;
}

.auth-shell__feature-strip {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-top: 28px;
}

.auth-shell__feature-chip {
  display: inline-flex;
  align-items: center;
  min-height: 36px;
  padding: 0 14px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.18);
  background: rgba(255, 255, 255, 0.12);
  backdrop-filter: blur(10px);
  color: rgba(255, 255, 255, 0.94);
  font-size: 13px;
  font-weight: 600;
}

.auth-shell__ambient {
  position: absolute;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.1);
  pointer-events: none;

  &--one {
    top: 10%;
    left: -12%;
    width: 280px;
    height: 280px;
    animation: auth-orb-float 10s ease-in-out infinite;
  }

  &--two {
    right: 8%;
    top: 16%;
    width: 120px;
    height: 120px;
    animation: auth-orb-float 8.5s ease-in-out infinite reverse;
  }

  &--three {
    right: -10%;
    bottom: -4%;
    width: 320px;
    height: 320px;
    animation: auth-orb-float 11.5s ease-in-out infinite;
  }
}

.auth-shell__form-panel {
  flex: 0 0 46%;
  display: flex;
  /* safe 居中:矮视口下卡片高于面板时顶部不会被裁掉 */
  align-items: safe center;
  justify-content: safe center;
  padding: 40px 32px;
  background: linear-gradient(180deg, #ffffff 0%, #f9fbfa 100%);
}

.auth-shell__form-card {
  width: min(420px, 100%);
  animation: auth-shell-rise 0.5s ease both;
}

@keyframes auth-shell-rise {
  from {
    opacity: 0;
    transform: translateY(16px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes auth-shell-slide-up {
  from {
    opacity: 0;
    transform: translateY(18px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes auth-orb-float {
  0%,
  100% {
    transform: translate3d(0, 0, 0);
  }

  50% {
    transform: translate3d(0, -14px, 0);
  }
}

@media (max-width: 980px) {
  .auth-shell__brand-panel {
    flex-basis: 50%;
    padding-inline: 30px;
  }

  .auth-shell__form-panel {
    flex-basis: 50%;
    padding-inline: 24px;
  }

  .auth-shell__headline {
    font-size: 36px;
  }
}

@media (max-width: 768px) {
  .auth-shell__brand-panel {
    display: none;
  }

  .auth-shell__form-panel {
    flex: 1;
    padding: 24px 20px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .auth-shell__brand-content,
  .auth-shell__form-card,
  .auth-shell__ambient {
    animation: none;
  }
}
</style>
