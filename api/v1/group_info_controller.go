package v1

import (
	"github.com/gin-gonic/gin"
	"gochat/internal/dto/request"
	"gochat/internal/service/gorm"
)

// CreateGroup 创建群聊（群主身份取自已认证用户）
func CreateGroup(c *gin.Context) {
	var createGroupReq request.CreateGroupRequest
	if !BindJSON(c, &createGroupReq) {
		return
	}
	createGroupReq.OwnerId = AuthUUID(c)
	if err := gorm.GroupInfoService.CreateGroup(createGroupReq); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// LoadMyGroup 获取我创建的群聊
func LoadMyGroup(c *gin.Context) {
	groupList, err := gorm.GroupInfoService.LoadMyGroup(AuthUUID(c))
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, groupList)
}

// CheckGroupAddMode 检查群聊加群方式
func CheckGroupAddMode(c *gin.Context) {
	var req request.CheckGroupAddModeRequest
	if !BindJSON(c, &req) {
		return
	}
	addMode, err := gorm.GroupInfoService.CheckGroupAddMode(req.GroupId)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, addMode)
}

// EnterGroupDirectly 直接进群：owner_id 是群聊 id，入群用户是当前登录用户。
func EnterGroupDirectly(c *gin.Context) {
	var req request.EnterGroupDirectlyRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.GroupInfoService.EnterGroupDirectly(req.OwnerId, AuthUUID(c)); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// LeaveGroup 退群（退群用户是当前登录用户）
func LeaveGroup(c *gin.Context) {
	var req request.LeaveGroupRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.GroupInfoService.LeaveGroup(AuthUUID(c), req.GroupId); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// DismissGroup 解散群聊（仅群主）
func DismissGroup(c *gin.Context) {
	var req request.DismissGroupRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.GroupInfoService.DismissGroup(AuthUUID(c), req.GroupId); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// GetGroupInfo 获取群聊详情
func GetGroupInfo(c *gin.Context) {
	var req request.GetGroupInfoRequest
	if !BindJSON(c, &req) {
		return
	}
	groupInfo, err := gorm.GroupInfoService.GetGroupInfo(req.GroupId)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, groupInfo)
}

// GetGroupInfoList 获取群聊列表 - 管理员
func GetGroupInfoList(c *gin.Context) {
	groupList, err := gorm.GroupInfoService.GetGroupInfoList()
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, groupList)
}

// DeleteGroups 删除列表中群聊 - 管理员
func DeleteGroups(c *gin.Context) {
	var req request.DeleteGroupsRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.GroupInfoService.DeleteGroups(req.UuidList); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// SetGroupsStatus 设置群聊是否启用 - 管理员
func SetGroupsStatus(c *gin.Context) {
	var req request.SetGroupsStatusRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.GroupInfoService.SetGroupsStatus(req.UuidList, req.Status); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// UpdateGroupInfo 更新群聊消息
func UpdateGroupInfo(c *gin.Context) {
	var req request.UpdateGroupInfoRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.GroupInfoService.UpdateGroupInfo(req); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// GetGroupMemberList 获取群聊成员列表
func GetGroupMemberList(c *gin.Context) {
	var req request.GetGroupMemberListRequest
	if !BindJSON(c, &req) {
		return
	}
	groupMemberList, err := gorm.GroupInfoService.GetGroupMemberList(req.GroupId)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, groupMemberList)
}

// RemoveGroupMembers 移除群聊成员（操作者身份取自已认证用户）
func RemoveGroupMembers(c *gin.Context) {
	var req request.RemoveGroupMembersRequest
	if !BindJSON(c, &req) {
		return
	}
	req.OwnerId = AuthUUID(c)
	if err := gorm.GroupInfoService.RemoveGroupMembers(req); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}
