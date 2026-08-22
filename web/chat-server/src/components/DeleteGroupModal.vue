<template>
  <div class="manager-table-pane" v-if="isVisible">
    <div class="manager-table">
      <el-table
        :data="deleteGroupTableData"
        height="100%"
        @selection-change="selectGroups"
      >
        <el-table-column type="selection" width="46" />
        <el-table-column label="群聊" min-width="230">
          <template #default="scope">
            <div class="manager-cell-user">
              <el-avatar :size="34" shape="square" :src="avatarOf(scope.row)" />
              <div class="manager-cell-user__meta">
                <span class="manager-cell-user__name">{{ scope.row.name }}</span>
                <span class="manager-cell-user__id">{{ scope.row.uuid }}</span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="owner_id" label="群主 ID" width="180" />
        <el-table-column label="状态" width="100" align="center">
          <template #default="scope">
            <el-tag v-if="scope.row.is_deleted == true" type="danger" effect="light" round size="small">已删除</el-tag>
            <el-tag v-else-if="scope.row.status == 1" type="warning" effect="light" round size="small">已禁用</el-tag>
            <el-tag v-else type="success" effect="light" round size="small">正常</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div class="manager-table-bar">
      <span class="manager-table-bar__count">已选 {{ uuidList.length }} 项,解散后不可恢复</span>
      <el-button class="soft-action-btn soft-action-btn--danger" @click="deleteGroups">删除 / 解散选中</el-button>
    </div>
  </div>
</template>

<script>
import { onMounted, reactive, toRefs } from 'vue';
import { useStore } from 'vuex';
import axios from 'axios';
import { ElMessage, ElMessageBox } from "element-plus";
export default {
  name: "DeleteGroupModal",
  props: {
    isVisible: false,
  },
  setup() {
    const store = useStore();
    const data = reactive({
      deleteGroupTableData: [],
      uuidList: [],
    });

    onMounted(() => {
      getGroupInfoList();
    });

    const getGroupInfoList = async () => {
      try {
        const rsp = await axios.post(
          store.state.apiUrl + "/admin/getGroupInfoList"
        );
        data.deleteGroupTableData = rsp.data.data;
      } catch (error) {
        ElMessage.error("群聊列表加载失败");
      }
    };

    const avatarOf = (row) => {
      const avatar = row.avatar || "";
      return avatar && !avatar.startsWith("http")
        ? store.state.backendUrl + avatar
        : avatar;
    };

    const selectGroups = (val) => {
      data.uuidList = val.map((item) => item.uuid);
    };

    const deleteGroups = async () => {
      if (data.uuidList.length === 0) {
        ElMessage.warning("请先勾选要解散的群聊");
        return;
      }
      try {
        await ElMessageBox.confirm(
          `确定解散选中的 ${data.uuidList.length} 个群聊?解散后不可恢复。`,
          "解散群聊",
          { confirmButtonText: "解散", cancelButtonText: "取消", type: "warning" }
        );
      } catch (e) {
        return;
      }
      try {
        const req = {
          uuid_list: data.uuidList,
        };
        await axios.post(store.state.apiUrl + "/admin/deleteGroups", req);
        ElMessage.success("已解散选中群聊");
        getGroupInfoList();
      } catch (error) {
        ElMessage.error("操作失败,请重试");
      }
    };

    return {
      ...toRefs(data),
      getGroupInfoList,
      avatarOf,
      selectGroups,
      deleteGroups,
    }
  }
};
</script>
