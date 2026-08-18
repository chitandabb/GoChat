<template>
  <div class="chat-page-shell">
    <el-header>
            <div class="chat-title" v-if="contactInfo.contact_avatar">
              <img
                :src="contactInfo.contact_avatar"
                class="chat-title-avatar"
              />
              <div class="chat-title-copy">
                <h2 class="chat-name">{{ contactInfo.contact_name }}</h2>
                <p class="chat-title-subtitle">{{ chatTitleMeta }}</p>
              </div>
            </div>
            <div class="chat-title-right">
              <span
                v-if="store.state.connectionState !== 'connected'"
                class="conn-state-pill"
              >
                ● {{ store.state.connectionState === "reconnecting" ? "连接断开，重连中…" : "未连接" }}
              </span>
              <span class="chat-status-pill">{{ chatStatusText }}</span>
              <Modal :isVisible="isUserContactInfoModalVisible">
                <template v-slot:header>
                  <div class="userinfo-modal-quit-btn-container">
                    <button
                      class="userinfo-modal-quit-btn"
                      @click="quitUserContactInfoModal"
                    >
                      <el-icon><Close /></el-icon>
                    </button>
                  </div>
                  <div class="userinfo-modal-header-title">
                    <h3>个人主页</h3>
                  </div>
                </template>
                <template v-slot:body>
                  <el-descriptions
                    direction="vertical"
                    border
                    class="modal-list"
                    size="small"
                  >
                    <el-descriptions-item
                      :rowspan="2"
                      :width="120"
                      label="头像"
                      align="center"
                    >
                      <el-image
                        style="width: 100px; height: 100px"
                        :src="contactInfo.contact_avatar"
                      />
                    </el-descriptions-item>
                    <el-descriptions-item label="Id" :width="140">{{
                      contactInfo.contact_id
                    }}</el-descriptions-item>
                    <el-descriptions-item label="性别">{{
                      contactInfo.contact_gender == 0 ? "男" : "女"
                    }}</el-descriptions-item>
                    <el-descriptions-item label="电话号码">{{
                      contactInfo.contact_phone
                    }}</el-descriptions-item>
                    <el-descriptions-item label="昵称">{{
                      contactInfo.contact_name
                    }}</el-descriptions-item>

                    <el-descriptions-item label="邮箱" :span="2">
                      <div style="height: 30px">
                        {{ contactInfo.contact_email }}
                      </div></el-descriptions-item
                    >

                    <el-descriptions-item label="生日" :span="1" :width="140"
                      >{{ contactInfo.contact_birthday }}
                    </el-descriptions-item>
                    <el-descriptions-item label="个性签名">
                      <div style="height: 70px">
                        {{ contactInfo.contact_signature }}
                      </div>
                    </el-descriptions-item>
                  </el-descriptions>
                </template>
              </Modal>
              <Modal :isVisible="isGroupContactInfoModalVisible">
                <template v-slot:header>
                  <div class="groupcontactinfo-modal-quit-btn-container">
                    <button
                      class="groupcontactinfo-modal-quit-btn"
                      @click="quitGroupContactInfoModal"
                    >
                      <el-icon><Close /></el-icon>
                    </button>
                  </div>
                  <div class="groupcontactinfo-modal-header-title">
                    <h3>群聊主页</h3>
                  </div>
                </template>
                <template v-slot:body>
                  <el-descriptions
                    direction="vertical"
                    border
                    class="modal-list"
                    size="small"
                  >
                    <el-descriptions-item
                      :rowspan="2"
                      :width="120"
                      label="头像"
                      align="center"
                    >
                      <el-image
                        style="width: 100px; height: 100px"
                        :src="contactInfo.contact_avatar"
                      />
                    </el-descriptions-item>
                    <el-descriptions-item label="Id" :width="140">{{
                      contactInfo.contact_id
                    }}</el-descriptions-item>
                    <el-descriptions-item label="群人数">{{
                      contactInfo.contact_member_cnt
                    }}</el-descriptions-item>
                    <el-descriptions-item label="群主id">{{
                      contactInfo.contact_owner_id
                    }}</el-descriptions-item>
                    <el-descriptions-item label="入群方式" :width="140"
                      >{{
                        contactInfo.contact_add_mode == 0
                          ? "直接加入"
                          : "群主审核"
                      }}
                    </el-descriptions-item>
                    <el-descriptions-item label="群名称" :span="3">{{
                      contactInfo.contact_name
                    }}</el-descriptions-item>
                    <el-descriptions-item label="群公告" :span="3">
                      <div style="height: 70px">
                        {{ contactInfo.contact_notice }}
                      </div>
                    </el-descriptions-item>
                  </el-descriptions>
                </template>
              </Modal>
              <el-dropdown placement="bottom" trigger="click">
                <button class="setting-btn">
                  <el-icon><MoreFilled /></el-icon>
                </button>
                <template #dropdown>
                  <el-dropdown-menu v-if="contactInfo.contact_id[0] === 'U'">
                    <el-dropdown-item @click="showUserContactInfoModal">
                      个人信息
                    </el-dropdown-item>

                    <el-dropdown-item @click="preToDeleteSession"
                      >删除该会话</el-dropdown-item
                    >
                    <el-dropdown-item @click="preToDeleteContact"
                      >删除联系人</el-dropdown-item
                    >
                    <el-dropdown-item @click="preToBlackContact"
                      >拉黑联系人</el-dropdown-item
                    >
                  </el-dropdown-menu>
                  <el-dropdown-menu
                    v-else-if="contactInfo.contact_id[0] === 'G'"
                  >
                    <el-dropdown-item @click="showGroupContactInfoModal"
                      >群聊信息</el-dropdown-item
                    >
                    <el-dropdown-item
                      v-if="contactInfo.contact_owner_id == userInfo.uuid"
                      @click="showUpdateGroupInfoModal"
                    >
                      修改群聊信息
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="contactInfo.contact_owner_id == userInfo.uuid"
                      @click="showRemoveGroupMemberModal"
                    >
                      移除群组人员
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="contactInfo.contact_owner_id == userInfo.uuid"
                      @click="showAddGroupModal"
                      >加群申请</el-dropdown-item
                    >
                    <el-dropdown-item @click="preToDeleteSession"
                      >删除该会话</el-dropdown-item
                    >
                    <el-dropdown-item
                      v-if="contactInfo.contact_owner_id == userInfo.uuid"
                      @click="handleDismissGroup"
                      >解散群聊</el-dropdown-item
                    >
                    <el-dropdown-item
                      v-if="contactInfo.contact_owner_id != userInfo.uuid"
                      @click="handleLeaveGroup"
                      >退出群聊</el-dropdown-item
                    >
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
              <Modal :isVisible="isUpdateGroupInfoModalVisible">
                <template v-slot:header>
                  <div class="updategroupinfo-modal-quit-btn-container">
                    <button
                      class="updategroupinfo-modal-quit-btn"
                      @click="quitUpdateGroupInfoModal"
                    >
                      <el-icon><Close /></el-icon>
                    </button>
                  </div>
                  <div class="updategroupinfo-modal-header-title">
                    <h3>修改群聊信息</h3>
                  </div>
                </template>
                <template v-slot:body>
                  <el-scrollbar
                    max-height="255px"
                    style="
                      width: 400px;
                      height: 255px;
                      display: flex;
                      align-items: center;
                      justify-content: center;
                      margin-top: 20px;
                    "
                  >
                    <div class="modal-body">
                      <el-form
                        ref="formRef"
                        :model="updateGroupInfo"
                        label-width="80px"
                      >
                        <el-form-item
                          prop="name"
                          label="群名称"
                          :rules="[
                            {
                              min: 3,
                              max: 10,
                              message: '群名称长度在 3 到 10 个字符',
                              trigger: 'blur',
                            },
                          ]"
                        >
                          <el-input
                            v-model="updateGroupInfo.name"
                            placeholder="选填"
                          />
                        </el-form-item>
                        <el-form-item prop="add_mode" label="入群方式">
                          <el-radio-group v-model="updateGroupInfo.add_mode">
                            <el-radio :value="0">直接加入</el-radio>
                            <el-radio :value="1">群主审核</el-radio>
                          </el-radio-group>
                        </el-form-item>
                        <el-form-item prop="notice" label="群公告">
                          <el-input
                            v-model="updateGroupInfo.notice"
                            type="textarea"
                            show-word-limit
                            maxlength="500"
                            :autosize="{ minRows: 3, maxRows: 3 }"
                            placeholder="选填"
                          />
                        </el-form-item>
                        <el-form-item prop="avatar" label="群头像">
                          <el-upload
                            v-model:file-list="avatarList"
                            ref="uploadAvatarRef"
                            :auto-upload="false"
                            :action="uploadAvatarPath"
                            :headers="uploadHeaders"
                            :on-success="handleAvatarUploadSuccess"
                            :before-upload="beforeAvatarUpload"
                          >
                            <template #trigger>
                              <el-button class="soft-action-btn">
                                上传图片
                              </el-button>
                            </template>
                          </el-upload>
                        </el-form-item>
                      </el-form>
                    </div>
                  </el-scrollbar>
                </template>
                <template v-slot:footer>
                  <div class="updategroupinfo-modal-footer">
                    <el-button
                      class="soft-action-btn"
                      @click="closeUpdateGroupInfoModal"
                    >
                      完成
                    </el-button>
                  </div>
                </template>
              </Modal>
              <Modal :isVisible="isRemoveGroupMemberModalVisible">
                <template v-slot:header>
                  <div class="removegroupmember-modal-quit-btn-container">
                    <button
                      class="removegroupmember-modal-quit-btn"
                      @click="quitRemoveGroupMemberModal"
                    >
                      <el-icon><Close /></el-icon>
                    </button>
                  </div>
                  <div class="removegroupmember-modal-header-title">
                    <h3>移除群组人员</h3>
                  </div>
                </template>
                <template v-slot:body>
                  <span
                    style="
                      font-size: 14px;
                      font-weight: bold;
                      font-family: Arial, Helvetica, sans-serif;
                      color: rgb(57, 57, 57);
                      width: 270px;
                      display: flex;
                      justify-content: left;
                      margin-bottom: 5px;
                    "
                    >群组成员：</span
                  >
                  <el-scrollbar
                    max-height="400px"
                    style="height: 300px; width: 350px"
                  >
                    <div class="modal-body">
                      <ul
                        style="list-style-type: none"
                        class="removegroupmembers-list"
                      >
                        <li
                          v-for="groupMember in groupMemberList"
                          :key="groupMember.user_id"
                          class="removegroupmembers-item"
                        >
                          <div style="display: flex; align-items: center">
                            <el-image
                              :src="groupMember.avatar"
                              class="removegroupmembers-item-avatar"
                            />
                            <span class="removegroupmembers-item-name">{{
                              groupMember.nickname
                            }}</span>
                          </div>
                          <input
                            type="checkbox"
                            :value="groupMember.user_id"
                            v-model="selectedGroupMembers"
                            @change="handleCheckboxChange"
                          />
                        </li>
                      </ul>
                    </div>
                  </el-scrollbar>
                </template>
                <template v-slot:footer>
                  <div
                    style="
                      height: 50px;
                      width: 300px;
                      display: flex;
                      justify-content: right;
                    "
                  >
                    <el-button
                      class="removegroupmembers-button"
                      @click="handleRemoveGroupMembers"
                      >移除所选人员</el-button
                    >
                  </div>
                </template>
              </Modal>
              <SmallModal :isVisible="isAddGroupModalVisible">
                <template v-slot:header>
                  <div class="modal-header">
                    <div class="modal-quit-btn-container">
                      <button class="modal-quit-btn" @click="quitAddGroupModal">
                        <el-icon><Close /></el-icon>
                      </button>
                    </div>
                    <div class="modal-header-title">
                      <h3>加群申请</h3>
                    </div>
                  </div>
                </template>
                <template v-slot:body>
                  <div class="addGroup-modal-body">
                    <el-scrollbar max-height="400px">
                      <ul class="addGroup-list" style="list-style-type: none">
                        <li
                          v-for="addGroup in addGroupList"
                          :key="addGroup.contact_id"
                          class="addGroup-item"
                        >
                          <div
                            style="
                              display: flex;
                              align-items: center;
                              justify-content: center;
                            "
                          >
                            <img
                              :src="addGroup.contact_avatar"
                              style="
                                width: 30px;
                                height: 30px;
                                margin-right: 10px;
                              "
                            />

                            <el-tooltip
                              effect="customized"
                              :content="addGroup.message"
                              placement="top"
                              hide-after="0"
                              enterable="false"
                            >
                              <div style="color: black">
                                {{ addGroup.contact_name }}
                              </div>
                            </el-tooltip>
                          </div>
                          <el-dropdown placement="right" trigger="click">
                            <el-button class="action-btn"> 去处理 </el-button>
                            <template #dropdown>
                              <el-dropdown-menu>
                                <el-dropdown-item
                                  @click="handleAgree(addGroup.contact_id)"
                                  >同意</el-dropdown-item
                                >
                                <el-dropdown-item
                                  @click="handleReject(addGroup.contact_id)"
                                >
                                  拒绝
                                </el-dropdown-item>
                              </el-dropdown-menu>
                            </template>
                          </el-dropdown>
                        </li>
                      </ul>
                    </el-scrollbar>
                  </div>
                </template>
              </SmallModal>
            </div>
    </el-header>
    <el-main class="main-container">
      <el-scrollbar class="message-scrollbar" ref="scrollbarRef">
              <div ref="innerRef" class="message-list">
                <div
                  v-for="(messageItem, index) in messageList"
                  :key="index"
                  class="message-item"
                >
                  <!-- 历史里的通话信令渲染为一条通话记录 -->
                  <div v-if="messageItem.type == 3" class="message-call-log">
                    {{ callLogText(messageItem) }}
                  </div>
                  <div
                    v-if="
                      messageItem.send_id != userInfo.uuid &&
                      messageItem.type == 0
                    "
                    class="left-message"
                  >
                    <div class="left-message-left">
                      <el-image :src="messageItem.send_avatar" class="message-avatar">
                      </el-image>
                    </div>

                    <div class="left-message-right">
                      <div class="left-message-right-top">
                        <div class="left-message-contactname">
                          {{ messageItem.send_name }}
                        </div>
                        <div class="left-message-time">
                          {{ messageItem.created_at }}
                        </div>
                      </div>

                      <div class="left-message-content">
                        {{ messageItem.content }}
                      </div>
                    </div>
                  </div>
                  <div
                    v-if="
                      messageItem.send_id != userInfo.uuid &&
                      messageItem.type == 2
                    "
                    class="left-message"
                  >
                    <div class="left-message-left">
                      <el-image :src="messageItem.send_avatar" class="message-avatar">
                      </el-image>
                    </div>

                    <div class="left-message-right">
                      <div class="left-message-right-top">
                        <div class="left-message-contactname">
                          {{ messageItem.send_name }}
                        </div>
                        <div class="left-message-time">
                          {{ messageItem.created_at }}
                        </div>
                      </div>

                      <div class="left-message-file-container" v-if="!isMediaMessage(messageItem)">
                        <div style="display: flex; flex-direction: row">
                          <div class="left-message-file-name">
                            {{ messageItem.file_name }}
                          </div>
                          <div class="left-message-file-size">
                            {{ messageItem.file_size }}
                          </div>
                        </div>

                        <div class="left-message-file-download">
                          <el-button
                            class="soft-action-btn"
                            size="small"
                            style="margin-top: 20px"
                            @click="downloadFile(messageItem.file_name)"
                          >
                            下载
                          </el-button>
                        </div>
                      </div>
                      <div class="left-message-media" v-else>
                        <el-image
                          v-if="isImage(messageItem)"
                          :src="messageItem.url"
                          :preview-src-list="[messageItem.url]"
                          fit="cover"
                          class="message-image"
                        />
                        <video
                          v-else-if="isVideo(messageItem)"
                          :src="messageItem.url"
                          controls
                          class="message-video"
                        ></video>
                        <audio
                          v-else-if="isAudio(messageItem)"
                          :src="messageItem.url"
                          controls
                          class="message-audio"
                        ></audio>
                        <div class="left-message-media-meta">
                          {{ messageItem.file_name }} · {{ messageItem.file_size }}
                          <el-button
                            class="soft-action-btn"
                            size="small"
                            @click="downloadFile(messageItem.file_name)"
                          >
                            下载
                          </el-button>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div class="message-self-wrap">
                    <div
                      v-if="
                        messageItem.send_id == userInfo.uuid &&
                        messageItem.type == 0
                      "
                      class="right-message"
                    >
                      <div class="right-message-right">
                        <el-image :src="userInfo.avatar" class="message-avatar">
                        </el-image>
                      </div>

                      <div class="right-message-left">
                        <div class="right-message-left-top">
                          <div class="right-message-contactname">
                            {{ userInfo.nickname }}
                          </div>
                          <div class="right-message-time">
                            {{ messageItem.created_at }}
                          </div>
                        </div>
                        <div class="message-self-content">
                          <div class="right-message-content">
                            {{ messageItem.content }}
                          </div>
                        </div>
                      </div>
                    </div>
                    <div
                      v-if="
                        messageItem.send_id == userInfo.uuid &&
                        messageItem.type == 2
                      "
                      class="right-message"
                    >
                      <div class="right-message-right">
                        <el-image :src="userInfo.avatar" class="message-avatar">
                        </el-image>
                      </div>

                      <div class="right-message-left">
                        <div class="right-message-left-top">
                          <div class="right-message-contactname">
                            {{ userInfo.nickname }}
                          </div>
                          <div class="right-message-time">
                            {{ messageItem.created_at }}
                          </div>
                        </div>
                        <div class="message-self-content">
                          <div
                            class="right-message-file-container"
                            v-if="!isMediaMessage(messageItem)"
                          >
                            <div style="display: flex; flex-direction: row">
                              <div class="right-message-file-name">
                                {{ messageItem.file_name }}
                              </div>
                              <div class="right-message-file-size">
                                {{ messageItem.file_size }}
                              </div>
                            </div>

                            <div class="right-message-file-download">
                              已发送
                            </div>
                          </div>
                          <div class="right-message-media" v-else>
                            <el-image
                              v-if="isImage(messageItem)"
                              :src="messageItem.url"
                              :preview-src-list="[messageItem.url]"
                              fit="cover"
                              class="message-image"
                            />
                            <video
                              v-else-if="isVideo(messageItem)"
                              :src="messageItem.url"
                              controls
                              class="message-video"
                            ></video>
                            <audio
                              v-else-if="isAudio(messageItem)"
                              :src="messageItem.url"
                              controls
                              class="message-audio"
                            ></audio>
                            <div class="right-message-media-meta">
                              {{ messageItem.file_name }} · {{ messageItem.file_size }}
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
      </el-scrollbar>
      <div class="tool-bar">
              <div class="tool-bar-left">
                <el-tooltip
                  effect="customized"
                  content="文件上传"
                  placement="top"
                  hide-after="0"
                  enterable="false"
                >
                  <button class="image-button">
                    <el-upload
                      v-model:file-list="fileList"
                      ref="uploadRef"
                      :auto-upload="true"
                      :show-file-list="false"
                      :action="uploadPath"
                      :headers="uploadHeaders"
                      :on-success="handleUploadSuccess"
                      :before-upload="beforeFileUpload"
                      style="
                        display: flex;
                        align-items: center;
                        justify-content: center;
                      "
                    >
                      <svg
                        t="1733503065264"
                        class="upload-icon"
                        viewBox="0 0 1024 1024"
                        version="1.1"
                        xmlns="http://www.w3.org/2000/svg"
                        p-id="2430"
                        width="128"
                        height="128"
                      >
                        <path
                          d="M543.7 157v534c0 16.6-13.4 30-30 30s-30-13.4-30-30V157c0-16.6 13.4-30 30-30 16.5 0 30 13.4 30 30z"
                          fill=""
                          p-id="2431"
                        ></path>
                        <path
                          d="M323.1 331c11.8 11.8 30.7 11.8 42.5 0l119.9-119.9c15.6-15.6 40.9-15.6 56.6 0L662 331c11.7 11.7 30.7 11.7 42.4 0s11.7-30.7 0-42.4L541.7 126.1c-15.6-15.6-41-15.6-56.6 0L323 288.6c-11.6 11.8-11.6 30.7 0.1 42.4zM853.7 913h-680c-33.1 0-60-26.9-60-60V583.7c0-16.4 12.8-30.2 29.2-30.7 16.9-0.4 30.8 13.2 30.8 30v240c0 16.6 13.4 30 30 30h620c16.6 0 30-13.4 30-30V583.7c0-16.4 12.8-30.2 29.2-30.7 16.9-0.4 30.8 13.2 30.8 30v270c0 33.1-26.9 60-60 60z"
                          fill=""
                          p-id="2432"
                        ></path>
                      </svg>
                    </el-upload>
                  </button>
                </el-tooltip>

                <el-tooltip
                  effect="customized"
                  content="全文复制"
                  placement="top"
                  hide-after="0"
                  enterable="false"
                >
                  <button class="image-button" @click="copyAllMessages">
                    <svg
                      t="1733503137487"
                      class="copy-icon"
                      viewBox="0 0 1024 1024"
                      version="1.1"
                      xmlns="http://www.w3.org/2000/svg"
                      p-id="3442"
                      width="128"
                      height="128"
                    >
                      <path
                        d="M394.666667 106.666667h448a74.666667 74.666667 0 0 1 74.666666 74.666666v448a74.666667 74.666667 0 0 1-74.666666 74.666667H394.666667a74.666667 74.666667 0 0 1-74.666667-74.666667V181.333333a74.666667 74.666667 0 0 1 74.666667-74.666666z m0 64a10.666667 10.666667 0 0 0-10.666667 10.666666v448a10.666667 10.666667 0 0 0 10.666667 10.666667h448a10.666667 10.666667 0 0 0 10.666666-10.666667V181.333333a10.666667 10.666667 0 0 0-10.666666-10.666666H394.666667z m245.333333 597.333333a32 32 0 0 1 64 0v74.666667a74.666667 74.666667 0 0 1-74.666667 74.666666H181.333333a74.666667 74.666667 0 0 1-74.666666-74.666666V394.666667a74.666667 74.666667 0 0 1 74.666666-74.666667h74.666667a32 32 0 0 1 0 64h-74.666667a10.666667 10.666667 0 0 0-10.666666 10.666667v448a10.666667 10.666667 0 0 0 10.666666 10.666666h448a10.666667 10.666667 0 0 0 10.666667-10.666666v-74.666667z"
                        fill="#000000"
                        p-id="3443"
                      ></path>
                    </svg>
                  </button>
                </el-tooltip>
              </div>
              <div class="tool-bar-right">
                <el-tooltip
                  effect="customized"
                  content="音视频通话"
                  placement="top"
                  hide-after="0"
                  enterable="false"
                >
                  <button
                    v-if="contactInfo.contact_id[0] === 'U'"
                    class="image-button"
                    @click="startCallRequest"
                  >
                    <svg
                      t="1733503700535"
                      class="av-icon"
                      viewBox="0 0 1024 1024"
                      version="1.1"
                      xmlns="http://www.w3.org/2000/svg"
                      p-id="4492"
                      width="128"
                      height="128"
                    >
                      <path
                        d="M790.207709 1023.317561c-100.48917-0.05687-302.832389-33.89448-528.321671-260.00933C-57.722981 442.903032-9.212929 154.458736 25.02277 119.995557L114.194824 30.709763c19.506387-19.563257 47.372654-30.709763 76.319449-30.709763 28.662446 0 56.073753 10.975897 75.23892 30.141064l3.980896 4.606465 131.881373 176.865489c35.145618 52.377208 33.32578 108.564701-4.720205 146.781295l-39.012773 39.069643c11.942686 71.087415 42.31123 113.398645 87.181606 158.439632l5.686993 5.686993c51.865378 52.092858 96.678885 97.076974 174.021993 103.730756l38.899033-38.955903a99.522381 99.522381 0 0 1 71.883595-30.368544c24.169721 0 49.419971 8.41675 73.020993 24.340331l178.002888 133.303121c21.212485 14.558703 34.918138 38.728424 37.477285 66.253471a113.853604 113.853604 0 0 1-33.26891 89.513274l-89.058314 89.285793c-22.179274 22.236144-85.304898 24.624681-111.465068 24.624681h-0.056869zM190.628013 88.091525a19.278907 19.278907 0 0 0-13.421304 5.402644L94.290348 176.63801c-4.549595 22.861713-44.984116 247.554815 230.607575 523.885815 202.684439 203.196268 377.50261 233.507942 463.774297 233.507942 30.652893 0 50.898589-3.753416 58.121071-5.402643l80.982784-82.006443a26.160169 26.160169 0 0 0 7.67744-18.539598l-178.457847-135.293568c-4.151505-2.786627-12.568255-7.677441-20.302566-7.677441a13.478174 13.478174 0 0 0-10.009108 3.980895l-65.969121 66.196601-18.653338-0.17061c-125.227591-1.080529-193.812729-69.950017-254.322337-130.743974l-5.686993-5.630123c-52.490947-52.661557-102.763968-117.20893-115.445963-232.199934l-2.388537-21.155614L333.826502 295.609908c8.41675-8.41675 1.990448-22.349883-4.833944-32.586471L200.750861 91.105631a17.515939 17.515939 0 0 0-10.122848-3.014106z m350.603132 312.159058c-44.131067 0-79.959125-34.235699-79.959125-76.319449V170.609797c0-42.08375 35.828057-76.376319 79.959125-76.376319h292.311452c37.136066 0 68.812618 77.968677 77.627457 111.863156 8.1324-4.606465 14.103743-8.07553 15.923581-9.269799 8.75797-5.743863 18.937687-62.670665 29.458625-62.670665a53.457736 53.457736 0 0 1 25.36399 6.426303 56.130623 56.130623 0 0 1 29.003666 49.87493v121.303566c0 21.496834-11.373986 40.775741-29.572365 50.443629a52.547817 52.547817 0 0 1-24.681551 6.141953c-10.577807 0-21.041875-56.983672-29.970454-62.955015-2.331667-1.421748-8.814839-5.118294-17.686549-10.179718-11.089637 30.368544-41.515051 105.038765-75.40953 105.038765H541.231145z m283.326003-88.944574V183.178052H550.273464v128.127957h274.283684z"
                        fill="#666666"
                        p-id="4493"
                      ></path>
                    </svg>
                  </button>
                </el-tooltip>
              </div>
      </div>
    </el-main>
    <el-footer>
            <div class="chat-input">
              <el-input
                v-model="chatMessage"
                type="textarea"
                show-word-limit
                maxlength="500"
                :autosize="{ minRows: 5, maxRows: 6 }"
                placeholder="输入消息，按 Enter 发送，Shift + Enter 换行"
                @keydown.enter.exact.prevent="sendMessage"
              />
            </div>
            <div class="chat-send">
              <el-button
                class="send-btn"
                :disabled="!canSendMessage"
                @click="sendMessage"
              >
                发送
              </el-button>
            </div>
    </el-footer>
  </div>
