<template>
  <div class="chat-page-shell">
    <el-header class="chat-pane-header">
      <div class="chat-pane-header-copy">
        <h2 class="chat-name">我的主页</h2>
      </div>
      <div class="chat-pane-meta">查看和编辑个人资料</div>
    </el-header>
    <el-main class="main-container">
      <div class="owner-info-window">
        <div class="profile-header">
          <div class="profile-header-main">
            <el-avatar :src="userInfo.avatar" :size="68" class="profile-avatar" />
            <div class="profile-header-copy">
              <h2>{{ userInfo.nickname || "未设置昵称" }}</h2>
              <p>{{ userInfo.signature || "这个人很酷，还没有留下签名。" }}</p>
            </div>
          </div>
          <el-button class="edit-btn profile-edit-btn" @click="showMyInfoModal">
            编辑
          </el-button>
        </div>

        <div class="profile-card">
          <div class="profile-row">
            <span class="profile-label">用户 ID</span>
            <span class="profile-value">{{ userInfo.uuid || "--" }}</span>
          </div>
          <div class="profile-row">
            <span class="profile-label">电话</span>
            <span class="profile-value">{{ userInfo.telephone || "未填写" }}</span>
          </div>
          <div class="profile-row">
            <span class="profile-label">邮箱</span>
            <span class="profile-value">{{ userInfo.email || "未填写" }}</span>
          </div>
          <div class="profile-row">
            <span class="profile-label">性别</span>
            <span class="profile-value">{{ userInfo.gender === 0 ? "男" : "女" }}</span>
          </div>
          <div class="profile-row">
            <span class="profile-label">生日</span>
            <span class="profile-value">{{ userInfo.birthday || "未填写" }}</span>
          </div>
          <div class="profile-row">
            <span class="profile-label">加入时间</span>
            <span class="profile-value">{{ userInfo.created_at || "--" }}</span>
          </div>
          <div class="profile-row profile-row--top">
            <span class="profile-label">头像地址</span>
            <div class="profile-avatar-row">
              <el-avatar :src="userInfo.avatar" :size="40" />
              <span class="profile-value">{{ userInfo.avatar || "暂无头像" }}</span>
            </div>
          </div>
        </div>
      </div>
    </el-main>

    <Modal :isVisible="isMyInfoModalVisible">
      <template v-slot:header>
        <div class="modal-header">
          <div class="modal-quit-btn-container">
            <button class="modal-quit-btn" @click="quitMyInfoModal">
              <el-icon><Close /></el-icon>
            </button>
          </div>
          <div class="modal-header-title">
            <h3>修改主页</h3>
          </div>
        </div>
      </template>
      <template v-slot:body>
        <el-scrollbar
          max-height="300px"
          style="
            width: 400px;
            display: flex;
            align-items: center;
            justify-content: center;
            margin-top: 20px;
          "
        >
          <div class="modal-body">
            <el-form ref="formRef" :model="updateInfo" label-width="70px">
              <el-form-item
                prop="nickname"
                label="昵称"
                :rules="[
                  {
                    min: 3,
                    max: 10,
                    message: '昵称长度在 3 到 10 个字符',
                    trigger: 'blur',
                  },
                ]"
              >
                <el-input v-model="updateInfo.nickname" placeholder="选填" />
              </el-form-item>
              <el-form-item prop="email" label="邮箱">
                <el-input v-model="updateInfo.email" placeholder="选填" />
              </el-form-item>
              <el-form-item prop="birthday" label="生日">
                <el-input
                  v-model="updateInfo.birthday"
                  placeholder="选填，格式为2024.1.1"
                />
              </el-form-item>
              <el-form-item prop="signature" label="个性签名">
                <el-input v-model="updateInfo.signature" placeholder="选填" />
              </el-form-item>
              <el-form-item prop="avatar" label="头像">
                <el-upload
                  v-model:file-list="fileList"
                  ref="uploadRef"
                  :auto-upload="false"
                  :action="uploadPath"
                  :on-success="handleUploadSuccess"
                  :before-upload="beforeFileUpload"
                >
                  <template #trigger>
                    <el-button class="soft-action-btn">上传图片</el-button>
                  </template>
                </el-upload>
              </el-form-item>
            </el-form>
          </div>
        </el-scrollbar>
      </template>
      <template v-slot:footer>
        <div class="modal-footer">
          <el-button class="modal-close-btn" @click="closeMyInfoModal">
            完成
          </el-button>
        </div>
      </template>
    </Modal>
  </div>
</template>

<script>
import { reactive, toRefs } from "vue";
import { useStore } from "vuex";
import axios from "axios";
import Modal from "@/components/Modal.vue";
import { checkEmailValid } from "@/assets/js/valid.js";
import { ElMessage } from "element-plus";

