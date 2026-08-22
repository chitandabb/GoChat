package respond

type UserSessionListRespond struct {
	SessionId string `json:"session_id"`
	Avatar    string `json:"avatar"`
	UserId    string `json:"user_id"`
	Username  string `json:"user_name"`
	// LastMessage 最近一条消息的预览文案(服务端生成,与前端实时预览规则一致);
	// 只在出参时回填,不进 Redis 缓存,保证新消息后预览即时可见。
	LastMessage string `json:"last_message,omitempty"`
}
