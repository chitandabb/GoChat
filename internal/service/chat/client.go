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
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// MessageBack 是下行推送的载体。Uuid 非空时，Write goroutine 在发送成功后把消息置为 Sent。
type MessageBack struct {
	Message []byte
	Uuid    string
}

// Kafka 模式发送路径异步化（8n 节）：WS 读循环只入队，后台 sendDrainLoop
// 攒批后一次 WriteMessages 提交多条。此前逐条同步写 ChatWriter
// （BatchSize=10/BatchTimeout=10ms，acks=one）在空 writer 上每次约 10ms，
// 读循环被钳制在 ~100 msg/s/连接（压测实测 5 连接 1000 msg/s 目标仅
// ~532 msg/s 入 topic，其余堆积 TCP 缓冲、连接关闭时丢弃，端到端延迟
// 虚高到 P50=3.2s）；批量提交让 writer 的 BatchSize=10 生效（64 条 ≈
// 7 次批量往返 ≈ 10-16ms）。入队不阻塞读循环；通道满（上行持续过载）
// 丢弃并记错误——与同步版"写失败仅记日志"的语义一致（消息丢失可观测）。
// 8n 复测：容量 8192 时 50 对 × 200 条突发（1 万条）在 0.3s 内涌来，
// drainer 写入 ~7k msg/s 追不上，溢出丢弃 1615 条（order 压测实测）；
// 提到 16384（≈10MB 上限）覆盖 1 万级突发（峰值积压 ~13k < 容量）。
var (
	sendCh     = make(chan []byte, 16384)
	sendChOnce sync.Once
)

// enqueueSend 非阻塞入队一条上行消息（Kafka 模式读循环专用）。
func enqueueSend(raw []byte) {
	sendChOnce.Do(func() {
		go sendDrainLoop()
	})
	select {
	case sendCh <- raw:
	default:
		zlog.Error("发送通道已满，丢弃消息（上行持续过载）")
	}
}

// sendDrainLoop 后台攒批写 chat topic（8n 节）：一次 WriteMessages 提交
// 多条，按 receiveId 分区键保序（通道 FIFO + writer 按分区发送，同一
// 会话/群的消息保持原序）。
func sendDrainLoop() {
	const maxBatch = 64
	for {
		raw := <-sendCh
		batch := make([][]byte, 0, maxBatch)
		batch = append(batch, raw)
	collect:
		for len(batch) < maxBatch {
			select {
			case q := <-sendCh:
				batch = append(batch, q)
			default:
				break collect
			}
		}
		msgs := make([]kafka.Message, 0, len(batch))
		for _, b := range batch {
			var message request.ChatMessageRequest
			if err := json.Unmarshal(b, &message); err != nil {
				zlog.Error("上行消息解析失败: " + err.Error())
				continue
			}
			key := message.ReceiveId
			if key == "" {
				key = message.SessionId
			}
			msgs = append(msgs, kafka.Message{Key: []byte(key), Value: b})
		}
		if len(msgs) == 0 {
			continue
		}
		if err := myKafka.KafkaService.ChatWriter.WriteMessages(context.Background(), msgs...); err != nil {
			zlog.Error("批量发送写入失败: " + err.Error())
		}
	}
}

