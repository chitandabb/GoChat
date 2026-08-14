package v1

import (
	"github.com/gin-gonic/gin"
	"gochat/internal/dto/request"
	"gochat/internal/service/gorm"
)

// OpenSession 打开会话（发起方身份取自已认证用户）
func OpenSession(c *gin.Context) {
	var openSessionReq request.OpenSessionRequest
	if !BindJSON(c, &openSessionReq) {
		return
	}
	openSessionReq.SendId = AuthUUID(c)
	sessionId, err := gorm.SessionService.OpenSession(openSessionReq)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, sessionId)
}

// GetUserSessionList 获取用户会话列表
func GetUserSessionList(c *gin.Context) {
	sessionList, err := gorm.SessionService.GetUserSessionList(AuthUUID(c))
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, sessionList)
}

// GetGroupSessionList 获取群聊会话列表
func GetGroupSessionList(c *gin.Context) {
	groupList, err := gorm.SessionService.GetGroupSessionList(AuthUUID(c))
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, groupList)
}

// DeleteSession 删除会话
func DeleteSession(c *gin.Context) {
	var deleteSessionReq request.DeleteSessionRequest
	if !BindJSON(c, &deleteSessionReq) {
		return
	}
	if err := gorm.SessionService.DeleteSession(AuthUUID(c), deleteSessionReq.SessionId); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// CheckOpenSessionAllowed 检查是否可以打开会话（发起方身份取自已认证用户）
func CheckOpenSessionAllowed(c *gin.Context) {
	var req request.CreateSessionRequest
	if !BindJSON(c, &req) {
		return
	}
	res, err := gorm.SessionService.CheckOpenSessionAllowed(AuthUUID(c), req.ReceiveId)
	if err != nil {
		c.Error(err)
		return
	}
	OK(c, res)
}
