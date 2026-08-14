package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gochat/internal/dto/request"
	"gochat/internal/service/auth"
	"gochat/internal/service/chat"
	"gochat/pkg/apperr"
)

// WsLogin WebSocket 升级入口。
// 鉴权契约见 docs/design/api.md：/wss?token=<access_token>，
// token 无效 / 过期直接在升级前拒绝（HTTP 401），不建立连接；
// 连接身份以 claims 中的 uuid 为准，client_id 参数已废弃。
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