// Client 是一条 WebSocket 连接。
//
// 执行模型（见 docs/design/messaging.md）：
//   - Read goroutine：读帧 → 投递 Transmit / 写 Kafka；
//   - Write goroutine：消费 SendBack → 写连接（唯一写者）→ 置 Sent；空闲时发 Ping；
//   - 回收 goroutine：两个 goroutine 都退出后，由连接管理方关闭 channel（关闭权唯一）。
type Client struct {
	Conn      *websocket.Conn
	Uuid      string
	SendTo    chan []byte       // 给 server 端（上行积压缓冲，Flush goroutine 转发）
	SendBack  chan *MessageBack // 给前端（下行）
	dropCount int               // 连续丢弃计数
	dropMu    sync.Mutex        // Kafka 推送循环与 Server 分发循环可能并发调用 push
	readDone  chan struct{}
	writeDone chan struct{}
	flushDone chan struct{}
	closeOnce sync.Once
	// closed 8m 节：回收 goroutine 关闭 SendBack 前置位（原子）。推送路径
	// （push / 欢迎消息）发送前检查——此前回收方先 close(SendBack) 而 client
	// 仍留在 Clients map 中，推送命中即 "send on closed channel" panic，
	// 且 panic 发生在持锁期间（无 defer），锁被永久占用，分发循环死锁
	// （实测：全站连接堆积 299+，见 docs/notes/压测报告.md 8m 节）。
	closed atomic.Bool
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
// 默认允许任意 host 的 http(s)://*:8080（前端 dev server 默认端口），
// 同时兼容显式配置的 WSAllowedOrigins / GOCHAT_CORS_ORIGINS 列表。
func originAllowed(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// 开发环境：任意 host 的 8080 端口前端（localhost / 127.0.0.1 / 局域网 IP 均可）。
	if isDevFrontendOrigin(parsed) {
		return true
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

// isDevFrontendOrigin 判断是否为前端开发服务器来源（http/https + 端口 8080）。
// 与 https_server 的 CORS 白名单规则保持一致。
func isDevFrontendOrigin(parsed *url.URL) bool {
	port := parsed.Port()
	if port == "" {
		return false
	}
	return port == "8080" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

var messageMode = config.GetConfig().KafkaConfig.MessageMode

// Flush 把本连接 SendTo 中的积压消息持续转发到全局 Transmit。
//
// 为什么需要独立转发 goroutine：如果只在"收到新消息"时顺带排空 SendTo，
// 突发场景下积压在 SendTo 里的消息在发送方不再发新消息后就永远无法到达
// 分发循环（实测 300 条突发仅 101 条进入分发）。独立转发保证积压必然被排空。
func (c *Client) Flush() {
	defer close(c.flushDone)
	for msg := range c.SendTo {
		ChatServer.SendMessageToTransmit(msg, "", nil)
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
				// 8m 节：拒绝回写改非阻塞（原为阻塞发送）——Write goroutine
				// 若已退出/正在退出，阻塞发送会卡死 Read goroutine，回收流程
				// 等待 readDone 随之挂起（连接泄漏）。
				if !c.closed.Load() {
					select {
					case c.SendBack <- &MessageBack{
						Message: []byte("由于目前同一时间过多用户发送消息，消息发送失败，请稍后重试"),
					}:
					default:
					}
				}
			}
		} else {
			// Kafka 模式：以 receiveId 为分区键，保证同一会话/群的消息进入同一分区保序。
			// 8n 节：读循环不再逐条同步写 ChatWriter（每条约 10ms，读循环被钳制在
			// ~100 msg/s/连接，实测 30% 上行堆积 TCP 缓冲在断开时丢弃）——非阻塞入队，
			// 由 sendDrainLoop 攒批一次提交多条；通道满丢弃并记错误（见 enqueueSend）。
			enqueueSend(jsonMessage)
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
		case <-c.readDone:
			// 8m 节：读侧已退出（连接断开/读错误，closeConn 已关 TCP）——
			// 写侧同步退出，回收流程不再等待下一次 Ping 周期（此前空闲连接
			// 断开后 Write 需等 PingPeriod 才感知，回收整体挂起，
			// 每断连泄漏 Read/Write/Flush 三个 goroutine 直到超时）。
			return
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
	// 8n 节：TCP_NODELAY——关闭 Nagle 后小帧即时发送。此前下行小帧受
	// Nagle + 延迟 ACK 抑制（实测 ~8ms/帧），突发推送时成员连接排空
	// 仅 ~125 msg/s，SendBack 队列被打满误判慢客户端丢弃（压测实测：
	// 群 500/s 扇出时 75% 推送被丢弃，见 docs/notes/压测报告.md 8n 节）。
	if tcp, ok := conn.UnderlyingConn().(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
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
	// 8m 节修复：先启动 Read/Write/Flush 三个 goroutine，再投递登录。
	// 此前顺序相反——dispatchOnce 处理 Login case 时往 SendBack 发欢迎消息，
	// 若 Write goroutine 尚未启动/已退出（连接在登录排队期间断开），
	// SendBack 无人消费，阻塞发送会把分发循环永久卡死（压测实测：
	// 299 个登录请求堆积，全部新连接不可用，见 docs/notes/压测报告.md 8m 节）。
	go client.Read()
	go client.Write()
	go client.Flush()
	if kafkaConfig.MessageMode == "channel" {
		ChatServer.SendClientToLogin(client)
	} else {
		KafkaChatServer.SendClientToLogin(client)
	}
	// 回收：等读/写 goroutine 退出后，先关 SendTo 让 Flush 退出，再收尾。
	// 8m 节修复：此前顺序为"等 flushDone → 关 SendTo"，而 Flush 只有在
	// SendTo 关闭后才退出（for range），形成循环等待——每次断连泄漏
	// Flush 与回收两个 goroutine（压测审查发现，见 docs/notes/压测报告.md 8m 节）。
	go func() {
		<-client.readDone
		<-client.writeDone
		close(client.SendTo) // Flush 退出条件（先于等待 flushDone）
		<-client.flushDone
		// 8m 节：置 closed 标记后再关 SendBack——推送路径据此跳过本连接，
		// 避免"已关闭 channel 发送 panic"（此前实测 panic 死锁分发循环）。
		client.closed.Store(true)
		close(client.SendBack)
		MarkOffline(clientId)
	}()
	zlog.Info("ws连接成功")
}

// ClientLogout 当接受到前端有登出消息时，会调用该函数。
// 只摘除在线表并关闭连接，channel 由回收 goroutine 统一关闭（消除关闭竞态）。
func ClientLogout(clientId string) (string, int) {
	kafkaConfig := config.GetConfig().KafkaConfig
	server := ChatServer
	if kafkaConfig.MessageMode != "channel" {
		server = KafkaChatServer.server
	}
	var client *Client
	func() {
		server.mutex.Lock()
		defer server.mutex.Unlock()
		client = server.Clients[clientId]
	}()
	if client != nil {
		server.SendClientToLogout(client)
		if err := client.Conn.Close(); err != nil {
			zlog.Error(err.Error())
			return constants.SYSTEM_ERROR, -1
		}
	}
	return "退出成功", 0
}
