package chat

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/segmentio/kafka-go"
	"gochat/internal/config"
	"gochat/internal/dto/request"
	myKafka "gochat/internal/service/kafka"
	"gochat/pkg/constants"
	"gochat/pkg/zlog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// MessageBack 是下行推送的载体。Uuid 非空时，Write goroutine 在发送成功后把消息置为 Sent。
type MessageBack struct {
	Message []byte
	Uuid    string
}

// Client 是一条 WebSocket 连接。
//
// 执行模型（见 docs/design/messaging.md）：
//   - Read goroutine：读帧 → 投递 Transmit / 写 Kafka；
//   - Write goroutine：消费 SendBack → 写连接（唯一写者）→ 置 Sent；空闲时发 Ping；
//   - 回收 goroutine：两个 goroutine 都退出后，由连接管理方关闭 channel（关闭权唯一）。
type Client struct {
	Conn       *websocket.Conn
	Uuid       string
	SendTo     chan []byte       // 给 server 端（上行积压缓冲，Flush goroutine 转发）
	SendBack   chan *MessageBack // 给前端（下行）
	dropCount  int               // 连续丢弃计数，仅分发循环（单 goroutine）读写
	readDone   chan struct{}
	writeDone  chan struct{}
	flushDone  chan struct{}
	closeOnce  sync.Once
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  2048,
	WriteBufferSize: 2048,
	// 校验 Origin，拒绝跨站 WebSocket 劫持。
	// 允许：无 Origin（非浏览器客户端）或命中白名单（同源 / 前端开发地址）。
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		return originAllowed(origin)
	},
}

// originAllowed 判断 Origin 是否在白名单内。
func originAllowed(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	allowed := config.GetConfig().ServerConfig.WSAllowedOrigins
	if len(allowed) == 0 {
		allowed = []string{"http://localhost:8080", "http://127.0.0.1:8080"}
	}
	for _, a := range allowed {
		if parsed.String() == a || parsed.Host == a {
			return true
		}
	}
	return false
}

var ctx = context.Background()

var messageMode = config.GetConfig().KafkaConfig.MessageMode

// Flush 把本连接 SendTo 中的积压消息持续转发到全局 Transmit。
//
// 为什么需要独立转发 goroutine：如果只在"收到新消息"时顺带排空 SendTo，
// 突发场景下积压在 SendTo 里的消息在发送方不再发新消息后就永远无法到达
// 分发循环（实测 300 条突发仅 101 条进入分发）。独立转发保证积压必然被排空。
func (c *Client) Flush() {
	defer close(c.flushDone)
	for msg := range c.SendTo {
		ChatServer.SendMessageToTransmit(msg)
	}
}

// Read 读取 websocket 消息并发送给 send 通道（上行链路）。
func (c *Client) Read() {
	defer func() {
		close(c.readDone)
		c.closeConn()
	}()

	// 心跳：读侧通过 Pong 续期读超时，半开连接在 pongWait 内未被清理即断开。
	_ = c.Conn.SetReadDeadline(time.Now().Add(constants.PongWait))
	c.Conn.SetReadLimit(constants.MaxMessageSize)
	c.Conn.SetPongHandler(func(string) error {
		return c.Conn.SetReadDeadline(time.Now().Add(constants.PongWait))
	})

	for {
		_, jsonMessage, err := c.Conn.ReadMessage() // 阻塞状态
		if err != nil {
			zlog.Error(err.Error())
			return // 直接断开 websocket
		}
		var message = request.ChatMessageRequest{}
		if err := json.Unmarshal(jsonMessage, &message); err != nil {
			zlog.Error(err.Error())
			continue
		}
		if messageMode == "channel" {
			// 投递到本连接 SendTo（容量有界），由 Flush goroutine 转发到 Transmit。
			// SendTo 满说明本连接上行过载：拒绝本帧（拒绝回写走 SendBack，单写者约束）。
			select {
			case c.SendTo <- jsonMessage:
			default:
				c.SendBack <- &MessageBack{
					Message: []byte("由于目前同一时间过多用户发送消息，消息发送失败，请稍后重试"),
				}
			}
		} else {
			// Kafka 模式：以 receiveId 为分区键，保证同一会话/群的消息进入同一分区保序。
			key := message.ReceiveId
			if key == "" {
				key = message.SessionId
			}
			if err := myKafka.KafkaService.ChatWriter.WriteMessages(ctx, kafka.Message{
				Key:   []byte(key),
				Value: jsonMessage,
			}); err != nil {
				zlog.Error(err.Error())
			}
			zlog.Info("已发送消息：" + string(jsonMessage))
		}
	}
}

