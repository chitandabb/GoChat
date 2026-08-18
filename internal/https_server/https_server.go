package https_server

import (
	"net/url"
	"os"
	"strings"

	v1 "gochat/api/v1"
	"gochat/internal/config"
	"gochat/pkg/apperr"
	"gochat/pkg/ssl"
	"gochat/pkg/zlog"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// GE 是当前项目全局共享的 Gin 引擎实例。
// main 启动 HTTP/HTTPS 服务时，就是直接用这份引擎来 Run / RunTLS。
var GE *gin.Engine

func init() {
	// 先把全局配置读出来，后面中间件、静态资源目录、SSL 跳转都会用到。
	conf := config.GetConfig()

	// gin.Default() 会创建一个带默认中间件的引擎：
	// 其中已经包含 Logger 和 Recovery。
	GE = gin.Default()

	// 配置跨域规则，方便前端开发环境直接访问后端接口。
	// 双 Token 方案的 Refresh Cookie 需要跨域携带（withCredentials），
	// 因此必须显式允许来源（不能用 *）+ AllowCredentials。
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowOriginFunc = corsAllowOrigin()
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	corsConfig.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	corsConfig.AllowCredentials = true
	GE.Use(cors.New(corsConfig))

	// 统一错误响应中间件：handler 通过 c.Error(err) 上报错误，
	// 由这里统一按业务码序列化为 {code, message, data} 失败响应。
	GE.Use(errorHandler())

	// 如果配置里开启了 SSLRedirect，就给 Gin 挂一个跳转中间件，
	// 把不安全请求重定向到 HTTPS。
	if conf.ServerConfig.SSLRedirect {
		GE.Use(ssl.TlsHandler(conf.ServerConfig.Host, conf.ServerConfig.Port))
	}

	// 挂载静态资源目录。
	// 前端访问 /static/avatars/... 或 /static/files/... 时，
	// Gin 会去本地文件系统对应目录读取文件。
	GE.Static("/static/avatars", conf.StaticSrcConfig.StaticAvatarPath)
	GE.Static("/static/files", conf.StaticSrcConfig.StaticFilePath)

	registerRoutes(GE)
}

// registerRoutes 按模块分组注册 /api/v1 路由。
// 分组规则见 docs/design/api.md：
//   - 公开路由：/auth/* 与 /user/register；
//   - 其余 /api/v1 路由挂鉴权中间件，/admin 额外挂管理员中间件。
func registerRoutes(ge *gin.Engine) {
	api := ge.Group("/api/v1")

	// 认证：登录、短信验证码、续期、登出（无需鉴权）
	auth := api.Group("/auth")
	{
		auth.POST("/login", v1.Login)
		auth.POST("/sendSmsCode", v1.SendSmsCode)
		auth.POST("/smsLogin", v1.SmsLogin)
		auth.POST("/refresh", v1.Refresh)
		auth.POST("/logout", v1.Logout)
	}

	// 注册（公开）
	api.POST("/user/register", v1.Register)

	// 受保护路由：全部需要 Access Token
	protected := api.Group("", v1.AuthMiddleware())

	// 账号：资料
	user := protected.Group("/user")
	{
		user.POST("/updateUserInfo", v1.UpdateUserInfo)
		user.POST("/getUserInfo", v1.GetUserInfo)
		user.POST("/wsLogout", v1.WsLogout)
	}

	// 管理后台：禁用 / 启用 / 删除用户与群、设置管理员（管理员中间件）
	admin := protected.Group("/admin", v1.AdminMiddleware())
	{
		admin.POST("/getUserInfoList", v1.GetUserInfoList)
		admin.POST("/ableUsers", v1.AbleUsers)
		admin.POST("/disableUsers", v1.DisableUsers)
		admin.POST("/deleteUsers", v1.DeleteUsers)
		admin.POST("/setAdmin", v1.SetAdmin)
		admin.POST("/getGroupInfoList", v1.GetGroupInfoList)
		admin.POST("/deleteGroups", v1.DeleteGroups)
		admin.POST("/setGroupsStatus", v1.SetGroupsStatus)
	}

	// 群聊
	group := protected.Group("/group")
	{
		group.POST("/createGroup", v1.CreateGroup)
		group.POST("/loadMyGroup", v1.LoadMyGroup)
		group.POST("/checkGroupAddMode", v1.CheckGroupAddMode)
		group.POST("/enterGroupDirectly", v1.EnterGroupDirectly)
		group.POST("/leaveGroup", v1.LeaveGroup)
		group.POST("/dismissGroup", v1.DismissGroup)
		group.POST("/getGroupInfo", v1.GetGroupInfo)
		group.POST("/updateGroupInfo", v1.UpdateGroupInfo)
		group.POST("/getGroupMemberList", v1.GetGroupMemberList)
		group.POST("/removeGroupMembers", v1.RemoveGroupMembers)
	}

	// 会话
	session := protected.Group("/session")
	{
		session.POST("/openSession", v1.OpenSession)
		session.POST("/getUserSessionList", v1.GetUserSessionList)
		session.POST("/getGroupSessionList", v1.GetGroupSessionList)
		session.POST("/deleteSession", v1.DeleteSession)
		session.POST("/checkOpenSessionAllowed", v1.CheckOpenSessionAllowed)
	}

	// 联系人
	contact := protected.Group("/contact")
	{
		contact.POST("/getUserList", v1.GetUserList)
		contact.POST("/loadMyJoinedGroup", v1.LoadMyJoinedGroup)
		contact.POST("/getContactInfo", v1.GetContactInfo)
		contact.POST("/deleteContact", v1.DeleteContact)
		contact.POST("/applyContact", v1.ApplyContact)
		contact.POST("/getNewContactList", v1.GetNewContactList)
		contact.POST("/passContactApply", v1.PassContactApply)
		contact.POST("/blackContact", v1.BlackContact)
		contact.POST("/cancelBlackContact", v1.CancelBlackContact)
		contact.POST("/getAddGroupList", v1.GetAddGroupList)
		contact.POST("/refuseContactApply", v1.RefuseContactApply)
		contact.POST("/blackApply", v1.BlackApply)
	}

	// 消息
	message := protected.Group("/message")
	{
		message.POST("/getMessageList", v1.GetMessageList)
		message.POST("/getGroupMessageList", v1.GetGroupMessageList)
		message.POST("/uploadAvatar", v1.UploadAvatar)
		message.POST("/uploadFile", v1.UploadFile)
	}

	// WebSocket 主连接入口：实时聊天消息长连接（不进 /api/v1，升级前自行校验 token）
	ge.GET("/wss", v1.WsLogin)
}

// corsAllowOrigin 返回 CORS 来源判定函数。
// 允许：显式配置的 GOCHAT_CORS_ORIGINS（逗号分隔）命中；或开发前端来源
// （http/https + 8080 端口，兼容 localhost / 127.0.0.1 / 局域网 IP 访问 dev server）。
// 与 chat 包 WS 握手的 originAllowed 规则保持一致。
func corsAllowOrigin() func(origin string) bool {
	explicit := corsOrigins()
	return func(origin string) bool {
		for _, allowed := range explicit {
			if origin == allowed {
				return true
			}
		}
		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return parsed.Port() == "8080" && (parsed.Scheme == "http" || parsed.Scheme == "https")
	}
}

// corsOrigins 返回显式允许的 CORS 来源。
// 允许通过 GOCHAT_CORS_ORIGINS 覆盖（逗号分隔），默认覆盖本地前端开发地址。
func corsOrigins() []string {
	raw := os.Getenv("GOCHAT_CORS_ORIGINS")
	if strings.TrimSpace(raw) == "" {
		return []string{"http://localhost:8080", "http://127.0.0.1:8080"}
	}
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

// errorHandler 把 handler 通过 c.Error() 上报的错误统一序列化为失败响应。
func errorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 {
			return
		}
		appErr := apperr.From(c.Errors.Last().Err)
		if appErr.Err != nil {
			zlog.Error(appErr.Err.Error())
		}
		// gin 的 responseWriter 初始 size 为 -1（noWritten）：
		// 只有 size < 0 时才说明响应尚未开始，可以统一补写状态码与业务响应体。
		// 已开始写响应（如 AbortWithError / 直接 Write）的场景只记日志。
		if c.Writer.Size() < 0 {
			c.JSON(apperr.HTTPStatus(appErr.Code), gin.H{
				"code":    appErr.Code,
				"message": appErr.Message,
				"data":    nil,
			})
		} else {
			// 响应已写出（如升级场景），只记录日志。
			zlog.Error("error after response written: " + appErr.Message)
		}
	}
}
