<template>
  <div class="contactlist-container">
    <div class="contactlist-header">
      <el-input
        v-model="contactSearch"
        class="contact-search-input"
        placeholder="搜索联系人/群聊"
        size="small"
        suffix-icon="Search"
        clearable
      />
      <div class="contactlist-header-right">
        <el-dropdown placement="bottom" trigger="click">
          <button class="create-group-btn">
            <svg
              t="1733664667695"
              class="create-group-icon"
              viewBox="0 0 1024 1024"
              version="1.1"
              xmlns="http://www.w3.org/2000/svg"
              p-id="2875"
              width="128"
              height="128"
            >
              <path
                d="M488.021333 96a248.021333 248.021333 0 1 1-17.92 495.36l-1.749333 0.341333-4.352 0.298667A304 304 0 0 0 160 896a32 32 0 1 1-64 0 368.170667 368.170667 0 0 1 250.026667-348.672A247.978667 247.978667 0 0 1 488.021333 96z m288 528a32 32 0 0 1 32 32l-0.042666 87.978667H896a32 32 0 0 1 31.701333 27.690666l0.298667 4.352a32 32 0 0 1-32 32l-88.021333-0.042666V896a32 32 0 0 1-27.648 31.701333l-4.352 0.298667a32 32 0 0 1-32-32v-88.021333h-87.978667a32 32 0 0 1-31.701333-27.648l-0.298667-4.352a32 32 0 0 1 32-32h87.978667v-87.978667a32 32 0 0 1 27.690666-31.701333zM488.021333 160a184.021333 184.021333 0 1 0 0 368 184.021333 184.021333 0 0 0 0-368z"
                fill="#2c2c2c"
                p-id="2876"
              ></path>
            </svg>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="showCreateGroupModal">
                创建群聊
              </el-dropdown-item>
              <el-dropdown-item @click="showApplyContactModal">
                添加用户/群聊
              </el-dropdown-item>
              <el-dropdown-item @click="showNewContactModal">
                新的好友
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <SmallModal :isVisible="isNewContactModalVisible">
          <template v-slot:header>
            <div class="modal-header">
              <div class="modal-quit-btn-container">
                <button class="modal-quit-btn" @click="quitNewContactModal">
                  <el-icon><Close /></el-icon>
                </button>
              </div>
              <div class="modal-header-title">
                <h3>新的朋友</h3>
              </div>
            </div>
          </template>
          <template v-slot:body>
            <div class="newcontact-modal-body">
              <el-scrollbar max-height="400px">
                <ul class="newcontact-list" style="list-style-type: none">
                  <li
                    v-for="newContact in newContactList"
                    :key="newContact.contact_id"
                    class="newcontact-item"
                  >
                    <div
                      style="
                        display: flex;
                        align-items: center;
                        justify-content: center;
                      "
                    >
                      <img
                        :src="newContact.contact_avatar"
                        style="width: 30px; height: 30px; margin-right: 10px"
                      />

                      <el-tooltip
                        effect="customized"
                        :content="newContact.message"
                        placement="top"
                        hide-after="0"
                        enterable="false"
                      >
                        <div>
                          {{ newContact.contact_name }}
                        </div>
                      </el-tooltip>
                    </div>
                    <el-dropdown placement="right" trigger="click">
                      <el-button class="action-btn"> 去处理 </el-button>
                      <template #dropdown>
                        <el-dropdown-menu>
                          <el-dropdown-item
                            @click="handleAgree(newContact.contact_id)"
                            >同意</el-dropdown-item
                          >
                          <el-dropdown-item
                            @click="handleReject(newContact.contact_id)"
                          >
                            拒绝
                          </el-dropdown-item>
                          <el-dropdown-item
                            @click="handleBlack(newContact.contact_id)"
                          >
                            拉黑
                          </el-dropdown-item>
                        </el-dropdown-menu>
                      </template>
                    </el-dropdown>
                  </li>
                </ul>
              </el-scrollbar>
            </div>
          </template>
          <template v-slot:footer>
            <div class="newcontact-modal-footer"></div>
          </template>
        </SmallModal>
        <SmallModal :isVisible="isApplyContactModalVisible">
          <template v-slot:header>
            <div class="modal-header">
              <div class="modal-quit-btn-container">
                <button class="modal-quit-btn" @click="quitApplyContactModal">
                  <el-icon><Close /></el-icon>
                </button>
              </div>
              <div class="modal-header-title">
                <h3>添加用户/群聊</h3>
              </div>
            </div>
          </template>
          <template v-slot:body>
            <div class="modal-body">
              <el-form
                ref="formRef"
                :model="applyContactReq"
                label-width="100px"
                class="apply-contact-form"
              >
                <el-form-item
                  prop="name"
                  label="用户/群聊id"
                  :rules="[
                    {
                      required: true,
                      message: '此项为必填项',
                      trigger: 'blur',
                    },
                  ]"
                >
                  <el-input
                    v-model="applyContactReq.contact_id"
                    placeholder="请填写申请的用户/群聊id"
                  />
                </el-form-item>
                <el-form-item prop="message" label="申请消息">
                  <el-input
                    v-model="applyContactReq.message"
                    placeholder="选填，填写更容易通过"
                    type="textarea"
                    show-word-limit
                    maxlength="100"
                    :autosize="{ minRows: 3, maxRows: 3 }"
                  />
                </el-form-item>
              </el-form>
            </div>
          </template>
          <template v-slot:footer>
            <div class="modal-footer">
              <el-button
                class="modal-close-btn"
                @click="closeApplyContactModal"
              >
                完成
              </el-button>
            </div>
          </template>
        </SmallModal>
        <Modal :isVisible="isCreateGroupModalVisible">
          <template v-slot:header>
            <div class="modal-header">
              <div class="modal-quit-btn-container">
                <button class="modal-quit-btn" @click="quitCreateGroupModal">
                  <el-icon><Close /></el-icon>
                </button>
              </div>
              <div class="modal-header-title">
                <h3>创建群聊</h3>
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
              <div class="creatgroup-modal-body">
                <el-form
                  ref="formRef"
                  :model="createGroupReq"
                  label-width="80px"
                  class="demo-dynamic"
                >
                  <el-form-item
                    prop="name"
                    label="群名称"
                    :rules="[
                      {
                        required: true,
                        message: '此项为必填项',
                        trigger: 'blur',
                      },
                    ]"
                  >
                    <el-input
                      v-model="createGroupReq.name"
                      placeholder="必填"
                    />
                  </el-form-item>
                  <el-form-item prop="notice" label="群公告">
                    <el-input
                      v-model="createGroupReq.notice"
                      type="textarea"
                      show-word-limit
                      maxlength="500"
                      :autosize="{ minRows: 3, maxRows: 3 }"
                      placeholder="选填"
                    />
                  </el-form-item>
                  <el-form-item
                    prop="add_mode"
                    label="加群方式"
                    :rules="[
                      {
                        required: true,
                        message: 'Please select activity resource',
                        trigger: 'change',
                      },
                    ]"
                  >
                    <el-radio-group v-model="createGroupReq.add_mode">
                      <el-radio :value="0">直接加入</el-radio>
                      <el-radio :value="1">群主审核</el-radio>
                    </el-radio-group>
                  </el-form-item>
                  <el-form-item prop="avatar" label="群头像">
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
            <div class="creategroup-modal-footer">
              <el-button class="modal-close-btn" @click="closeCreateGroupModal">
                完成
              </el-button>
            </div>
          </template>
        </Modal>
      </div>
    </div>
    <div class="contactlist-body">
      <div class="contactlist-user">
        <el-menu
          router
          unique-opened
          :default-openeds="['contacts']"
          @open="handleShowUserList"
          @close="handleHideUserList"
        >
          <el-sub-menu index="contacts">
            <template #title>
              <div class="contactlist-title-row">
                <span class="contactlist-user-title">联系人</span>
                <span class="contactlist-count">
                  {{ filteredContactUserList.length }}
                </span>
              </div>
            </template>
          </el-sub-menu>
          <el-menu-item
            v-for="user in filteredContactUserList"
            :key="user.user_id"
            @click="handleToChatUser(user)"
            class="contactlist-user-menu-item"
          >
            <el-dropdown
              trigger="contextmenu"
              class="contactlist-dropdown"
              placement="right"
            >
              <div class="contactlist-user-item">
                <img :src="user.avatar" class="contactlist-user-avatar" />
                {{ user.user_name }}
              </div>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item @click="handleCancelBlack(user)"
                    >解除拉黑</el-dropdown-item
                  >
                  <!-- 其他菜单项 -->
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </el-menu-item>
          <el-menu-item
            v-if="!filteredContactUserList.length"
            disabled
            class="menu-empty-item"
          >
            没有匹配的联系人
          </el-menu-item>
        </el-menu>

        <el-menu
          router
          unique-opened
          :default-openeds="['owned-groups']"
          @open="handleShowMyGroupList"
          @close="handleHideMyGroupList"
        >
          <el-sub-menu index="owned-groups">
            <template #title>
              <div class="contactlist-title-row">
                <span class="contactlist-user-title">我创建的群聊</span>
                <span class="contactlist-count">
                  {{ filteredMyGroupList.length }}
                </span>
              </div>
            </template>
          </el-sub-menu>
          <el-menu-item
            v-for="group in filteredMyGroupList"
            :key="group.group_id"
            @click="handleToChatGroup(group)"
          >
            <img :src="group.avatar" class="contactlist-avatar" />
            {{ group.group_name }}
          </el-menu-item>
          <el-menu-item
            v-if="!filteredMyGroupList.length"
            disabled
            class="menu-empty-item"
          >
            没有匹配的群聊
          </el-menu-item>
        </el-menu>
        <el-menu
          router
          unique-opened
          :default-openeds="['joined-groups']"
          @open="handleShowMyJoinedGroupList"
          @close="handleHideMyJoinedGroupList"
        >
          <el-sub-menu index="joined-groups">
            <template #title>
              <div class="contactlist-title-row">
                <span class="contactlist-user-title">我加入的群聊</span>
                <span class="contactlist-count">
                  {{ filteredMyJoinedGroupList.length }}
                </span>
              </div>
            </template>
          </el-sub-menu>
          <el-menu-item
            v-for="group in filteredMyJoinedGroupList"
            :key="group.group_id"
            @click="handleToChatGroup(group)"
          >
            <img :src="group.avatar" class="contactlist-avatar" />
            {{ group.group_name }}
          </el-menu-item>
          <el-menu-item
            v-if="!filteredMyJoinedGroupList.length"
            disabled
            class="menu-empty-item"
          >
            没有匹配的群聊
          </el-menu-item>
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
import { ElMessage } from "element-plus";
import Modal from "./Modal.vue";
import SmallModal from "./SmallModal.vue";
export default {
  name: "ContactListModal",
  components: {
    Modal,
    SmallModal,
  },
  setup() {
    const router = useRouter();
    const store = useStore();
    const data = reactive({
      userInfo: store.state.userInfo,
      contactSearch: "",
      createGroupReq: {
        owner_id: "",
        name: "",
        notice: "",
        add_mode: null,
        avatar: "",
      },
      isCreateGroupModalVisible: false,
      isApplyContactModalVisible: false,
      isNewContactModalVisible: false,
      contactUserList: [],
      myGroupList: [],
      myJoinedGroupList: [],
      applyContactReq: {
        owner_id: "",
        contact_id: "",
        message: "",
      },
      ownListReq: {
        owner_id: "",
      },
      newContactList: [],
      uploadRef: null,
      uploadPath: store.state.backendUrl + "/message/uploadAvatar",
      fileList: [],
      loadedUserList: false,
      loadedMyGroupList: false,
      loadedMyJoinedGroupList: false,
      loadingUserList: false,
      loadingMyGroupList: false,
      loadingMyJoinedGroupList: false,
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

    const filteredContactUserList = computed(() =>
      data.contactUserList.filter((user) =>
        matchesSearch([user.user_name, user.user_id])
      )
    );

    const filteredMyGroupList = computed(() =>
      data.myGroupList.filter((group) =>
        matchesSearch([group.group_name, group.group_id])
      )
    );

    const filteredMyJoinedGroupList = computed(() =>
      data.myJoinedGroupList.filter((group) =>
        matchesSearch([group.group_name, group.group_id])
      )
    );

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

    const handleUploadSuccess = () => {
      ElMessage.success("头像上传成功");
      data.fileList = [];
    };
    const beforeFileUpload = (file) => {
      console.log("上传前file====>", file);
      console.log(data.fileList);
      console.log(file);
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
    const handleCreateGroup = async () => {
      try {
        data.createGroupReq.owner_id = data.userInfo.uuid;
        if (data.fileList.length > 0) {
          data.createGroupReq.avatar =
            "/static/avatars/" + data.fileList[0].name;
          console.log(data.createGroupReq.avatar);
          data.uploadRef.submit();
        }
        const response = await axios.post(
          store.state.backendUrl + "/group/createGroup",
          data.createGroupReq
        );
        if (response.data.code == 200) {
          data.loadedMyGroupList = false;
          await handleShowMyGroupList();
        }
      } catch (error) {
        console.error(error);
      }
    };
    const showCreateGroupModal = () => {
      data.isCreateGroupModalVisible = true;
    };
    const quitCreateGroupModal = () => {
      data.isCreateGroupModalVisible = false;
    };
    const closeCreateGroupModal = () => {
      if (data.createGroupReq.name == "") {
        ElMessage("请输入群聊名称");
        return;
      }
      if (data.createGroupReq.add_mode == null) {
        ElMessage("请选择加群方式");
        return;
      }
      data.isCreateGroupModalVisible = false;
      handleCreateGroup();
    };
    const showApplyContactModal = () => {
      data.isApplyContactModalVisible = true;
    };
    const quitApplyContactModal = () => {
      data.isApplyContactModalVisible = false;
    };
    const closeApplyContactModal = () => {
      if (data.applyContactReq.contact_id == "") {
        ElMessage.error("请输入申请用户/群组id");
        return;
      }
      if (data.applyContactReq.contact_id[0] == "G") {
        handleApplyGroup();
      } else {
        handleApplyContact();
      }
    };

    const showNewContactModal = () => {
      handleNewContactList();
    };

    const quitNewContactModal = () => {
      data.isNewContactModalVisible = false;
      data.newContactList = [];
    };
    const handleApplyGroup = async () => {
      try {
        let req = {
          group_id: data.applyContactReq.contact_id,
        };
        let rsp = await axios.post(
          store.state.backendUrl + "/group/checkGroupAddMode",
          req
        );
        if (rsp.data.code == 200) {
          if (rsp.data.data == 0) {
            // 直接加入
            handleEnterDirectly(data.applyContactReq.contact_id);
            return;
          }
        } else {
          ElMessage.error("申请失败");
          return;
        }
        data.applyContactReq.owner_id = data.userInfo.uuid;
        rsp = await axios.post(
          store.state.backendUrl + "/contact/applyContact",
          data.applyContactReq
        );
        console.log(rsp);
        if (rsp.data.code == 200) {
          if (rsp.data.message == "申请成功") {
            data.isApplyContactModalVisible = false;
            ElMessage.success("申请成功");
            return;
          } else {
            ElMessage.error(rsp.data.message);
          }
        } else {
          ElMessage.error("申请失败");
        }
      } catch (error) {
        console.error(error);
      }
    };
    const handleApplyContact = async () => {
      try {
        data.applyContactReq.owner_id = data.userInfo.uuid;
        const rsp = await axios.post(
          store.state.backendUrl + "/contact/applyContact",
          data.applyContactReq
        );
        console.log(rsp);
        if (rsp.data.code == 200) {
          if (rsp.data.message == "申请成功") {
            data.isApplyContactModalVisible = false;
            ElMessage.success("申请成功");
            return;
          }
        }
        ElMessage.error(rsp.data.message);
      } catch (error) {
        console.error(error);
      }
    };
    const handleShowUserList = async () => {
      if (data.loadedUserList || data.loadingUserList) {
        return;
      }
      data.loadingUserList = true;
      try {
        data.ownListReq.owner_id = data.userInfo.uuid;
        const getUserListRsp = await axios.post(
          store.state.backendUrl + "/contact/getUserList",
          data.ownListReq
        );
        data.contactUserList = normalizeAvatarList(getUserListRsp.data.data || []);
        data.loadedUserList = true;
      } catch (error) {
        console.error(error);
      } finally {
        data.loadingUserList = false;
      }
    };
    const handleHideUserList = () => {};

    const handleShowMyGroupList = async () => {
      if (data.loadedMyGroupList || data.loadingMyGroupList) {
        return;
      }
      data.loadingMyGroupList = true;
      try {
        data.ownListReq.owner_id = data.userInfo.uuid;
        const loadMyGroupRsp = await axios.post(
          store.state.backendUrl + "/group/loadMyGroup",
          data.ownListReq
        );
        data.myGroupList = normalizeAvatarList(loadMyGroupRsp.data.data || []);
        data.loadedMyGroupList = true;
      } catch (error) {
        console.error(error);
      } finally {
        data.loadingMyGroupList = false;
      }
    };
    const handleHideMyGroupList = () => {};
    const handleShowMyJoinedGroupList = async () => {
      if (data.loadedMyJoinedGroupList || data.loadingMyJoinedGroupList) {
        return;
      }
      data.loadingMyJoinedGroupList = true;
      try {
        data.ownListReq.owner_id = data.userInfo.uuid;
        const loadMyJoinedGroupRsp = await axios.post(
          store.state.backendUrl + "/contact/loadMyJoinedGroup",
          data.ownListReq
        );
        data.myJoinedGroupList = normalizeAvatarList(
          loadMyJoinedGroupRsp.data.data || []
        );
        data.loadedMyJoinedGroupList = true;
      } catch (error) {
        console.error(error);
      } finally {
        data.loadingMyJoinedGroupList = false;
      }
    };
    const handleHideMyJoinedGroupList = () => {};

    const handleToChatUser = async (user) => {
      try {
        const req = {
          send_id: data.userInfo.uuid,
          receive_id: user.user_id,
        };
        const rsp = await axios.post(
          store.state.backendUrl + "/session/checkOpenSessionAllowed",
          req
        );
        if (rsp.data.code == 200) {
          if (rsp.data.data == true) {
            router.push("/chat/" + user.user_id);
          } else {
            ElMessage.warning(rsp.data.message);
            console.error(rsp.data.message);
          }
        } else {
          ElMessage.error(rsp.data.message);
          console.error(rsp.data.message);
        }
      } catch (error) {
        ElMessage.error(error);
        console.error(error);
      }
    };

    const handleToChatGroup = async (group) => {
      try {
        const req = {
          send_id: data.userInfo.uuid,
          receive_id: group.group_id,
        };
        const rsp = await axios.post(
          store.state.backendUrl + "/session/checkOpenSessionAllowed",
          req
        );
        if (rsp.data.code == 200) {
          if (rsp.data.data == true) {
            router.push("/chat/" + group.group_id);
          } else {
            ElMessage.warning(rsp.data.message);
            console.error(rsp.data.message);
          }
        } else {
          if (rsp.data.code == 400) {
            ElMessage.warning(rsp.data.message);
            console.error(rsp.data.message);
          } else {
            ElMessage.error(rsp.data.message);
            console.error(rsp.data.message);
          }
        }
      } catch (error) {
        console.error(error);
      }
    };

    const handleNewContactList = async () => {
      try {
        data.ownListReq.owner_id = data.userInfo.uuid;
        const rsp = await axios.post(
          store.state.backendUrl + "/contact/getNewContactList",
          data.ownListReq
        );
        console.log(rsp);
        data.newContactList = rsp.data.data;
        if (data.newContactList == null) {
          ElMessage.warning("没有新的好友申请");
          return;
        }
        for (let i = 0; i < data.newContactList.length; i++) {
          if (!data.newContactList[i].contact_avatar.startsWith("http")) {
            data.newContactList[i].contact_avatar =
              store.state.backendUrl + data.newContactList[i].contact_avatar;
          }
        }
        data.isNewContactModalVisible = true;
        console.log(rsp);
      } catch (error) {
        console.error(error);
      }
    };

    const handleAgree = async (contactId) => {
      try {
        const req = {
          owner_id: data.userInfo.uuid,
          contact_id: contactId,
        };
        const rsp = await axios.post(
          store.state.backendUrl + "/contact/passContactApply",
          req
        );
        console.log(rsp);
        if (rsp.data.code == 200) {
          ElMessage.success(rsp.data.message);
          data.newContactList = data.newContactList.filter(
            (c) => c.contact_id !== contactId
          );
          data.loadedUserList = false;
          await handleShowUserList();
        } else {
          ElMessage.error(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };
    const handleEnterDirectly = async (groupId) => {
      try {
        const req = {
          owner_id: groupId,
          contact_id: data.userInfo.uuid,
        };
        const rsp = await axios.post(
          store.state.backendUrl + "/group/enterGroupDirectly",
          req
        );
        console.log(rsp);
        if (rsp.data.code == 200) {
          ElMessage.success(rsp.data.message);
          data.isApplyContactModalVisible = false;
          data.loadedMyJoinedGroupList = false;
          await handleShowMyJoinedGroupList();
        } else {
          ElMessage.error(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };
    const handleReject = async (contactId) => {
      try {
        const req = {
          owner_id: data.userInfo.uuid,
          contact_id: contactId,
        };
        const rsp = await axios.post(
          store.state.backendUrl + "/contact/refuseContactApply",
          req
        );
        console.log(rsp);
        if (rsp.data.code == 200) {
          ElMessage.success(rsp.data.message);
          console.log(rsp.data.message);
          data.newContactList = data.newContactList.filter(
            (c) => c.contact_id !== contactId
          );
        } else if (rsp.data.code == 400) {
          ElMessage.warning(rsp.data.message);
          console.log(rsp.data.message);
        } else if (rsp.data.code == 500) {
          ElMessage.error(rsp.data.message);
          console.log(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };
    const handleBlack = async (contactId) => {
      try {
        const req = {
          owner_id: data.userInfo.uuid,
          contact_id: contactId,
        };
        const rsp = await axios.post(
          store.state.backendUrl + "/contact/blackApply",
          req
        );
        if (rsp.data.code == 200) {
          ElMessage.success(rsp.data.message);
          console.log(rsp.data.message);
          data.newContactList = data.newContactList.filter(
            (c) => c.contact_id !== contactId
          );
        } else if (rsp.data.code == 400) {
          ElMessage.warning(rsp.data.message);
          console.log(rsp.data.message);
        } else if (rsp.data.code == 500) {
          ElMessage.error(rsp.data.message);
          console.log(rsp.data.message);
        }
      } catch (error) {
        ElMessage.error(error);
        console.error(error);
      }
    };
    const handleCancelBlack = async (user) => {
      try {
        const req = {
          owner_id: data.userInfo.uuid,
          contact_id: user.user_id,
        };
        const rsp = await axios.post(
          store.state.backendUrl + "/contact/cancelBlackContact",
          req
        );
        if (rsp.data.code == 200) {
          ElMessage.success(rsp.data.message);
          console.log(rsp.data.message);
          data.loadedUserList = false;
          await handleShowUserList();
        } else if (rsp.data.code == 400) {
          ElMessage.warning(rsp.data.message);
          console.log(rsp.data.message);
        } else if (rsp.data.code == 500) {
          ElMessage.error(rsp.data.message);
          console.log(rsp.data.message);
        }
      } catch (error) {
        ElMessage.error(error);
        console.error(error);
      }
    };

    const preloadContactLists = async () => {
      try {
        await Promise.all([
          handleShowUserList(),
          handleShowMyGroupList(),
          handleShowMyJoinedGroupList(),
        ]);
      } catch (error) {
        console.error(error);
      }
    };

    onMounted(() => {
      preloadContactLists();
    });

    return {
      ...toRefs(data),
      filteredContactUserList,
      filteredMyGroupList,
      filteredMyJoinedGroupList,
      preloadContactLists,
      router,
      handleCreateGroup,
      showCreateGroupModal,
      closeCreateGroupModal,
      quitCreateGroupModal,
      showApplyContactModal,
      quitApplyContactModal,
      closeApplyContactModal,
      showNewContactModal,
      quitNewContactModal,
      handleShowUserList,
      handleHideUserList,
      handleShowMyGroupList,
      handleHideMyGroupList,
      handleShowMyJoinedGroupList,
      handleHideMyJoinedGroupList,
      handleToChatUser,
      handleToChatGroup,
      handleNewContactList,
      handleAgree,
      handleReject,
      handleCancelBlack,
      handleBlack,
      handleUploadSuccess,
      beforeFileUpload,
    };
  },
};
</script>

<style scoped>
.contactlist-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.contact-search-input {
  width: 100%;
}

.contact-search-input :deep(.el-input__wrapper) {
  background: rgba(255, 255, 255, 0.72);
}

.contactlist-header-right {
  width: 36px;
  height: 36px;
  display: flex;
  justify-content: center;
  align-items: center;
}

.create-group-btn {
  width: 36px;
  height: 36px;
  border-radius: 12px;
  cursor: pointer;
  border: 1px solid var(--go-border);
  display: flex;
  justify-content: center;
  align-items: center;
  background: #fff;
  transition:
    background-color 0.18s ease,
    border-color 0.18s ease;
}

.create-group-btn:hover {
  background: #f3f7f4;
  border-color: #ccd7d0;
}

.create-group-icon {
  width: 18px;
  height: 18px;
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

.contactlist-user-title {
  font-size: 14px;
  font-weight: 600;
}

.contactlist-title-row {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.contactlist-count {
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

.creatgroup-modal-body {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.newcontact-modal-body {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.newcontact-modal-footer {
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
}

.modal-footer {
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
}

.creategroup-modal-footer {
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

.contactlist-avatar {
  width: 32px;
  height: 32px;
  margin-right: 12px;
  border-radius: 10px;
  object-fit: cover;
}

.newcontact-list {
  width: 100%;
  max-width: 300px;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
  padding: 4px 0;
}

.newcontact-item {
  display: flex;
  flex-direction: row;
  justify-content: space-between;
  align-items: center;
  width: 100%;
  min-height: 54px;
  padding: 8px 0;
  border-bottom: 1px solid #eff3f0;
}

.action-btn {
  border: 1px solid var(--go-border);
  border-radius: 10px;
  cursor: pointer;
  justify-content: center;
  align-items: center;
  color: var(--go-text);
  background: #f4f7f5;
}

.contactlist-user-menu-item {
  justify-content: center;
  align-items: center;
}

.contactlist-user-item {
  width: 100%;
  height: 44px;
  display: flex;
  align-items: center;
  color: var(--go-text);
}

.contactlist-user-avatar {
  width: 32px;
  height: 32px;
  margin-left: 12px;
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
