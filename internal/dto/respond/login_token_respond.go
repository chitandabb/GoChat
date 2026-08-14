package respond

// LoginTokenRespond 是登录 / 注册成功后的统一响应。
// Refresh Token 通过 HttpOnly Cookie 下发，不进入响应体。
type LoginTokenRespond struct {
	AccessToken string        `json:"access_token"`
	UserInfo    *LoginRespond `json:"user_info"`
}

// RefreshRespond 是 /auth/refresh 的响应（仅新的 Access Token）。
type RefreshRespond struct {
	AccessToken string `json:"access_token"`
}
