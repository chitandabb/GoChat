<template>
  <AuthShell
    eyebrow="注册账号"
    title="创建你的 GoChat"
    subtitle="完成基础信息后，通过短信验证码完成注册，快速加入你的聊天与群聊。"
    :features="['快速注册', '短信验证', '即刻开始']"
  >
    <div class="auth-card auth-card--compact">
      <div class="auth-card__header">
        <span class="auth-card__eyebrow">Create Account</span>
        <h2 class="auth-card__title">注册</h2>
        <p class="auth-card__subtitle">填写基础信息后，通过短信验证码完成注册。</p>
      </div>

      <el-form
        ref="formRef"
        :model="registerData"
        label-position="top"
        class="auth-form auth-form--wechat"
      >
        <el-form-item
          prop="nickname"
          label="昵称"
          :rules="[
            {
              required: true,
              message: '此项为必填项',
              trigger: 'blur',
            },
            {
              min: 3,
              max: 10,
              message: '昵称长度在 3 到 10 个字符',
              trigger: 'blur',
            },
          ]"
        >
          <el-input v-model="registerData.nickname" placeholder="请输入昵称" />
        </el-form-item>

        <el-form-item
          prop="telephone"
          label="账号"
          :rules="[
            {
              required: true,
              message: '此项为必填项',
              trigger: 'blur',
            },
          ]"
        >
          <el-input
            v-model="registerData.telephone"
            placeholder="请输入手机号"
          />
        </el-form-item>

        <el-form-item
          prop="password"
          label="密码"
          :rules="[
            {
              required: true,
              message: '此项为必填项',
              trigger: 'blur',
            },
          ]"
        >
          <el-input
            v-model="registerData.password"
            type="password"
            placeholder="请设置登录密码"
            show-password
          />
        </el-form-item>

        <el-form-item
          prop="sms_code"
          label="验证码"
          :rules="[
            {
              required: true,
              message: '此项为必填项',
              trigger: 'blur',
            },
          ]"
        >
          <el-input v-model="registerData.sms_code" class="auth-code-input">
            <template #append>
              <el-button
                class="soft-action-btn code-send-btn"
                :class="{ 'is-loading': smsCountdown > 0 }"
                @click="sendSmsCode"
              >
                {{ smsCountdown > 0 ? `${smsCountdown}s` : "点击发送" }}
              </el-button>
            </template>
          </el-input>
        </el-form-item>
      </el-form>

      <el-button
        type="primary"
        class="auth-submit-btn auth-submit-btn--lift"
        @click="handleRegister"
      >
        注册
      </el-button>

      <div class="auth-link-row auth-link-row--center">
        <button class="auth-inline-link" @click="handleSmsLogin">
          验证码登录
        </button>
        <button class="auth-inline-link" @click="handleLogin">密码登录</button>
      </div>
    </div>
  </AuthShell>
</template>

