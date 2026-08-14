package v1

import (
	"strings"

	"github.com/gin-gonic/gin"

	"gochat/internal/dao"
	"gochat/internal/model"
	"gochat/internal/service/auth"
	"gochat/pkg/apperr"
)

// 注入 context 的鉴权字段。
const (
	ctxUUIDKey    = "auth_uuid"
	ctxIsAdminKey = "auth_is_admin"
)

// AuthUUID 从请求上下文读取已认证用户 uuid（由 AuthMiddleware 注入）。
func AuthUUID(c *gin.Context) string {
	return c.GetString(ctxUUIDKey)
}

// AuthIsAdmin 从请求上下文读取已认证用户的管理员标记。
func AuthIsAdmin(c *gin.Context) int8 {
	value, ok := c.Get(ctxIsAdminKey)
	if !ok {
		return 0
	}
	if v, ok := value.(int8); ok {
		return v
	}
	return 0
}

// AuthMiddleware 校验 Authorization: Bearer <access_token>，
// 解析成功后把 uuid / is_admin 注入 context。
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			c.Abort()
			c.Error(apperr.Unauthorized("未登录或登录已过期"))
			return
		}
		claims, err := auth.ParseAccessToken(token)
		if err != nil {
			c.Abort()
			c.Error(apperr.Unauthorized("登录已过期，请重新登录"))
			return
		}
		c.Set(ctxUUIDKey, claims.UUID)
		c.Set(ctxIsAdminKey, claims.IsAdmin)
		c.Next()
	}
}

// AdminMiddleware 校验管理员身份与账号状态（挂在 /admin 分组）。
func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if AuthIsAdmin(c) != 1 {
			c.Abort()
			c.Error(apperr.Forbidden("无管理员权限"))
			return
		}
		// 管理员账号被禁用后立即失去管理能力。
		uuid := AuthUUID(c)
		var user model.UserInfo
		if err := dao.GormDB.Select("status").Where("uuid = ?", uuid).First(&user).Error; err != nil {
			c.Abort()
			c.Error(apperr.SystemError(err))
			return
		}
		if user.Status == 1 {
			c.Abort()
			c.Error(apperr.Forbidden("该账号已被禁用"))
			return
		}
		c.Next()
	}
}

func bearerToken(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(header[len("Bearer "):])
	}
	return ""
}
