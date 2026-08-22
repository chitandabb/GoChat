<template>
  <div class="manager-table-pane" v-if="isVisible">
    <div class="manager-table">
      <el-table
        :data="setAdminTableData"
        height="100%"
        @selection-change="selectUsers"
      >
        <el-table-column type="selection" width="46" />
        <el-table-column label="用户" min-width="220">
          <template #default="scope">
            <div class="manager-cell-user">
              <el-avatar :size="34" :src="avatarOf(scope.row)" />
              <div class="manager-cell-user__meta">
                <span class="manager-cell-user__name">{{ scope.row.nickname }}</span>
                <span class="manager-cell-user__id">{{ scope.row.uuid }}</span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="telephone" label="手机号" width="150" />
        <el-table-column prop="email" label="邮箱" min-width="200" show-overflow-tooltip />
        <el-table-column label="角色" width="96" align="center">
          <template #default="scope">
            <el-tag v-if="scope.row.is_admin == 1" type="success" effect="light" round size="small">管理员</el-tag>
            <el-tag v-else type="info" effect="light" round size="small">普通用户</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="账号状态" width="100" align="center">
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
      <el-button class="soft-action-btn" @click="setAdmin(0)">取消管理员</el-button>
      <el-button class="soft-action-btn" @click="setAdmin(1)">设为管理员</el-button>
    </div>
  </div>
</template>

<script>
import { onMounted, reactive, toRefs } from "vue";
import { useStore } from "vuex";
import axios from "axios";
import { ElMessage } from "element-plus";
export default {
  name: "SetAdminModal",
  props: {
    isVisible: false,
  },
  setup() {
    const store = useStore();
    const data = reactive({
      setAdminTableData: [],
      uuidList: [],
    });
    onMounted(() => {
      getUserList();
    });
    const getUserList = async () => {
      try {
        const req = {
          owner_id: store.state.userInfo.uuid,
        }
        const rsp = await axios.post(
          store.state.apiUrl + "/admin/getUserInfoList", req
        );
        data.setAdminTableData = rsp.data.data;
      } catch (error) {
        ElMessage.error("用户列表加载失败");
      }
    };

    const avatarOf = (row) => {
      const avatar = row.avatar || "";
      return avatar && !avatar.startsWith("http")
        ? store.state.backendUrl + avatar
        : avatar;
    };

    const selectUsers = (val) => {
      data.uuidList = val.map((item) => item.uuid);
    };

    const setAdmin = async (isAdmin) => {
      if (data.uuidList.length === 0) {
        ElMessage.warning("请先勾选要操作的用户");
        return;
      }
      try {
        const req = {
          uuid_list: data.uuidList,
          is_admin: isAdmin,
        }
        await axios.post(store.state.apiUrl + "/admin/setAdmin", req);
        ElMessage.success(isAdmin === 1 ? "已设为管理员" : "已取消管理员");
        getUserList();
      } catch (error) {
        ElMessage.error("操作失败,请重试");
      }
    };

    return {
      ...toRefs(data),
      getUserList,
      avatarOf,
      selectUsers,
      setAdmin,
    };
  },
};
</script>
