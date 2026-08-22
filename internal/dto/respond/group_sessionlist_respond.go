package respond

type GroupSessionListRespond struct {
	SessionId string `json:"session_id"`
	GroupName string `json:"group_name"`
	GroupId   string `json:"group_id"`
	Avatar    string `json:"avatar"`
	// LastMessage 最近一条消息的预览文案(群内非本人发送时带“发送者：”前缀);
	// 只在出参时回填,不进 Redis 缓存。
	LastMessage string `json:"last_message,omitempty"`
}