</template>

<script>
  import { computed, reactive, toRefs, onMounted, onBeforeUnmount, ref, nextTick } from "vue";
  import { useRouter, onBeforeRouteUpdate } from "vue-router";
  import { useStore } from "vuex";
  import axios from "axios";
  import Modal from "@/components/Modal.vue";
  import SmallModal from "@/components/SmallModal.vue";
  import { ElMessage, ElMessageBox } from "element-plus";
  import { on, emit, sessionKeyOf } from "@/utils/messageBus";
  import { sendChatMessage } from "@/utils/ws";
  import { uploadFile } from "@/utils/upload";
export default {
  name: "ContactChat",
  components: {
    Modal,
    SmallModal,
  },

  setup() {
    const router = useRouter();
    const store = useStore();
    // 消息滚动容器引用（模板 ref 绑定，见 ref="scrollbarRef" / ref="innerRef"）
    const scrollbarRef = ref(null);
    const innerRef = ref(null);
    const data = reactive({
      chatMessage: "",
      chatName: "",
      userInfo: store.state.userInfo,
      isUserContactInfoModalVisible: false,
      isGroupContactInfoModalVisible: false,
      isAddGroupModalVisible: false,
      isUpdateGroupInfoModalVisible: false,
      isRemoveGroupMemberModalVisible: false,
      getUserListReq: {
        owner_id: "",
      },
      contactUserList: [],
      loadMyGroupReq: {
        owner_id: "",
      },
      myGroupList: [],
      loadMyJoinedGroupReq: {
        owner_id: "",
      },
      myJoinedGroupList: [],
      getContactInfoReq: {
        contact_id: "",
      },
      contactInfo: {
        contact_id: "",
        contact_name: "",
        contact_avatar: "",
        contact_phone: "",
        contact_email: "",
        contact_gender: null,
        contact_signature: "",
        contact_birthday: "",
        contact_notice: "",
        contact_members: [],
        contact_member_cnt: 0,
        contact_owner_id: "",
        contact_add_mode: null,
      },
      ownListReq: {
        owner_id: "",
      },
      sessionId: "",
      messageList: [],
      addGroupList: [],
      uploadRef: null,
      uploadPath: store.state.apiUrl + "/message/uploadFile",
      fileList: [],
      uploadAvatarRef: null,
      uploadAvatarPath: store.state.apiUrl + "/message/uploadAvatar",
      avatarList: [],
      backendUrl: store.state.backendUrl,
      updateGroupInfo: {
        uuid: "",
        avatar: "",
        add_mode: -1,
        name: "",
        notice: "",
      },
      groupMemberList: [],
      selectedGroupMembers: [],
      removeGroupMembersList: [],
    });

    // el-upload 不走 axios 拦截器，需要手动带上 Bearer Token（后端上传接口有鉴权）
    const uploadHeaders = computed(() => ({
      Authorization: "Bearer " + store.state.accessToken,
    }));

    const isGroupChat = computed(
      () => data.contactInfo.contact_id && data.contactInfo.contact_id[0] === "G"
    );

    const chatStatusText = computed(() => {
      if (!data.contactInfo.contact_id) {
        return "会话详情";
      }
      return isGroupChat.value
        ? `${data.contactInfo.contact_member_cnt || 0} 位成员`
        : "单人会话";
    });

    const chatTitleMeta = computed(() => {
      if (!data.contactInfo.contact_id) {
        return "打开一个会话开始聊天";
      }
      if (isGroupChat.value) {
        return data.contactInfo.contact_notice || "点击右上角查看群聊详情";
      }
      return data.contactInfo.contact_signature || "点击右上角查看联系人资料";
    });

    const canSendMessage = computed(() => data.chatMessage.trim().length > 0);

    // 当前会话消息过滤：chat-message 事件由 messageBus 全局分发，
    // 这里只把属于当前会话的消息追加进列表（其他会话的由侧栏未读处理）。
    const handleIncomingMessage = (message) => {
      const sessionKey = sessionKeyOf(message, data.userInfo.uuid);
      if (!sessionKey || sessionKey !== data.contactInfo.contact_id) {
        return;
      }
      // type=3 通话信令已由 CallOverlay 处理，不进消息列表
      if (message.type == 3) {
        return;
      }
      data.messageList.push(message);
      scrollToBottom();
    };

    // 断线重连成功后的补偿：重拉当前会话全量历史，补齐断线期间可能丢的消息
    const handleReconnected = () => {
      if (!data.contactInfo.contact_id) {
        return;
      }
      if (data.contactInfo.contact_id[0] == "U") {
        getMessageList();
      } else {
        getGroupMessageList();
      }
    };

    // 会话激活（首次进入 / :id 变化时复用）：拉资料、开会话、拉历史、清未读
    const activate = async (routeId) => {
      await getChatContactInfo(routeId);
      await getSessionId(routeId);
      store.commit("setCurrentChatId", data.contactInfo.contact_id);
      store.commit("clearUnread", data.contactInfo.contact_id);
      if (data.contactInfo.contact_id[0] == "U") {
        await getMessageList();
      } else {
        await getGroupMessageList();
      }
      // openSession 可能新建了会话，通知侧栏刷新
      emit("session-list-changed");
      scrollToBottom();
    };

    //这是/chat/:id 的id改变时会调用（同组件复用，切换聊天对象）
    onBeforeRouteUpdate(async (to, from, next) => {
      try {
        await activate(to.params.id);
      } catch (error) {
        console.error(error);
      }
      next();
    });
    // 这是刚渲染/chat/:id页面的时候会调用
    onMounted(async () => {
      try {
        await activate(router.currentRoute.value.params.id);
      } catch (error) {
        console.error(error);
      }
    });

    const offChatMessage = on("chat-message", handleIncomingMessage);
    const offWsConnected = on("ws:connected", handleReconnected);
    onBeforeUnmount(() => {
      offChatMessage();
      offWsConnected();
      store.commit("setCurrentChatId", "");
    });
    const getChatContactInfo = async (id) => {
      try {
        data.getContactInfoReq.contact_id = id;
        const rsp = await axios.post(
          store.state.apiUrl + "/contact/getContactInfo",
          data.getContactInfoReq
        );
        if (!rsp.data.data.contact_avatar.startsWith("http")) {
          rsp.data.data.contact_avatar =
            store.state.backendUrl + rsp.data.data.contact_avatar;
        }
        data.contactInfo = rsp.data.data;
        console.log(data.contactInfo);
      } catch (error) {
        console.log(error);
      }
    };
    const getSessionId = async (contactId) => {
      try {
        const req = {
          send_id: data.userInfo.uuid,
          receive_id: contactId,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/session/openSession",
          req
        );
        data.sessionId = rsp.data.data;
        console.log(rsp);
      } catch (error) {
        console.error(error);
      }
    };

    const showUserContactInfoModal = () => {
      data.isUserContactInfoModalVisible = true;
    };
    const quitUserContactInfoModal = () => {
      data.isUserContactInfoModalVisible = false;
    };
    const showGroupContactInfoModal = () => {
      data.isGroupContactInfoModalVisible = true;
    };
    const showUpdateGroupInfoModal = () => {
      data.isUpdateGroupInfoModalVisible = true;
    };
    const quitUpdateGroupInfoModal = () => {
      data.isUpdateGroupInfoModalVisible = false;
    };
    const quitGroupContactInfoModal = () => {
      data.isGroupContactInfoModalVisible = false;
    };
    const showAddGroupModal = () => {
      handleAddGroupList();
    };
    const quitAddGroupModal = () => {
      data.isAddGroupModalVisible = false;
    };
    const handleAddGroupList = async () => {
      try {
        const req = {
          group_id: data.contactInfo.contact_id,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/contact/getAddGroupList",
          req
        );
        if (rsp.data.code == 0) {
          data.addGroupList = rsp.data.data;
          console.log(rsp.data.data);
          if (data.addGroupList == null) {
            ElMessage.warning("没有新的加群申请");
            return;
          } else {
            for (let i = 0; i < data.addGroupList.length; i++) {
              if (!data.addGroupList[i].contact_avatar.startsWith("http")) {
                data.addGroupList[i].contact_avatar =
                  store.state.backendUrl + data.addGroupList[i].contact_avatar;
              }
            }
            data.isAddGroupModalVisible = true;
            console.log(rsp);
          }
        }
      } catch (error) {
        console.log(error);
      }
    };
    const preToDeleteSession = () => {
      try {
        ElMessageBox.confirm("确认删除该会话以及其聊天记录？", "Warning", {
          confirmButtonText: "确认",
          cancelButtonText: "取消",
          type: "warning",
        })
          .then(() => {
            deleteSession();
            ElMessage({
              type: "success",
              message: "成功删除",
            });
          })
          .catch(() => {
            ElMessage({
              type: "info",
              message: "取消删除",
            });
          });
      } catch (error) {
        console.error(error);
      }
    };
    const deleteSession = async () => {
      try {
        const req = {
          owner_id: data.userInfo.uuid,
          session_id: data.sessionId,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/session/deleteSession",
          req
        );
        if (rsp.data && rsp.data.code !== 0) {
          ElMessage.error(
            (rsp.data && rsp.data.message) || "删除会话失败，请重试"
          );
          return;
        }
        ElMessage.success("会话已删除");
      } catch (error) {
        ElMessage.error("删除会话失败，请重试");
        console.error(error);
        return;
      }
      emit("session-list-changed");
      router.push("/chat/sessionlist");
    };
    const preToDeleteContact = () => {
      try {
        ElMessageBox.confirm("确认删除该联系人？", "Warning", {
          confirmButtonText: "确认",
          cancelButtonText: "取消",
          type: "warning",
        })
          .then(() => {
            deleteContact();
          })
          .catch(() => {
            ElMessage({
              type: "info",
              message: "取消删除",
            });
          });
      } catch (error) {
        console.error(error);
      }
    };
    const deleteContact = async () => {
      try {
        const req = {
          owner_id: data.userInfo.uuid,
          contact_id: data.contactInfo.contact_id,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/contact/deleteContact",
          req
        );
        if (rsp.data && rsp.data.code !== 0) {
          ElMessage.error(
            (rsp.data && rsp.data.message) || "删除联系人失败，请重试"
          );
          return;
        }
        ElMessage.success("联系人已删除");
      } catch (error) {
        ElMessage.error("删除联系人失败，请重试");
        console.error(error);
        return;
      }
      emit("session-list-changed");
      router.push("/chat/sessionlist");
    };
    const preToBlackContact = () => {
      try {
        ElMessageBox.confirm("确认拉黑该联系人？", "Warning", {
          confirmButtonText: "确认",
          cancelButtonText: "取消",
          type: "warning",
        })
          .then(() => {
            blackContact();
          })
          .catch(() => {
            ElMessage({
              type: "info",
              message: "取消拉黑",
            });
          });
      } catch (error) {
        console.error(error);
      }
    };
    const blackContact = async () => {
      try {
        const req = {
          owner_id: data.userInfo.uuid,
          contact_id: data.contactInfo.contact_id,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/contact/blackContact",
          req
        );
        if (rsp.data && rsp.data.code !== 0) {
          ElMessage.error(
            (rsp.data && rsp.data.message) || "拉黑失败，请重试"
          );
          return;
        }
        ElMessage.success("已拉黑该联系人");
      } catch (error) {
        ElMessage.error("拉黑失败，请重试");
        console.error(error);
        return;
      }
      emit("session-list-changed");
      router.push("/chat/sessionlist");
    };
    let lastSendAt = 0;
    const sendMessage = () => {
      const content = data.chatMessage.trim();
      if (!content) {
        return;
      }
      // 防重复：500ms 内的连按（Enter 连击/双击发送按钮）直接忽略，避免一次操作发出多条
      const now = Date.now();
      if (now - lastSendAt < 500) {
        return;
      }
      lastSendAt = now;
      const chatMessageRequest = {
        session_id: data.sessionId,
        type: 0,
        content,
        url: "",
        send_id: data.userInfo.uuid,
        send_name: data.userInfo.nickname,
        send_avatar: data.userInfo.avatar,
        receive_id: data.contactInfo.contact_id,
        file_size: getFileSize(0),
        file_name: "",
        file_type: "",
      };
      // 连接断开时消息进入待发队列，重连后自动补发
      if (!sendChatMessage(store, chatMessageRequest)) {
        ElMessage.info("当前连接断开，消息将在重连后自动发送");
      }
      data.chatMessage = "";
      scrollToBottom();
    };

    const sendFileMessage = async (fileUrl) => {
      const chatFileMessageRequest = {
        session_id: data.sessionId,
        type: 2,
        content: "",
        url: fileUrl,
        send_id: data.userInfo.uuid,
        send_name: data.userInfo.nickname,
        send_avatar: data.userInfo.avatar,
        receive_id: data.contactInfo.contact_id,
        file_size: getFileSize(data.fileList[0].size),
        file_name: data.fileList[0].name,
        file_type: data.fileList[0].type,
      };
      console.log(chatFileMessageRequest);
      if (!sendChatMessage(store, chatFileMessageRequest)) {
        ElMessage.info("当前连接断开，文件消息将在重连后自动发送");
      }
      scrollToBottom();
    };

    const getMessageList = async () => {
      try {
        console.log(data.contactInfo);
        const req = {
          user_one_id: data.userInfo.uuid,
          user_two_id: data.contactInfo.contact_id,
        };
        console.log(req);
        const rsp = await axios.post(
          store.state.apiUrl + "/message/getMessageList",
          req
        );
        if (rsp.data.data) {
          for (let i = 0; i < rsp.data.data.length; i++) {
            if (!rsp.data.data[i].send_avatar.startsWith("http")) {
              rsp.data.data[i].send_avatar =
                store.state.backendUrl + rsp.data.data[i].send_avatar;
            }
          }
        }
        data.messageList = rsp.data.data;
        console.log(data.messageList);
        console.log(rsp);
      } catch (error) {
        console.error(error);
      }
    };

    const getGroupMessageList = async () => {
      try {
        console.log(data.contactInfo);
        const req = {
          group_id: data.contactInfo.contact_id,
        };
        console.log(req);
        const rsp = await axios.post(
          store.state.apiUrl + "/message/getGroupMessageList",
          req
        );
        if (rsp.data.data) {
          for (let i = 0; i < rsp.data.data.length; i++) {
            if (!rsp.data.data[i].send_avatar.startsWith("http")) {
              rsp.data.data[i].send_avatar =
                store.state.backendUrl + rsp.data.data[i].send_avatar;
            }
          }
        }
        data.messageList = rsp.data.data;
        console.log(rsp);
      } catch (error) {
        console.error(error);
      }
    };

    const scrollToBottom = () => {
      nextTick(() => {
        const scrollbar = scrollbarRef.value;
        const listEl = innerRef.value;
        if (!scrollbar || !listEl) {
          return;
        }
        const scrollHeight = listEl.scrollHeight;
        console.log("滚动到底部:", scrollHeight);
        scrollbar.setScrollTop(scrollHeight);
      });
    };

    const handleAgree = async (contactId) => {
      try {
        const req = {
          owner_id: data.contactInfo.contact_id,
          contact_id: contactId,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/contact/passContactApply",
          req
        );
        console.log(rsp);
        if (rsp.data.code == 0) {
          ElMessage.success(rsp.data.message);
          data.addGroupList = data.addGroupList.filter(
            (c) => c.contact_id !== contactId
          );
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
          owner_id: data.contactInfo.contact_id,
          contact_id: contactId,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/contact/refuseContactApply",
          req
        );
        console.log(rsp);
        if (rsp.data.code == 0) {
          ElMessage.success(rsp.data.message);
          data.addGroupList = data.addGroupList.filter(
            (c) => c.contact_id !== contactId
          );
        } else {
          ElMessage.error(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };

    const handleLeaveGroup = async () => {
      try {
        const req = {
          user_id: data.userInfo.uuid,
          group_id: data.contactInfo.contact_id,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/group/leaveGroup",
          req
        );
        if (rsp.data.code == 0) {
          ElMessage.success(rsp.data.message);
          console.log(rsp.data.message);
          emit("session-list-changed");
          router.push("/chat/sessionlist");
        } else if (rsp.data.code == 40000) {
          ElMessage.warning(rsp.data.message);
          console.log(rsp.data.message);
        } else {
          ElMessage.error(rsp.data.message);
          console.error(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };

    const handleDismissGroup = async () => {
      try {
        const req = {
          owner_id: data.userInfo.uuid,
          group_id: data.contactInfo.contact_id,
        };
        const rsp = await axios.post(
          store.state.apiUrl + "/group/dismissGroup",
          req
        );
        if (rsp.data.code == 0) {
          ElMessage.success(rsp.data.message);
          console.log(rsp.data.message);
          emit("session-list-changed");
          router.push("/chat/sessionlist");
        } else if (rsp.data.code == 40000) {
          ElMessage.warning(rsp.data.message);
          console.log(rsp.data.message);
        } else {
          ElMessage.error(rsp.data.message);
          console.error(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };

    const handleUploadSuccess = (response) => {
      // response.data 是后端落盘后的相对路径（如 /static/files/<新文件名>）
      const savedPath = response && response.data;
      if (!savedPath) {
        ElMessage.error("文件上传失败，请重试");
        data.fileList = [];
        return;
      }
      ElMessage.success("文件上传成功");
      sendFileMessage(store.state.backendUrl + savedPath);
      data.fileList = [];
    };

    const handleAvatarUploadSuccess = () => {
      ElMessage.success("头像上传成功");
      data.avatarList = [];
    };

    const beforeAvatarUpload = (avatar) => {
      console.log("上传前avatar====>", avatar);
      console.log(data.avatarList);
      console.log(avatar);
      if (data.avatarList.length > 1) {
        ElMessage.error("只能上传一张头像");
        return false;
      }
      const isLt50M = avatar.size / 1024 / 1024 < 50;
      if (!isLt50M) {
        ElMessage.error("上传头像图片大小不能超过 50MB!");
        return false;
      }
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
    const downloadFile = async (fileName) => {
      try {
        const rsp = await axios.get(
          store.state.backendUrl + "/static/files/" + fileName,
          {
            responseType: "blob",
          }
        );
        console.log(rsp);
        const blob = new Blob([rsp.data], {
          type: rsp.headers["content-type"] || "application/octet-stream",
        });
        const link = document.createElement("a");
        link.href = window.URL.createObjectURL(blob);
        link.download = fileName;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
      } catch (error) {
        console.error(error);
      }
    };
    const getFileSize = (size) => {
      if (size < 1024) {
        return size + "B";
      } else if (size < 1024 * 1024) {
        return (size / 1024).toFixed(2) + "KB";
      } else if (size < 1024 * 1024 * 1024) {
        return (size / 1024 / 1024).toFixed(2) + "MB";
      } else {
        return (size / 1024 / 1024 / 1024).toFixed(2) + "GB";
      }
    };

    // ---------- 消息类型辅助（媒体内联渲染 / 通话记录行） ----------

    const isImage = (message) =>
      message.type == 2 && (message.file_type || "").startsWith("image/");
    const isVideo = (message) =>
      message.type == 2 && (message.file_type || "").startsWith("video/");
    const isAudio = (message) =>
      message.type == 2 && (message.file_type || "").startsWith("audio/");
    const isMediaMessage = (message) =>
      isImage(message) || isVideo(message) || isAudio(message);

    // 历史里的 type=3 信令（start/receive/reject 落库）渲染为一条通话记录
    const callLogText = (message) => {
      let av = {};
      try {
        av = JSON.parse(message.av_data) || {};
      } catch (e) {
        // ignore
      }
      const who = message.send_name || "";
      if (av.type === "start_call") {
        return `${who} 发起了音视频通话`;
      }
      if (av.type === "receive_call") {
        return `${who} 已接听`;
      }
      if (av.type === "reject_call") {
        return `${who} 拒绝了通话`;
      }
      return "通话记录";
    };

    const copyAllMessages = async () => {
      const lines = (data.messageList || [])
        .filter((m) => m.type != 3)
        .map(
          (m) =>
            `[${m.created_at}] ${m.send_name}: ${
              m.type == 2 ? "[文件] " + (m.file_name || "") : m.content
            }`
        );
      if (!lines.length) {
        ElMessage.warning("当前会话没有可复制的消息");
        return;
      }
      try {
        await navigator.clipboard.writeText(lines.join("\n"));
        ElMessage.success("已复制当前会话全部消息");
      } catch (e) {
        ElMessage.error("复制失败，浏览器未授权剪贴板权限");
      }
    };

    // 发起音视频通话：交给全局 CallOverlay 处理（WebRTC 逻辑不再放在聊天窗口内）
    const startCallRequest = () => {
      if (!data.contactInfo.contact_id || data.contactInfo.contact_id[0] !== "U") {
        ElMessage.warning("音视频通话目前仅支持单聊");
        return;
      }
      emit("call:start", {
        peer: {
          id: data.contactInfo.contact_id,
          name: data.contactInfo.contact_name,
          avatar: data.contactInfo.contact_avatar,
          sessionId: data.sessionId,
        },
      });
    };

    const handleUpdateGroupInfo = async () => {
      try {
        if (
          data.updateGroupInfo.name == "" &&
          data.updateGroupInfo.notice == "" &&
          data.updateGroupInfo.add_mode == -1 &&
          data.avatarList.length == 0
        ) {
          ElMessage.error("请至少修改一项");
          return;
        }
        if (data.avatarList.length > 0) {
          // 先上传拿后端真实路径，再提交更新（避免 submit 未完成即发请求、路径靠猜）
          try {
            data.updateGroupInfo.avatar = await uploadFile(
              "/message/uploadAvatar",
              data.avatarList[0].raw
            );
          } catch (uploadError) {
            ElMessage.error("群头像上传失败，请重试");
            console.error(uploadError);
            return;
          }
        }
        data.updateGroupInfo.uuid = data.contactInfo.contact_id;
        const rsp = await axios.post(
          store.state.apiUrl + "/group/updateGroupInfo",
          data.updateGroupInfo
        );
        if (rsp.data.code == 0) {
          ElMessage.success(rsp.data.message);
          data.isUpdateGroupInfoModalVisible = false;
          data.avatarList = [];
          await getChatContactInfo(router.currentRoute.value.params.id);
        } else {
          ElMessage.error(rsp.data.message);
          console.log(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };

    const closeUpdateGroupInfoModal = () => {
      handleUpdateGroupInfo();
    };
    const getGroupMemberList = async () => {
      const req = {
        group_id: data.contactInfo.contact_id,
      };
      try {
        const rsp = await axios.post(
          store.state.apiUrl + "/group/getGroupMemberList",
          req
        );
        console.log(rsp);
        if (rsp.data.code == 0) {
          for (let i = 0; i < rsp.data.data.length; i++) {
            if (!rsp.data.data[i].avatar.startsWith("http")) {
              rsp.data.data[i].avatar =
                store.state.backendUrl + rsp.data.data[i].avatar;
            }
          }
          data.groupMemberList = rsp.data.data;
          console.log(data.groupMemberList);
        } else {
          ElMessage.error(rsp.data.message);
          console.log(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };
    const showRemoveGroupMemberModal = async () => {
      await getGroupMemberList();
      data.isRemoveGroupMemberModalVisible = true;
    };

    const quitRemoveGroupMemberModal = () => {
      data.isRemoveGroupMemberModalVisible = false;
    };

    const closeRemoveGroupMemberModal = () => {};

    const handleCheckboxChange = () => {
      data.removeGroupMembersList = data.selectedGroupMembers;
      console.log(data.removeGroupMembersList);
    };

    const handleRemoveGroupMembers = async () => {
      const req = {
        group_id: data.contactInfo.contact_id,
        owner_id: data.contactInfo.contact_owner_id,
        uuid_list: data.removeGroupMembersList,
      };
      console.log(data.contactInfo);
      try {
        const rsp = await axios.post(
          store.state.apiUrl + "/group/removeGroupMembers",
          req
        );
        console.log(rsp);
        if (rsp.data.code == 0) {
          ElMessage.success(rsp.data.message);
          getGroupMemberList();
        } else if (rsp.data.code == 40000) {
          ElMessage.warning(rsp.data.message);
        } else {
          ElMessage.error(rsp.data.message);
        }
      } catch (error) {
        console.error(error);
      }
    };
    return {
      ...toRefs(data),
      scrollbarRef,
      innerRef,
      store,
      uploadHeaders,
      chatStatusText,
      chatTitleMeta,
      canSendMessage,
      router,
      showUserContactInfoModal,
      quitUserContactInfoModal,
      showGroupContactInfoModal,
      quitGroupContactInfoModal,
      showAddGroupModal,
      quitAddGroupModal,
      deleteSession,
      preToDeleteSession,
      preToDeleteContact,
      preToBlackContact,
      sendMessage,
      getMessageList,
      getGroupMessageList,
      handleAgree,
      handleReject,
      handleAddGroupList,
      handleLeaveGroup,
      handleDismissGroup,
      handleUploadSuccess,
      beforeFileUpload,
      downloadFile,
      getFileSize,
      isImage,
      isVideo,
      isAudio,
      isMediaMessage,
      callLogText,
      copyAllMessages,
      startCallRequest,
      showUpdateGroupInfoModal,
      quitUpdateGroupInfoModal,
      beforeAvatarUpload,
      handleAvatarUploadSuccess,
      handleUpdateGroupInfo,
      closeUpdateGroupInfoModal,
      showRemoveGroupMemberModal,
      quitRemoveGroupMemberModal,
      closeRemoveGroupMemberModal,
      getGroupMemberList,
      handleCheckboxChange,
      handleRemoveGroupMembers,
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

h3 {
  margin: 0;
  color: var(--go-text-strong);
  font-size: 16px;
  font-weight: 600;
}

.groupcontactinfo-modal-quit-btn-container,
.userinfo-modal-quit-btn-container,
.updategroupinfo-modal-quit-btn-container,
.removegroupmember-modal-quit-btn-container,
.modal-quit-btn-container {
  width: 100%;
  display: flex;
  justify-content: flex-end;
}

.groupcontactinfo-modal-quit-btn,
.userinfo-modal-quit-btn,
.updategroupinfo-modal-quit-btn,
.removegroupmember-modal-quit-btn,
.modal-quit-btn {
  background: transparent;
  color: #666;
  padding: 10px;
  border: none;
  cursor: pointer;
}

.groupcontactinfo-modal-header-title,
.userinfo-modal-header-title,
.removegroupmember-modal-header-title,
.updategroupinfo-modal-header-title,
.modal-header-title {
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
}

.chat-title {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.chat-title-copy {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.chat-title-subtitle {
  margin: 0;
  color: var(--go-text-muted);
  font-size: 12px;
  line-height: 1.5;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.chat-title-right {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 10px;
  min-width: 0;
  color: var(--go-text-muted);
}

.chat-status-pill {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 0 12px;
  border-radius: 999px;
  background: rgba(7, 193, 96, 0.08);
  color: var(--go-accent-strong);
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.chat-title-avatar {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  object-fit: cover;
  box-shadow: 0 10px 18px rgba(16, 22, 18, 0.08);
}

.contactlist-avatar,
.removegroupmembers-item-avatar {
  width: 32px;
  height: 32px;
  margin-right: 12px;
  border-radius: 10px;
  object-fit: cover;
}

.setting-btn {
  width: 36px;
  height: 36px;
  border: 1px solid transparent;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.74);
  color: var(--go-text-muted);
  cursor: pointer;
  transition:
    background-color 0.22s ease,
    border-color 0.22s ease,
    color 0.22s ease,
    transform 0.22s ease;
}

.setting-btn:hover {
  background: #fff;
  border-color: var(--go-border);
  color: var(--go-text-strong);
  transform: translateY(-1px);
}

.modal-list {
  width: 88%;
  margin-top: 4px;
}

.message-scrollbar {
  flex: 1;
  min-height: 0;
  background:
    linear-gradient(180deg, rgba(240, 245, 240, 0.82) 0%, rgba(250, 252, 250, 0.96) 22%, rgba(255, 255, 255, 0.98) 100%);
}

.message-scrollbar :deep(.el-scrollbar__wrap) {
  padding: 24px 26px 18px;
}

.message-scrollbar :deep(.el-scrollbar__view) {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.message-list,
.message-item {
  width: 100%;
}

.message-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.message-self-wrap {
  width: 100%;
  display: flex;
  justify-content: flex-end;
}

.message-self-content {
  display: flex;
  flex-direction: row-reverse;
}

.message-avatar {
  width: 42px;
  height: 42px;
  margin: 8px 12px 0;
  border-radius: 12px;
  object-fit: cover;
}

.left-message,
.right-message {
  width: min(80%, 580px);
  display: flex;
  gap: 12px;
}

.left-message {
  align-items: flex-start;
}

.right-message {
  flex-direction: row-reverse;
  align-items: flex-start;
}

.left-message-left,
.right-message-right {
  display: flex;
  flex-shrink: 0;
}

.left-message-right,
.right-message-left {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.left-message-right-top,
.right-message-left-top {
  display: flex;
  align-items: center;
  gap: 10px;
}

.right-message-left-top {
  justify-content: flex-end;
}

.left-message-contactname,
.right-message-contactname {
  color: #59665e;
  font-size: 12px;
  font-weight: 600;
}

.left-message-time,
.right-message-time {
  color: #91a097;
  font-size: 12px;
}

.left-message-content,
.right-message-content {
  max-width: 420px;
  padding: 12px 14px;
  border-radius: 18px;
  border: 1px solid rgba(217, 226, 219, 0.9);
  color: var(--go-text);
  font-size: 14px;
  line-height: 1.7;
  word-break: break-word;
  background: rgba(255, 255, 255, 0.94);
  box-shadow: 0 12px 22px rgba(26, 47, 34, 0.05);
}

.left-message-content {
  border-bottom-left-radius: 8px;
}

.right-message-content {
  border-bottom-right-radius: 8px;
  background: linear-gradient(180deg, #b4f28a 0%, #a6ec7d 100%);
  border-color: var(--go-bubble-self-border);
}

.left-message-file-container,
.right-message-file-container {
  width: min(300px, 100%);
  min-height: 96px;
  padding: 14px;
  border-radius: 18px;
  border: 1px solid rgba(217, 226, 219, 0.9);
  background: rgba(255, 255, 255, 0.94);
  box-shadow: 0 12px 22px rgba(26, 47, 34, 0.05);
}

.right-message-file-container {
  background: var(--go-bubble-self-soft);
  border-color: var(--go-bubble-self-border);
}

.left-message-file-name,
.right-message-file-name {
  color: var(--go-text-strong);
  font-size: 14px;
  font-weight: 500;
  word-break: break-all;
}

.left-message-file-size,
.right-message-file-size,
.right-message-file-download {
  color: var(--go-text-muted);
  font-size: 12px;
}

.left-message-file-download {
  margin-top: 14px;
}

.modal-header {
  width: 100%;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
}

.modal-body,
.addGroup-modal-body {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.newcontact-modal-footer,
.updategroupinfo-modal-footer,
.modal-footer {
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
}

.addGroup-list,
.removegroupmembers-list {
  width: 100%;
  max-width: 300px;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
}

.addGroup-item,
.removegroupmembers-item {
  width: 100%;
  min-height: 48px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 0;
  border-bottom: 1px solid #f1f1f1;
}

.addGroup-item:last-child,
.removegroupmembers-item:last-child {
  border-bottom: none;
}

.removegroupmembers-item-name {
  color: var(--go-text-strong);
  font-size: 14px;
  font-weight: 500;
}

.action-btn.el-button,
.removegroupmembers-button.el-button {
  border: 1px solid var(--go-border);
  background: #f4f7f5;
  color: var(--go-text);
  box-shadow: none;
}

.action-btn.el-button:hover,
.removegroupmembers-button.el-button:hover {
  background: #ebebeb;
  color: var(--go-text);
}

.conn-state-pill {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 0 12px;
  border-radius: 999px;
  background: rgba(245, 108, 108, 0.1);
  color: #e6666c;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.message-call-log {
  width: 100%;
  padding: 4px 0;
  text-align: center;
  color: var(--go-text-muted);
  font-size: 12px;
  background: rgba(240, 244, 241, 0.7);
  border-radius: 10px;
}

.left-message-media,
.right-message-media {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-width: 320px;
}

.message-image {
  width: 240px;
  height: 180px;
  border-radius: 14px;
  border: 1px solid rgba(217, 226, 219, 0.9);
  background: rgba(255, 255, 255, 0.94);
  box-shadow: 0 12px 22px rgba(26, 47, 34, 0.05);
  cursor: zoom-in;
}

.message-video {
  width: 300px;
  max-width: 100%;
  border-radius: 14px;
  border: 1px solid rgba(217, 226, 219, 0.9);
  background: #111;
}

.message-audio {
  width: 260px;
  max-width: 100%;
}

.left-message-media-meta,
.right-message-media-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--go-text-muted);
  font-size: 12px;
  word-break: break-all;
}

@media (max-width: 900px) {
  .chat-title-right {
    margin-left: auto;
  }

  .left-message,
  .right-message {
    width: min(88%, 620px);
  }

  .message-image {
    width: 200px;
    height: 150px;
  }

  .message-video {
    width: 240px;
  }
}
</style>
