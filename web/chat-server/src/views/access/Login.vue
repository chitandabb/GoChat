<template>
  <AuthShell
    eyebrow="密码登录"
    title="欢迎回到 GoChat"
    subtitle="像微信一样自然地开始聊天，连接朋友、群聊和每一次正在发生的对话。"
    :features="['会话同步', '联系人管理', '轻盈桌面体验']"
  >
    <div class="auth-card auth-card--compact">
      <div class="auth-card__header">
        <span class="auth-card__eyebrow">Welcome back</span>
        <h2 class="auth-card__title">欢迎回来</h2>
        <p class="auth-card__subtitle">请输入您的账号和密码</p>
      </div>

      <el-form :model="loginData" label-position="top" class="auth-form auth-form--wechat">
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
            v-model="loginData.telephone"
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
            v-model="loginData.password"
            type="password"
            placeholder="请输入密码"
            show-password
          />
        </el-form-item>
      </el-form>

      <el-button
        type="primary"
        class="auth-submit-btn auth-submit-btn--lift"
        :loading="loginLoading"
        @click="handleLogin"
      >
        登录
      </el-button>

      <div class="auth-actions auth-actions--end">
        <button class="auth-inline-link" @click="handleSmsLogin">验证码登录</button>
      </div>

      <div class="auth-footer">
        <span>还没有账号？</span>
        <button class="auth-inline-link auth-inline-link--strong" @click="handleRegister">
          立即注册
        </button>
      </div>
    </div>
  </AuthShell>
</template>

<script setup>
import { reactive, ref } from "vue";
import axios from "axios";
import { useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { useStore } from "vuex";
import AuthShell from "@/components/AuthShell.vue";
import { connectSocket } from "@/utils/ws";
import { errorMsg } from "@/utils/error";

const loginData = reactive({
  telephone: "",
  password: "",
});

const router = useRouter();
const store = useStore();
const loginLoading = ref(false);

const handleLogin = async () => {
  if (loginLoading.value) {
    return;
  }
  try {
    if (!loginData.telephone || !loginData.password) {
      ElMessage.error("请填写完整登录信息。");
      return;
    }
    if (!checkTelephoneValid()) {
      ElMessage.error("请输入有效的手机号码。");
      return;
    }
    loginLoading.value = true;
    console.log(store.state.backendUrl, store.state.wsUrl);
    const response = await axios.post(
      store.state.apiUrl + "/auth/login",
      loginData
    );
    console.log(response);
    if (response.data.code == 0) {
      const data = response.data.data;
      if (data.user_info.status == 1) {
        ElMessage.error("该账号已被封禁，请联系管理员。");
        return;
      }
      try {
        ElMessage.success(response.data.message);
        store.commit("setAccessToken", data.access_token);
        const userInfo = data.user_info;
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
    ElMessage.error(errorMsg(error, "登录失败，请稍后重试"));
  } finally {
    loginLoading.value = false;
  }
};

const checkTelephoneValid = () => {
  const regex = /^1[3456789]\d{9}$/;
  return regex.test(loginData.telephone);
};

const handleRegister = () => {
  router.push("/register");
};

const handleSmsLogin = () => {
  router.push("/smsLogin");
};
</script>

<style lang="scss" scoped>
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
    line-height: 1.4;
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

  :deep(.el-input__suffix-inner) {
    color: #96a19b;
  }
}

.auth-actions {
  display: flex;
  margin-top: 16px;
}

.auth-actions--end {
  justify-content: flex-end;
}

.auth-footer {
  margin-top: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: #8a938d;
  font-size: 14px;
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

.auth-inline-link--strong {
  color: #07c160;
  font-weight: 600;
}

.auth-inline-link--strong:hover {
  color: #06ad56;
}
</style>