export default {
  name: "OwnInfo",
  components: {
    Modal,
  },
  setup() {
    const store = useStore();
    const data = reactive({
      userInfo: store.state.userInfo,
      updateInfo: {
        uuid: "",
        nickname: "",
        email: "",
        birthday: "",
        signature: "",
        avatar: "",
      },
      isMyInfoModalVisible: false,
      uploadRef: null,
      uploadPath: store.state.apiUrl + "/message/uploadAvatar",
      fileList: [],
      cnt: 0,
    });

    const showMyInfoModal = () => {
      data.isMyInfoModalVisible = true;
    };

    const closeMyInfoModal = async () => {
      if (
        data.updateInfo.nickname == "" &&
        data.fileList.length == 0 &&
        data.updateInfo.email == "" &&
        data.updateInfo.birthday == "" &&
        data.updateInfo.signature == ""
      ) {
        ElMessage("请至少修改一项");
        return;
      }
      if (
        data.updateInfo.nickname != "" &&
        (data.updateInfo.nickname.length < 3 ||
          data.updateInfo.nickname.length > 10)
      ) {
        return;
      }
      if (
        data.updateInfo.email != "" &&
        !checkEmailValid(data.updateInfo.email)
      ) {
        ElMessage("请输入有效的邮箱。");
        return;
      }

      if (data.updateInfo.nickname != "") {
        data.userInfo.nickname = data.updateInfo.nickname;
      }
      if (data.updateInfo.email != "") {
        data.userInfo.email = data.updateInfo.email;
      }
      if (data.fileList.length != 0) {
        data.updateInfo.avatar = "/static/avatars/" + data.fileList[0].name;
        data.userInfo.avatar = store.state.backendUrl + data.updateInfo.avatar;
        store.commit("setUserInfo", data.userInfo);
        data.uploadRef.submit();
      }
      if (data.updateInfo.birthday != "") {
        data.userInfo.birthday = data.updateInfo.birthday;
      }
      if (data.updateInfo.signature != "") {
        data.userInfo.signature = data.updateInfo.signature;
      }

      data.isMyInfoModalVisible = false;
      data.fileList = [];
      data.cnt = 0;
      data.updateInfo.uuid = data.userInfo.uuid;
      store.commit("setUserInfo", data.userInfo);

      try {
        const rsp = await axios.post(
          store.state.apiUrl + "/user/updateUserInfo",
          data.updateInfo
        );
        if (rsp.data.code == 0) {
          ElMessage.success(rsp.data.message);
        } else {
          ElMessage.error(rsp.data.message);
        }
      } catch (error) {
        console.log(error);
      }

      data.updateInfo = {
        uuid: "",
        nickname: "",
        email: "",
        birthday: "",
        signature: "",
        avatar: "",
      };
    };

    const quitMyInfoModal = () => {
      data.isMyInfoModalVisible = false;
      data.fileList = [];
      data.cnt = 0;
    };

    const handleUploadSuccess = () => {
      ElMessage.success("头像上传成功");
      data.fileList = [];
    };

    const beforeFileUpload = (file) => {
      if (data.fileList.length > 1) {
        ElMessage.error("只能上传一张头像");
        return false;
      }
      const isLt50M = file.size / 1024 / 1024 < 50;
      if (!isLt50M) {
        ElMessage.error("上传头像图片大小不能超过 50MB!");
        return false;
      }
    };

    return {
      ...toRefs(data),
      showMyInfoModal,
      closeMyInfoModal,
      quitMyInfoModal,
      handleUploadSuccess,
      beforeFileUpload,
    };
  },
};
</script>

<style scoped>
.chat-page-shell {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.owner-info-window {
  flex: 1;
  min-width: 0;
  padding: 28px;
  display: flex;
  flex-direction: column;
  gap: 22px;
  background:
    radial-gradient(circle at top right, rgba(7, 193, 96, 0.08), transparent 26%),
    linear-gradient(180deg, #fbfdfb 0%, #f4f8f4 100%);
}

.profile-card {
  border-radius: 24px;
  border: 1px solid var(--go-border);
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.96) 0%, rgba(248, 251, 248, 0.98) 100%);
  box-shadow: var(--go-shadow-soft);
  padding: 6px 24px;
  display: flex;
  flex-direction: column;
}

.profile-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 4px 4px 0;
  border: none;
  background: transparent;
  box-shadow: none;
}

.profile-header-main {
  display: flex;
  align-items: center;
  gap: 18px;
}

.profile-avatar {
  flex-shrink: 0;
}

.profile-header-copy h2 {
  margin: 0 0 6px;
  font-size: 28px;
  font-weight: 650;
  color: var(--go-text-strong);
  letter-spacing: -0.03em;
}

.profile-header-copy p {
  margin: 0;
  color: var(--go-text-muted);
  font-size: 14px;
  line-height: 1.6;
}

.profile-edit-btn {
  width: 88px;
  min-height: 40px;
  border: 1px solid var(--go-border);
  background: rgba(255, 255, 255, 0.88);
  color: var(--go-text);
  box-shadow: none;
}

.profile-edit-btn:hover {
  background: #fff;
  color: var(--go-text-strong);
}

.profile-row {
  min-height: 62px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border-bottom: 1px solid #edf2ee;
}

.profile-row:last-child {
  border-bottom: none;
}

.profile-row--top {
  align-items: flex-start;
  padding-top: 14px;
  padding-bottom: 14px;
}

.profile-label {
  color: #67746c;
  font-size: 14px;
  font-weight: 600;
}

.profile-value {
  max-width: 64%;
  color: var(--go-text-strong);
  font-size: 14px;
  text-align: right;
  word-break: break-all;
}

.profile-avatar-row {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
}

h3 {
  margin: 0;
  color: var(--go-text-strong);
}

.modal-quit-btn-container {
  width: 100%;
  display: flex;
  justify-content: flex-end;
}

.modal-quit-btn {
  background: transparent;
  color: #7d8881;
  padding: 12px;
  border: none;
  cursor: pointer;
}

.modal-header {
  width: 100%;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
}

.modal-body {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.modal-footer {
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
}

.modal-header-title {
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
}

@media (max-width: 1100px) {
  .profile-header {
    flex-direction: column;
    align-items: flex-start;
  }
}

@media (max-width: 820px) {
  .owner-info-window {
    padding: 18px;
  }

  .profile-header-main {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
