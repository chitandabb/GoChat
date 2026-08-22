<template>
  <div class="manager-table-pane" v-if="isVisible">
    <div class="manager-table">
      <el-table
        :data="disableGroupTableData"
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
      <span class="manager-table-bar__count">已选 {{ uuidList.length }} 项</span>
      <el-button class="soft-action-btn" @click="setGroupsStatus(0)">启用选中</el-button>
      <el-button class="soft-action-btn soft-action-btn--danger" @click="setGroupsStatus(1)">禁用选中</el-button>
    </div>
  </div>
</template>

<script>
import { onMounted, reactive, toRefs } from 'vue';
import { useStore } from 'vuex';
import axios from 'axios';
import { ElMessage } from "element-plus";
export default {
  name: "DisableGroupModal",
  props: {
    isVisible: false,
  },
  setup() {
    const store = useStore();
    const data = reactive({
      disableGroupTableData: [],
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
        data.disableGroupTableData = rsp.data.data;
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

    const setGroupsStatus = async (status) => {
      if (data.uuidList.length === 0) {
        ElMessage.warning("请先勾选要操作的群聊");
        return;
      }
      try {
        const req = {
          uuid_list: data.uuidList,
          status: status,
        };
        await axios.post(store.state.apiUrl + "/admin/setGroupsStatus", req);
        ElMessage.success(status === 1 ? "已禁用选中群聊" : "已启用选中群聊");
        getGroupInfoList();
      } catch (error) {
        ElMessage.error("操作失败,请重试");
      }
    };

    return {
      ...toRefs(data),
      getGroupInfoList,
      avatarOf,
      setGroupsStatus,
      selectGroups,
    }
  }
};
</script>
