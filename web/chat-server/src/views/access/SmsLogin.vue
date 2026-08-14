<template>
  <AuthShell
    eyebrow="验证码登录"
    title="短信快捷登录"
    subtitle="输入手机号与验证码，快速进入 GoChat，保持像微信一样熟悉的登录节奏。"
    :features="['短信验证', '快速进入', '无负担登录']"
  >
    <div class="auth-card auth-card--compact">
      <div class="auth-card__header">
        <span class="auth-card__eyebrow">SMS Login</span>
        <h2 class="auth-card__title">验证码登录</h2>
        <p class="auth-card__subtitle">输入手机号并获取验证码后登录。</p>
      </div>

      <el-form
        ref="formRef"
        :model="loginData"
        label-position="top"
        class="auth-form auth-form--wechat"
      >
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
          <el-input v-model="loginData.telephone" placeholder="请输入手机号" />
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
          <el-input v-model="loginData.sms_code" class="auth-code-input">
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
        @click="handleSmsLogin"
      >
        登录
      </el-button>

      <div class="auth-link-row auth-link-row--center">
        <button class="auth-inline-link" @click="handleLogin">密码登录</button>
        <button class="auth-inline-link" @click="handleRegister">注册账号</button>
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
import { connectSocket } from "@/utils/ws";

export default {
  name: "smsLogin",
  components: {
    AuthShell,
  },
  setup() {
    const data = reactive({
      loginData: {
        telephone: "",
        sms_code: "",
      },
    });
    const router = useRouter();
    const store = useStore();
    const smsCountdown = ref(0);
    let smsTimer = null;

    const handleSmsLogin = async () => {
      try {
        if (!data.loginData.telephone || !data.loginData.sms_code) {
          ElMessage.error("请填写完整登录信息。");
          return;
        }
        if (!checkTelephoneValid()) {
          ElMessage.error("请输入有效的手机号码。");
          return;
        }
        const response = await axios.post(
          store.state.apiUrl + "/auth/smsLogin",
          data.loginData
        );
        console.log(response);
        if (response.data.code == 0) {
          const loginData2 = response.data.data;
          if (loginData2.user_info.status == 1) {
            ElMessage.error("该账号已被封禁，请联系管理员。");
            return;
          }
          try {
            ElMessage.success(response.data.message);
            store.commit("setAccessToken", loginData2.access_token);
            const userInfo = loginData2.user_info;
            if (!userInfo.avatar.startsWith("http")) {
              userInfo.avatar = store.state.backendUrl + userInfo.avatar;
            }
            store.commit("setUserInfo", userInfo);
            connectSocket(store);
            router.push("/chat/sessionlist");
          } catch (error) {
            console.log(error);
          }
        } else {
          ElMessage.error(response.data.message);
        }
      } catch (error) {
        ElMessage.error(error);
      }
    };

    const checkTelephoneValid = () => {
      const regex = /^1[3456789]\d{9}$/;
      return regex.test(data.loginData.telephone);
    };

    const handleRegister = () => {
      router.push("/register");
    };

    const handleLogin = () => {
      router.push("/login");
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
      if (!data.loginData.telephone) {
        ElMessage.error("请输入手机号码。");
        return;
      }
      if (!checkTelephoneValid()) {
        ElMessage.error("请输入有效的手机号码。");
        return;
      }
      try {
        const req = {
          telephone: data.loginData.telephone,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/auth/sendSmsCode",
          req
        );
        console.log(rsp);
        if (rsp.data.code == 0) {
          ElMessage.success(rsp.data.message);
          startCountdown();
        } else if (rsp.data.code == 40000) {
          ElMessage.warning(rsp.data.message);
        } else {
          ElMessage.error(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };

    onBeforeUnmount(() => {
      clearTimer();
    });

    return {
      ...toRefs(data),
      router,
      handleSmsLogin,
      handleLogin,
      handleRegister,
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
