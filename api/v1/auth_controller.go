package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gochat/internal/config"
	"gochat/internal/dto/request"
	"gochat/internal/dto/respond"
	"gochat/internal/service/auth"
	"gochat/internal/service/gorm"
	"gochat/pkg/apperr"
)

// Login 登录（密码），签发双 Token：
//   - Access Token 放响应体 data.access_token（前端内存持有）；
//   - Refresh Token 走 HttpOnly + SameSite=Strict Cookie（前端不可读）。
func Login(c *gin.Context) {
	var loginReq request.LoginRequest
	if !BindJSON(c, &loginReq) {
		return
	}
	// 登录失败限流（连续失败退避）在尝试登录前检查。
	if err := gorm.UserInfoService.CheckLoginRateLimit(loginReq.Telephone); err != nil {
		c.Error(err)
		return
	}
	userInfo, err := gorm.UserInfoService.Login(loginReq)
	if err != nil {
		c.Error(err)
		return
	}
	issueLoginRespond(c, userInfo)
}

// SmsLogin 验证码登录，签发双 Token。
func SmsLogin(c *gin.Context) {
	var req request.SmsLoginRequest
	if !BindJSON(c, &req) {
		return
	}
	userInfo, err := gorm.UserInfoService.SmsLogin(req)
	if err != nil {
		c.Error(err)
		return
	}
	issueLoginRespond(c, userInfo)
}

// Register 注册（短信验证码校验），注册成功后自动登录并签发双 Token。
func Register(c *gin.Context) {
	var registerReq request.RegisterRequest
	if !BindJSON(c, &registerReq) {
		return
	}
	userInfo, err := gorm.UserInfoService.Register(registerReq)
	if err != nil {
		c.Error(err)
		return
	}
	issueLoginRespond(c, userInfo)
}

// SendSmsCode 发送短信验证码。
func SendSmsCode(c *gin.Context) {
	var req request.SendSmsCodeRequest
	if !BindJSON(c, &req) {
		return
	}
	if err := gorm.UserInfoService.SendSmsCode(req.Telephone); err != nil {
		c.Error(err)
		return
	}
	OK(c, nil)
}

// Refresh 续期：旋转 Refresh Token（重放检测），返回新的 Access Token 并重下 Refresh Cookie。
func Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil || refreshToken == "" {
		c.Error(apperr.Unauthorized("未登录或登录已过期"))
		return
	}
	access, newRefresh, err := auth.Refresh(refreshToken)
	if err != nil {
		c.Error(err)
		return
	}
	setRefreshCookie(c, newRefresh)
	OK(c, &respond.RefreshRespond{AccessToken: access})
}

// Logout 登出：撤销当前 Refresh Token 并清除 Cookie。
func Logout(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil || refreshToken == "" {
		c.Error(apperr.Unauthorized("未登录或登录已过期"))
		return
	}
	_ = auth.Logout(refreshToken)
	clearRefreshCookie(c)
	OK(c, nil)
}

// issueLoginRespond 统一签发双 Token 并返回登录响应。
func issueLoginRespond(c *gin.Context, userInfo *respond.LoginRespond) {
	access, refresh, err := auth.IssueTokens(userInfo.Uuid, userInfo.IsAdmin)
	if err != nil {
		c.Error(err)
		return
	}
	setRefreshCookie(c, refresh)
	OK(c, &respond.LoginTokenRespond{
		AccessToken: access,
		UserInfo:    userInfo,
	})
}

// setRefreshCookie 下发 Refresh Cookie：
// HttpOnly（JS 不可读）+ SameSite=Strict（CSRF 缓解）+ Path 限定 /api/v1/auth。
func setRefreshCookie(c *gin.Context, refresh string) {
	cfg := config.GetConfig()
	maxAge := cfg.JwtConfig.RefreshTokenTTL * 24 * 3600
	secure := cfg.ServerConfig.TLSEnabled
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("refresh_token", refresh, maxAge, "/api/v1/auth", "", secure, true)
}

// clearRefreshCookie 清除 Refresh Cookie。
func clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("refresh_token", "", -1, "/api/v1/auth", "", false, true)
}
