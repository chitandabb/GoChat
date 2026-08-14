package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gochat/pkg/apperr"
	"gochat/pkg/zlog"
)

// OK 统一成功响应。
// 契约见 docs/design/api.md: { "code": 0, "message": "ok", "data": ... }
func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{
		"code":    apperr.CodeOK,
		"message": "ok",
		"data":    data,
	})
}

// BindJSON 绑定并校验请求体。
// 失败时统一记录为 40001 参数错误并中断请求，由全局错误中间件序列化响应。
// 返回 false 表示绑定失败，handler 应立即 return。
func BindJSON(c *gin.Context, obj interface{}) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		zlog.Error(err.Error())
		c.Abort()
		c.Error(apperr.BadRequest("参数错误"))
		return false
	}
	return true
}
