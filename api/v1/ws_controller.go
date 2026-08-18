package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gochat/internal/dao"
	"gochat/internal/dto/request"
	"gochat/internal/model"
	"gochat/internal/service/auth"
	"gochat/internal/service/chat"
	"gochat/pkg/apperr"
	"gochat/pkg/enum/user_info/user_status_enum"
)

// WsLogin WebSocket 升级入口。
// 鉴权契约见 docs/design/api.md：/wss?token=<access_token>，
// token 无效 / 过期直接在升级前拒绝（HTTP 401），不建立连接；
// 连接身份以 claims 中的 uuid 为准，client_id 参数已废弃。
// 被禁用用户即使 Access Token 未过期也拒绝握手（与各 HTTP 接口一致）。
func WsLogin(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    apperr.CodeUnauthorized,
			"message": "未登录或登录已过期",
			"data":    nil,
		})
		return
	}
	claims, err := auth.ParseAccessToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    apperr.CodeUnauthorized,
			"message": "登录已过期，请重新登录",
			"data":    nil,
		})
		return
	}
	// 被禁用的用户禁止建立长连接：查询一次 user_info，避免禁用后仍能收发消息。
	var user model.UserInfo
	if res := dao.GormDB.Select("status").Where("uuid = ?", claims.UUID).First(&user); res.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    apperr.CodeUnauthorized,
			"message": "账号状态异常，请重新登录",
			"data":    nil,
		})
		return
	}
	if user.Status == user_status_enum.DISABLE {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    apperr.CodeForbidden,
			"message": "该账号已被禁用",
			"data":    nil,
		})
		return
	}
	chat.NewClientInit(c, claims.UUID)
}

// WsLogout 主动通知服务端断开当前连接（身份取自已认证用户）
func WsLogout(c *gin.Context) {
	var req request.WsLogoutRequest
	if !BindJSON(c, &req) {
		return
	}
	message, ret := chat.ClientLogout(AuthUUID(c))
	if ret == -1 {
		c.Error(apperr.SystemError(nil))
		return
	}
	if ret != 0 {
		c.Error(apperr.Biz(message))
		return
	}
	OK(c, nil)
}
