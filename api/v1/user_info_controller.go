package v1

import (
	"github.com/gin-gonic/gin"

	"gochat/internal/dto/request"
	"gochat/internal/service/gorm"
)

// UpdateUserInfo 修改用户信息（身份取自已认证用户，不信任请求体 uuid）。
func UpdateUserInfo(c *gin.Context) {
	var req request.UpdateUserInfoRequest
	if !BindJSON(c, &req) {
		return
	}
	req.Uuid = AuthUUID(c)
	if err := gorm.UserInfoService.UpdateUserInfo(req); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// GetUserInfoList 管理员获取用户列表（排除自己）。
func GetUserInfoList(c *gin.Context) {
	var req request.GetUserInfoListRequest
	if !BindJSON(c, &req) {
		return
	}
	userList, err := gorm.UserInfoService.GetUserInfoList(AuthUUID(c))
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, userList)
}

// AbleUsers 启用用户
func AbleUsers(c *gin.Context) {
	var req request.AbleUsersRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.UserInfoService.AbleUsers(req.UuidList); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// DisableUsers 禁用用户（同时撤销其全部 Refresh 并断开在线连接）
func DisableUsers(c *gin.Context) {
	var req request.AbleUsersRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.UserInfoService.DisableUsers(req.UuidList); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// GetUserInfo 获取当前登录用户信息（身份取自已认证用户，不信任请求体 uuid）。
func GetUserInfo(c *gin.Context) {
	userInfo, err := gorm.UserInfoService.GetUserInfo(AuthUUID(c))
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, userInfo)
}

// DeleteUsers 删除用户
func DeleteUsers(c *gin.Context) {
	var req request.AbleUsersRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.UserInfoService.DeleteUsers(req.UuidList); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// SetAdmin 设置管理员
func SetAdmin(c *gin.Context) {
	var req request.AbleUsersRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.UserInfoService.SetAdmin(req.UuidList, req.IsAdmin); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}
