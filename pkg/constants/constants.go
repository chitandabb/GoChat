package constants

import "time"

const (
	CHANNEL_SIZE  = 100            // 通道大小
	SYSTEM_ERROR  = "系统错误，请联系工作人员" // 系统错误
	FILE_MAX_SIZE = 50000          // 文件最大大小
	REDIS_TIMEOUT = 1              // redis timeout
)

// WebSocket 连接治理参数（见 docs/design/messaging.md M2）。
const (
	// MaxMessageSize 单帧消息大小上限（字节）。
	MaxMessageSize = 4096
	// WriteWait 写超时：单次写操作允许的最大时间。
	WriteWait = 10 * time.Second
	// PongWait 读超时：等待 Pong 的最大时间，超过则判定半开连接。
	PongWait = 60 * time.Second
	// PingPeriod 心跳发送周期，必须小于 PongWait。
	PingPeriod = 50 * time.Second
	// SLOW_CLIENT_DROP_LIMIT 连续丢弃阈值：达到后判定慢客户端并断开连接。
	SLOW_CLIENT_DROP_LIMIT = 8
)
