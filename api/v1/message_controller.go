package v1

import (
	"github.com/gin-gonic/gin"
	"gochat/internal/dto/request"
	"gochat/internal/service/gorm"
)

// GetMessageList 获取聊天记录（user_one_id 为当前登录用户，user_two_id 为对方）
func GetMessageList(c *gin.Context) {
	var req request.GetMessageListRequest
	if !BindJSON(c, &req) {
		return
	}
	rsp, err := gorm.MessageService.GetMessageList(AuthUUID(c), req.UserTwoId)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, rsp)
}

// GetGroupMessageList 获取群聊消息记录
func GetGroupMessageList(c *gin.Context) {
	var req request.GetGroupMessageListRequest
	if !BindJSON(c, &req) {
		return
	}
	rsp, err := gorm.MessageService.GetGroupMessageList(req.GroupId)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, rsp)
}

// UploadAvatar 上传头像，返回落盘后的相对路径（/static/avatars/<新文件名>）
func UploadAvatar(c *gin.Context) {
	path, err := gorm.MessageService.UploadAvatar(c)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, path)
}

// UploadFile 上传文件，返回落盘后的相对路径（/static/files/<新文件名>）
func UploadFile(c *gin.Context) {
	path, err := gorm.MessageService.UploadFile(c)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, path)
}