<script>
import { reactive, ref, toRefs, onBeforeUnmount } from "vue";
import axios from "axios";
import { useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { useStore } from "vuex";
import AuthShell from "@/components/AuthShell.vue";

export default {
  name: "Register",
  components: {
    AuthShell,
  },
  setup() {
    const data = reactive({
      registerData: {
        telephone: "",
        password: "",
        nickname: "",
        sms_code: "",
      },
    });
    const router = useRouter();
    const store = useStore();
    const smsCountdown = ref(0);
    let smsTimer = null;

    const handleRegister = async () => {
      try {
        if (
          !data.registerData.nickname ||
          !data.registerData.telephone ||
          !data.registerData.password ||
          !data.registerData.sms_code
        ) {
          ElMessage.error("请填写完整注册信息。");
          return;
        }
        if (
          data.registerData.nickname.length < 3 ||
          data.registerData.nickname.length > 10
        ) {
          ElMessage.error("昵称长度在 3 到 10 个字符。");
          return;
        }
        if (!checkTelephoneValid()) {
          ElMessage.error("请输入有效的手机号码。");
          return;
        }
        const response = await axios.post(
          store.state.backendUrl + "/register",
          data.registerData
        );
        if (response.data.code == 200) {
          ElMessage.success(response.data.message);
          console.log(response.data.message);
          if (!response.data.data.avatar.startsWith("http")) {
            response.data.data.avatar =
              store.state.backendUrl + response.data.data.avatar;
          }
          store.commit("setUserInfo", response.data.data);
          const wsUrl =
            store.state.wsUrl + "/wss?client_id=" + response.data.data.uuid;
          console.log(wsUrl);
          store.state.socket = new WebSocket(wsUrl);
          store.state.socket.onopen = () => {
            console.log("WebSocket连接已打开");
          };
          store.state.socket.onmessage = (message) => {
            console.log("收到消息：", message.data);
          };
          store.state.socket.onclose = () => {
            console.log("WebSocket连接已关闭");
          };
          store.state.socket.onerror = () => {
            console.log("WebSocket连接发生错误");
          };
          router.push("/chat/sessionlist");
        } else {
          ElMessage.error(response.data.message);
          console.log(response.data.message);
        }
      } catch (error) {
        ElMessage.error(error);
        console.log(error);
      }
    };

    const checkTelephoneValid = () => {
      const regex = /^1[3456789]\d{9}$/;
      return regex.test(data.registerData.telephone);
    };

    const handleLogin = () => {
      router.push("/login");
    };

    const handleSmsLogin = () => {
      router.push("/smsLogin");
    };

    const clearTimer = () => {
      if (smsTimer) {
        clearInterval(smsTimer);
        smsTimer = null;
      }
    };

    const startCountdown = () => {
      smsCountdown.value = 60;
      clearTimer();
      smsTimer = setInterval(() => {
        smsCountdown.value -= 1;
        if (smsCountdown.value <= 0) {
          smsCountdown.value = 0;
          clearTimer();
        }
      }, 1000);
    };

    const sendSmsCode = async () => {
      if (smsCountdown.value > 0) {
        return;
      }
      if (
        !data.registerData.telephone ||
        !data.registerData.nickname ||
        !data.registerData.password
      ) {
        ElMessage.error("请填写完整注册信息。");
        return;
      }
      if (!checkTelephoneValid()) {
        ElMessage.error("请输入有效的手机号码。");
        return;
      }
      const req = {
        telephone: data.registerData.telephone,
      };
      const rsp = await axios.post(
        store.state.backendUrl + "/user/sendSmsCode",
        req
      );
      console.log(rsp);
      if (rsp.data.code == 200) {
        ElMessage.success(rsp.data.message);
        startCountdown();
      } else if (rsp.data.code == 400) {
        ElMessage.warning(rsp.data.message);
      } else {
        ElMessage.error(rsp.data.message);
      }
    };

    onBeforeUnmount(() => {
      clearTimer();
    });

    return {
      ...toRefs(data),
      router,
      handleRegister,
      handleLogin,
      handleSmsLogin,
      sendSmsCode,
      smsCountdown,
    };
  },
};
</script>

<style scoped lang="scss">
.auth-card--compact {
  width: 100%;
}

.auth-form--wechat {
  :deep(.el-form-item) {
    margin-bottom: 22px;
  }

  :deep(.el-form-item__label) {
    padding-bottom: 8px;
    color: #111111;
    font-size: 13px;
    font-weight: 600;
  }

  :deep(.el-input__wrapper) {
    padding: 0 0 12px;
    border-radius: 0;
    background: transparent;
    box-shadow: inset 0 -1px 0 #dfe4e1;
  }

  :deep(.el-input__wrapper:hover) {
    box-shadow: inset 0 -1px 0 #c6d0cb;
  }

  :deep(.el-input__wrapper.is-focus) {
    box-shadow: inset 0 -2px 0 #07c160;
  }

  :deep(.el-input__inner) {
    height: 42px;
    color: #111111;
    font-size: 15px;
  }

  :deep(.el-input__inner::placeholder) {
    color: #a0a8a3;
  }

  :deep(.el-input-group__append) {
    padding: 0 0 0 10px;
    border: none;
    background: transparent;
    box-shadow: none;
  }
}

.auth-code-input {
  width: 100%;
}

.code-send-btn {
  min-width: 104px;
}

.code-send-btn.is-loading {
  opacity: 0.7;
}

.auth-link-row--center {
  margin-top: 20px;
  display: flex;
  justify-content: center;
  gap: 18px;
}

.auth-inline-link {
  padding: 0;
  border: none;
  background: transparent;
  color: #6b7470;
  cursor: pointer;
  font-size: 14px;
  transition: color 0.2s ease;
}

.auth-inline-link:hover {
  color: #07c160;
}
</style>