// Write 从 send 通道读取消息发送给 websocket（连接唯一写者），空闲时发 Ping 保活。
func (c *Client) Write() {
	defer func() {
		close(c.writeDone)
		c.closeConn()
	}()

	ticker := time.NewTicker(constants.PingPeriod)
	defer ticker.Stop()

	for {
		select {
		case messageBack, ok := <-c.SendBack:
			if !ok {
				return // 通道已由回收方关闭
			}
			if err := c.writeWithTimeout(websocket.TextMessage, messageBack.Message); err != nil {
				zlog.Error(err.Error())
				return
			}
			// 顺利发送即视为 Sent；状态落库交给异步批量 worker（避免写放大拖慢写侧）
			if messageBack.Uuid != "" {
				submitStatus(messageBack.Uuid)
			}
		case <-ticker.C:
			// 心跳：发送 Ping 控制帧；对端回 Pong 由读侧续期
			if err := c.writeWithTimeout(websocket.PingMessage, nil); err != nil {
				zlog.Error(err.Error())
				return
			}
		}
	}
}

func (c *Client) writeWithTimeout(messageType int, data []byte) error {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(constants.WriteWait))
	return c.Conn.WriteMessage(messageType, data)
}

func (c *Client) closeConn() {
	c.closeOnce.Do(func() {
		_ = c.Conn.Close()
	})
}

// NewClientInit 当接受到前端有登录消息时，会调用该函数
func NewClientInit(c *gin.Context, clientId string) {
	kafkaConfig := config.GetConfig().KafkaConfig
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		zlog.Error(err.Error())
		return
	}
	client := &Client{
		Conn:      conn,
		Uuid:      clientId,
		SendTo:    make(chan []byte, constants.CHANNEL_SIZE),
		SendBack:  make(chan *MessageBack, constants.CHANNEL_SIZE),
		readDone:  make(chan struct{}),
		writeDone: make(chan struct{}),
		flushDone: make(chan struct{}),
	}
	if kafkaConfig.MessageMode == "channel" {
		ChatServer.SendClientToLogin(client)
	} else {
		KafkaChatServer.SendClientToLogin(client)
	}
	go client.Read()
	go client.Write()
	go client.Flush()
	// 回收：三个 goroutine 都退出后统一关闭 channel（关闭权唯一），并记录离线时间。
	go func() {
		<-client.readDone
		<-client.writeDone
		<-client.flushDone
		close(client.SendTo)
		close(client.SendBack)
		MarkOffline(clientId)
	}()
	zlog.Info("ws连接成功")
}

// ClientLogout 当接受到前端有登出消息时，会调用该函数。
// 只摘除在线表并关闭连接，channel 由回收 goroutine 统一关闭（消除关闭竞态）。
func ClientLogout(clientId string) (string, int) {
	kafkaConfig := config.GetConfig().KafkaConfig
	client := ChatServer.Clients[clientId]
	if client != nil {
		if kafkaConfig.MessageMode == "channel" {
			ChatServer.SendClientToLogout(client)
		} else {
			KafkaChatServer.SendClientToLogout(client)
		}
		if err := client.Conn.Close(); err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
	}
	return "退出成功", 0
}
