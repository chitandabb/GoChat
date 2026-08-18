package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
	"gochat/internal/dao"
	"gochat/internal/dto/request"
	"gochat/internal/dto/respond"
	"gochat/internal/model"
	myredis "gochat/internal/service/redis"
	"gochat/pkg/constants"
	"gochat/pkg/enum/message/message_status_enum"
	"gochat/pkg/enum/message/message_type_enum"
	"gochat/pkg/util/random"
	"gochat/pkg/zlog"
	"strings"
	"sync"
	"time"
)

type Server struct {
	// Kafka 模式回调（8m 节）：落库成功（含 duplicate）后提交 offset 并清除
	// in-flight（KafkaCommit）；落库失败后登记重试（KafkaRetry）。channel
	// 模式为 nil，走原"本地直推"路径。
	KafkaCommit func(msg kafka.Message)
	KafkaRetry  func(msg kafka.Message)
	Clients     map[string]*Client
	mutex       *sync.Mutex
	// Transmit 携带待分发的上行消息：channel 模式由客户端 Flush 直接投递；
	// Kafka 模式由 kafka_server 消费 chat topic 后转入（8k 节）。
	Transmit chan *TransmitData // 转发通道
	Login    chan *Client       // 登录通道
	Logout   chan *Client       // 退出登录通道
}

// TransmitData 携带一条待分发的上行消息；Kafka 模式下附带消费幂等键
// （topic:partition:offset:时间戳），落库时写入 message.kafka_key 唯一索引，
// 由 DB 兜底防重放重复落库（见 docs/notes/压测报告.md 8j 节）；channel 模式为空。
// KafkaMsg（8m 节）：Kafka 模式 Transmit 路径携带原 Kafka 消息，落库成功由
// KafkaCommit 回调提交 offset、失败由 KafkaRetry 登记重试——"先落库、后提交"
// 覆盖全部消息类型；channel 模式为 nil。
type TransmitData struct {
	Data     []byte
	KafkaKey string
	KafkaMsg *kafka.Message
}

// PersistedText 携带已落库的文本消息与原始请求：channel 模式供本地推送复用；
// Kafka 模式（8k 节）作为 chat_push 推送事件的载荷，落库后广播、各实例
// 消费后查本地 Clients map 下发（跨实例可达）。
type PersistedText struct {
	Req *request.ChatMessageRequest
	Msg *model.Message
}

var ChatServer *Server

func init() {
	if ChatServer == nil {
		ChatServer = &Server{
			Clients:  make(map[string]*Client),
			mutex:    &sync.Mutex{},
			Transmit: make(chan *TransmitData, constants.CHANNEL_SIZE),
			Login:    make(chan *Client, constants.CHANNEL_SIZE),
			Logout:   make(chan *Client, constants.CHANNEL_SIZE),
		}
	}
}

// 将https://127.0.0.1:8000/static/xxx 转为 /static/xxx
func normalizePath(path string) string {
	// 查找 "/static/" 的位置
	if path == "https://cube.elemecdn.com/0/88/03b0d39583f48206768a7534e55bcpng.png" {
		return path
	}
	staticIndex := strings.Index(path, "/static/")
	if staticIndex < 0 {
		zlog.Error("路径不合法: " + path)
		return path
	}
	// 返回从 "/static/" 开始的部分
	return path[staticIndex:]
}

// Start 启动函数，Server端用主进程起，Client端可以用协程起
func (s *Server) Start() {
	defer func() {
		close(s.Transmit)
		close(s.Logout)
		close(s.Login)
	}()
	for {
		// 单次迭代 recover：分发循环是消息链路的命脉，
		// 任何单条消息处理 panic 都不能拖垮整个循环（自愈后继续消费）。
		func() {
			defer func() {
				if r := recover(); r != nil {
					zlog.Error(fmt.Sprintf("分发循环单次迭代 panic（已恢复）: %v", r))
				}
			}()
			s.dispatchOnce()
		}()
	}
}

// dispatchOnce 处理一轮 Login / Logout / Transmit 事件。
func (s *Server) dispatchOnce() {
	select {
	case client := <-s.Login:
		{
			if client == nil {
				return
			}
			func() {
				s.mutex.Lock()
				defer s.mutex.Unlock()
				s.Clients[client.Uuid] = client
			}()
			zlog.Debug(fmt.Sprintf("欢迎来到GoChat聊天服务器，亲爱的用户%s\n", client.Uuid))
			// 欢迎帧也走 push 的恢复保护，避免 closed 检查与 close(SendBack)
			// 之间的竞态触发 panic。
			s.push(client, &MessageBack{Message: []byte("欢迎来到GoChat聊天服务器")})
		}

	case client := <-s.Logout:
		{
			if client == nil {
				return
			}
			func() {
				s.mutex.Lock()
				defer s.mutex.Unlock()
				delete(s.Clients, client.Uuid)
			}()
			zlog.Info(fmt.Sprintf("用户%s退出登录\n", client.Uuid))
			// 关闭连接触发 Read/Write goroutine 退出，回收路径统一清理 channel 并记录离线时间
			client.closeConn()
			MarkOffline(client.Uuid)
		}

	case data := <-s.Transmit:
		{
			if data == nil {
				return
			}
			msgStart := time.Now()
			var chatMessageReq request.ChatMessageRequest
			if err := json.Unmarshal(data.Data, &chatMessageReq); err != nil {
				zlog.Error(err.Error())
				if s.KafkaCommit != nil && data.KafkaMsg != nil {
					s.KafkaCommit(*data.KafkaMsg)
				}
				return
			}
			if chatMessageReq.ReceiveId == "" {
				zlog.Warn("消息缺少 receive_id，跳过并提交")
				if s.KafkaCommit != nil && data.KafkaMsg != nil {
					s.KafkaCommit(*data.KafkaMsg)
				}
				return
			}
			if chatMessageReq.Type == message_type_enum.Text {
				// 存message
				message := model.Message{
					Uuid:       fmt.Sprintf("M%s", random.GetNowAndLenRandomString(11)),
					SessionId:  chatMessageReq.SessionId,
					Type:       chatMessageReq.Type,
					Content:    chatMessageReq.Content,
					Url:        "",
					SendId:     chatMessageReq.SendId,
					SendName:   chatMessageReq.SendName,
					SendAvatar: chatMessageReq.SendAvatar,
					ReceiveId:  chatMessageReq.ReceiveId,
					FileSize:   "0B",
					FileType:   "",
					FileName:   "",
					Status:     message_status_enum.Unsent,
					CreatedAt:  time.Now(),
					AVdata:     "",
				}
				// 对SendAvatar去除前面/static之前的所有内容，防止ip前缀引入
				message.SendAvatar = normalizePath(message.SendAvatar)
				// Kafka 模式：携带消费幂等键，由 kafka_key 唯一索引兜底防重放重复落库
				if data.KafkaKey != "" {
					kk := data.KafkaKey
					message.KafkaKey = &kk
				}
				duplicate, err := persistMessage(&message)
				if err != nil {
					zlog.Error(err.Error())
					// Kafka 模式（8m 节）：落库失败不丢——登记重试（保持
					// in-flight、offset 未提交，连续水位不越过）；channel 模式
					// 原语义：丢弃（发送方已收到失败回执）。
					if s.KafkaRetry != nil && data.KafkaMsg != nil {
						s.KafkaRetry(*data.KafkaMsg)
					}
					return
				}
				if duplicate {
					// 重复投递（如 Kafka 重复消费），已处理过：完成，提交 offset。
					if s.KafkaCommit != nil && data.KafkaMsg != nil {
						s.KafkaCommit(*data.KafkaMsg)
					}
					return
				}
				// 落库成功：置幂等键 done，重放时跳过（未完成保持 pending 重试）
				if data.KafkaKey != "" {
					markDoneKey(data.KafkaKey)
				}
				// 8m 节：落库成功才提交 offset（manual commit 覆盖 Transmit 路径）。
				if s.KafkaCommit != nil && data.KafkaMsg != nil {
					s.KafkaCommit(*data.KafkaMsg)
				}
				if data.KafkaKey != "" {
					// Kafka 模式（8k 节）：推送经 chat_push 广播，所有实例消费后
					// 查本地 Clients map 下发——多实例分摊分区时跨实例也能送达。
					// channel 模式保持本地直推（进程内无跨实例问题）。
					publishPush(&PersistedText{Req: &chatMessageReq, Msg: &message})
				} else {
					s.dispatchPersistedText(msgStart, &chatMessageReq, &message)
				}
			} else if chatMessageReq.Type == message_type_enum.File {
				// 存message
				message := model.Message{
					Uuid:       fmt.Sprintf("M%s", random.GetNowAndLenRandomString(11)),
					SessionId:  chatMessageReq.SessionId,
					Type:       chatMessageReq.Type,
					Content:    "",
					Url:        chatMessageReq.Url,
					SendId:     chatMessageReq.SendId,
					SendName:   chatMessageReq.SendName,
					SendAvatar: chatMessageReq.SendAvatar,
					ReceiveId:  chatMessageReq.ReceiveId,
					FileSize:   chatMessageReq.FileSize,
					FileType:   chatMessageReq.FileType,
					FileName:   chatMessageReq.FileName,
					Status:     message_status_enum.Unsent,
					CreatedAt:  time.Now(),
					AVdata:     "",
				}
				// 对SendAvatar去除前面/static之前的所有内容，防止ip前缀引入
				message.SendAvatar = normalizePath(message.SendAvatar)
				// Kafka 模式：携带消费幂等键，由 kafka_key 唯一索引兜底防重放重复落库
				if data.KafkaKey != "" {
					kk := data.KafkaKey
					message.KafkaKey = &kk
				}
				duplicate, err := persistMessage(&message)
				if err != nil {
					zlog.Error(err.Error())
					// Kafka 模式（8m 节）：落库失败登记重试，不丢。
					if s.KafkaRetry != nil && data.KafkaMsg != nil {
						s.KafkaRetry(*data.KafkaMsg)
					}
					return
				}
				if duplicate {
					if s.KafkaCommit != nil && data.KafkaMsg != nil {
						s.KafkaCommit(*data.KafkaMsg)
					}
					return
				}
				// 落库成功：置幂等键 done，重放时跳过（未完成保持 pending 重试）
				if data.KafkaKey != "" {
					markDoneKey(data.KafkaKey)
				}
				// 8m 节：落库成功才提交 offset。
				if s.KafkaCommit != nil && data.KafkaMsg != nil {
					s.KafkaCommit(*data.KafkaMsg)
				}
				if message.ReceiveId[0] == 'U' { // 发送给User
					messageRsp := respond.GetMessageListRespond{
						SendId:     message.SendId,
						SendName:   message.SendName,
						SendAvatar: chatMessageReq.SendAvatar,
						ReceiveId:  message.ReceiveId,
						Type:       message.Type,
						Content:    message.Content,
						Url:        message.Url,
						FileSize:   message.FileSize,
						FileName:   message.FileName,
						FileType:   message.FileType,
						CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
					}
					jsonMessage, err := json.Marshal(messageRsp)
					if err != nil {
						zlog.Error(err.Error())
					}
					var messageBack = &MessageBack{
						Message: jsonMessage,
						Uuid:    message.Uuid,
					}
					for _, client := range s.clientsByUUID(message.ReceiveId, message.SendId) {
						s.push(client, messageBack)
					}

					// Cache-Aside：写路径只做失效删除（读 miss 时回源重建），
					// 不再逐条读改写（RMW）。删除双向 key，避免对方缓存脏读。
					invalidateUserMessageList(message.SendId, message.ReceiveId)
				} else {
					messageRsp := respond.GetGroupMessageListRespond{
						SendId:     message.SendId,
						SendName:   message.SendName,
						SendAvatar: chatMessageReq.SendAvatar,
						ReceiveId:  message.ReceiveId,
						Type:       message.Type,
						Content:    message.Content,
						Url:        message.Url,
						FileSize:   message.FileSize,
						FileName:   message.FileName,
						FileType:   message.FileType,
						CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
					}
					jsonMessage, err := json.Marshal(messageRsp)
					if err != nil {
						zlog.Error(err.Error())
					}
					var messageBack = &MessageBack{
						Message: jsonMessage,
						Uuid:    message.Uuid,
					}
					// 8n 节：群成员缓存（见 cachedGroupMembers）——逐事件 MySQL
					// 查询实测 5-25ms/次，1000 事件分发超压测排空窗口。
					members := cachedGroupMembers(message.ReceiveId)
					for _, client := range s.clientsByUUID(members...) {
						s.push(client, messageBack)
					}

					// Cache-Aside：写路径只做失效删除
					if err := myredis.DelKeys("group_messagelist_" + message.ReceiveId); err != nil {
						zlog.Error(err.Error())
					}
				}
			} else if chatMessageReq.Type == message_type_enum.AudioOrVideo {
				var avData request.AVData
				if err := json.Unmarshal([]byte(chatMessageReq.AVdata), &avData); err != nil {
					zlog.Error(err.Error())
				}
				message := model.Message{
					Uuid:       fmt.Sprintf("M%s", random.GetNowAndLenRandomString(11)),
					SessionId:  chatMessageReq.SessionId,
					Type:       chatMessageReq.Type,
					Content:    "",
					Url:        "",
					SendId:     chatMessageReq.SendId,
					SendName:   chatMessageReq.SendName,
					SendAvatar: chatMessageReq.SendAvatar,
					ReceiveId:  chatMessageReq.ReceiveId,
					FileSize:   "",
					FileType:   "",
					FileName:   "",
					Status:     message_status_enum.Unsent,
					CreatedAt:  time.Now(),
					AVdata:     chatMessageReq.AVdata,
				}
				if avData.MessageId == "PROXY" && (avData.Type == "start_call" || avData.Type == "receive_call" || avData.Type == "reject_call") {
					// 存message
					// 对SendAvatar去除前面/static之前的所有内容，防止ip前缀引入
					message.SendAvatar = normalizePath(message.SendAvatar)
					// Kafka 模式：携带消费幂等键，由 kafka_key 唯一索引兜底防重放重复落库
					if data.KafkaKey != "" {
						kk := data.KafkaKey
						message.KafkaKey = &kk
					}
					duplicate, err := persistMessage(&message)
					if err != nil {
						zlog.Error(err.Error())
						// Kafka 模式（8m 节）：落库失败登记重试，不丢。
						if s.KafkaRetry != nil && data.KafkaMsg != nil {
							s.KafkaRetry(*data.KafkaMsg)
						}
						return
					}
					if duplicate {
						if s.KafkaCommit != nil && data.KafkaMsg != nil {
							s.KafkaCommit(*data.KafkaMsg)
						}
						return
					}
					// 落库成功（或已存在）：置幂等键 done，重放时跳过
					if data.KafkaKey != "" {
						markDoneKey(data.KafkaKey)
					}
					// 8m 节：落库成功才提交 offset。
					if s.KafkaCommit != nil && data.KafkaMsg != nil {
						s.KafkaCommit(*data.KafkaMsg)
					}
					_ = duplicate
				} else if s.KafkaCommit != nil && data.KafkaMsg != nil {
					// SDP/candidate 等不满足持久化条件的信令只转发，明确
					// 跳过落库并完成 Kafka 记账，避免 in-flight 永久冻结。
					s.KafkaCommit(*data.KafkaMsg)
				}

				if chatMessageReq.ReceiveId[0] == 'U' { // 发送给User
					// 通话信令不落库回显（避免出现两个 start_call），仅转发给接收方
					messageRsp := respond.AVMessageRespond{
						SendId:     message.SendId,
						SendName:   message.SendName,
						SendAvatar: message.SendAvatar,
						ReceiveId:  message.ReceiveId,
						Type:       message.Type,
						Content:    message.Content,
						Url:        message.Url,
						FileSize:   message.FileSize,
						FileName:   message.FileName,
						FileType:   message.FileType,
						CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
						AVdata:     message.AVdata,
					}
					jsonMessage, err := json.Marshal(messageRsp)
					if err != nil {
						zlog.Error(err.Error())
					}
					var messageBack = &MessageBack{
						Message: jsonMessage,
						Uuid:    message.Uuid,
					}
					for _, client := range s.clientsByUUID(message.ReceiveId) {
						s.push(client, messageBack)
					}
					// 通话这不能回显，发回去的话就会出现两个start_call。
					//sendClient := s.Clients[message.SendId]
					//sendClient.SendBack <- messageBack
				}
			} else {
				zlog.Warn("未知消息类型，跳过并提交", zap.Int8("type", chatMessageReq.Type))
				if s.KafkaCommit != nil && data.KafkaMsg != nil {
					s.KafkaCommit(*data.KafkaMsg)
				}
			}

		}
	}
}

// dispatchPersistedText 对一条已落库的文本消息执行推送：单聊（接收方 + 发送方回显）与
// 群聊（逐成员扇出），并做写路径缓存失效。channel 模式在落库后调用；
// Kafka 模式经 chat_push 广播消费后调用（msgStart 用于慢分发告警，见 8k 节）。
func (s *Server) dispatchPersistedText(msgStart time.Time, chatMessageReq *request.ChatMessageRequest, message *model.Message) {
	if chatMessageReq == nil || message == nil || message.ReceiveId == "" {
		zlog.Warn("持久化消息缺少推送必要字段，跳过")
		return
	}
	if message.ReceiveId[0] == 'U' { // 发送给User
		messageRsp := respond.GetMessageListRespond{
			SendId:     message.SendId,
			SendName:   message.SendName,
			SendAvatar: chatMessageReq.SendAvatar,
			ReceiveId:  message.ReceiveId,
			Type:       message.Type,
			Content:    message.Content,
			Url:        message.Url,
			FileSize:   message.FileSize,
			FileName:   message.FileName,
			FileType:   message.FileType,
			CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		jsonMessage, err := json.Marshal(messageRsp)
		if err != nil {
			zlog.Error(err.Error())
		}
		var messageBack = &MessageBack{
			Message: jsonMessage,
			Uuid:    message.Uuid,
		}
		for _, client := range s.clientsByUUID(message.ReceiveId, message.SendId) {
			s.push(client, messageBack)
		}

		// Cache-Aside：写路径只做失效删除（读 miss 时回源重建），
		// 不再逐条读改写（RMW）。删除双向 key，避免对方缓存脏读。
		invalidateUserMessageList(message.SendId, message.ReceiveId)

	} else if message.ReceiveId[0] == 'G' { // 发送给Group
		if time.Since(msgStart) > 50*time.Millisecond {
			zlog.Warn("dispatch slow (group) after persist", zap.Duration("elapsed", time.Since(msgStart)))
		}
		messageRsp := respond.GetGroupMessageListRespond{
			SendId:     message.SendId,
			SendName:   message.SendName,
			SendAvatar: chatMessageReq.SendAvatar,
			ReceiveId:  message.ReceiveId,
			Type:       message.Type,
			Content:    message.Content,
			Url:        message.Url,
			FileSize:   message.FileSize,
			FileName:   message.FileName,
			FileType:   message.FileType,
			CreatedAt:  message.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		jsonMessage, err := json.Marshal(messageRsp)
		if err != nil {
			zlog.Error(err.Error())
		}
		var messageBack = &MessageBack{
			Message: jsonMessage,
			Uuid:    message.Uuid,
		}
		var members []string
		// 8n 节：群成员缓存——每事件一次 MySQL 查询实测 5-25ms（1000 事件
		// 分发 10s+，超压测 10s 排空窗口）；群成员变更低频，TTL 30s 内
		// 成员列表的短暂陈旧可接受（多推/漏推窗口 ≤30s，历史拉取兜底）。
		members = cachedGroupMembers(message.ReceiveId)
		for _, client := range s.clientsByUUID(members...) {
			s.push(client, messageBack)
		}

		// Cache-Aside：写路径只做失效删除。8n 节：失效删除异步化——同步
		// Redis DEL 实测 ~1-4ms/事件（压测负载下更慢），群扇出被钳制在
		// ~100/s，1000 事件在压测 10s 排空窗口内分发不完（送达率 81-98%
		// 抖动）。缓存是读路径加速（TTL 兜底），异步失效不破坏正确性。
		invalidateGroupMessageList(message.ReceiveId)
	}
}

// clientsByUUID 只在锁内读取在线表，返回快照后由调用方在锁外执行推送。
// 推送可能触发连接关闭或其他可恢复逻辑，不能把这些调用放在 Clients 锁内。
func (s *Server) clientsByUUID(uuids ...string) []*Client {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	clients := make([]*Client, 0, len(uuids))
	for _, uuid := range uuids {
		if client, ok := s.Clients[uuid]; ok && client != nil {
			clients = append(clients, client)
		}
	}
	return clients
}

// persistMessage 落库消息；返回 duplicate=true 表示 uuid 唯一键冲突（重复投递，已处理过）。
// 语义见 messaging.md：先落库（Unsent）后推送；落库失败不进入推送。
func persistMessage(message *model.Message) (bool, error) {
	if res := dao.GormDB.Create(message); res.Error != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(res.Error, &mysqlErr) && mysqlErr.Number == 1062 {
			zlog.Warn("消息重复投递，幂等忽略", zap.String("uuid", message.Uuid))
			return true, nil
		}
		return false, res.Error
	}
	return false, nil
}

// cachedGroupMembers 返回群成员 uuid 列表，带 30s TTL 缓存（8n 节）。// 群分发逐事件 MySQL 查询实测 5-25ms/次，1000 事件分发 10s+，超过压测
// 排空窗口；群成员变更低频，TTL 内成员列表短暂陈旧可接受（新成员 ≤30s
// 后才收到群推送，历史拉取兜底；被移除成员 ≤30s 内仍可能收到，语义无害）。
// 互斥锁只保护缓存检查与写入，不把数据库回源放在锁内。
type cachedGroupMembersEntry struct {
	members []string
	expire  time.Time
}

var (
	groupMembersCache   sync.Map // gid -> cachedGroupMembersEntry
	groupMembersCacheMu sync.Mutex
	// groupListInvalidateCh 群消息列表缓存失效异步队列（8n 节）：见
	// invalidateGroupMessageList——把 Redis DEL 从群扇出路径挪到独立
	// goroutine，失效删除与推送分发解耦；worker 攒批管道化删除。
	groupListInvalidateCh = make(chan string, 2048)
	// userListInvalidateCh 单聊消息列表缓存失效异步队列（8n 复测）：见
	// invalidateUserMessageList。机器负载下 Redis RTT 实测 ~0.5→2ms，
	// 同步 DelKeys 串在 push 消费关键路径上把消费速率从 ~1800 钳制到
	// ~600 事件/s（CHAT 端到端 P50 94ms → 秒级、回显丢半）；与群分支
	// 同样异步化 + worker 攒批管道化删除后分发路径不再依赖 Redis 往返。
	userListInvalidateCh = make(chan string, 4096)
)

func init() {
	go func() {
		// 群消息列表失效 worker：攒批 + 管道化删除。逐 key 串行 DEL 被
		// Redis RTT 钳制（机器负载下 ~2ms/次 → ~500 删除/s），跟不上
		// 1000 事件/s 的生产速率，队列满后 dispatch 退化同步删除，
		// 群扇出被重新钳制（8n 复测同源问题，与单聊分支一并修复）。
		batch := make([]string, 0, 128)
		for gid := range groupListInvalidateCh {
			batch = append(batch, "group_messagelist_"+gid)
		collectG:
			for len(batch) < 128 {
				select {
				case g := <-groupListInvalidateCh:
					batch = append(batch, "group_messagelist_"+g)
				default:
					break collectG
				}
			}
			if err := myredis.DelKeysPipelined(batch); err != nil {
				zlog.Error(err.Error())
			}
			batch = batch[:0]
		}
	}()
	go func() {
		// 单聊消息列表失效 worker：攒批 + 管道化删除（DelKeysPipelined
		// 一次往返删整批）。逐 key 串行 DEL 上限 ~500 删除/s，跟不上
		// 1000 msg/s × 2 key 的生产速率，队列满后 dispatch 退化同步
		// 删除、push 消费被重新钳制（实测 dispAvg 1.5-2ms → 消费 ~600
		// 事件/s → CHAT 端到端 P50 秒级、回显丢半）。
		batch := make([]string, 0, 128)
		for key := range userListInvalidateCh {
			batch = append(batch, key)
		collectU:
			for len(batch) < 128 {
				select {
				case k := <-userListInvalidateCh:
					batch = append(batch, k)
				default:
					break collectU
				}
			}
			if err := myredis.DelKeysPipelined(batch); err != nil {
				zlog.Error(err.Error())
			}
			batch = batch[:0]
		}
	}()
}

// invalidateUserMessageList 异步失效单聊消息列表缓存（双向 key，避免
// 对方缓存脏读）：队列满时退化为同步删除（极端积压保底；TTL 兜底最坏
// 情况也仅是多读一次 DB）。
func invalidateUserMessageList(sendId, receiveId string) {
	key1 := "message_list_" + sendId + "_" + receiveId
	key2 := "message_list_" + receiveId + "_" + sendId
	select {
	case userListInvalidateCh <- key1:
	default:
		if err := myredis.DelKeys(key1, key2); err != nil {
			zlog.Error(err.Error())
		}
		return
	}
	select {
	case userListInvalidateCh <- key2:
	default:
		// key1 已入队（worker 可能已删）；双向再删一次幂等无害。
		if err := myredis.DelKeys(key1, key2); err != nil {
			zlog.Error(err.Error())
		}
	}
}

// invalidateGroupMessageList 异步失效群消息列表缓存：队列满时退化为同步
// 删除（极端积压保底；TTL 兜底最坏情况也仅是多读一次 DB）。
func invalidateGroupMessageList(gid string) {
	select {
	case groupListInvalidateCh <- gid:
	default:
		if err := myredis.DelKeys("group_messagelist_" + gid); err != nil {
			zlog.Error(err.Error())
		}
	}
}

func cachedGroupMembers(gid string) []string {
	if v, ok := groupMembersCache.Load(gid); ok {
		e := v.(cachedGroupMembersEntry)
		if time.Now().Before(e.expire) {
			return e.members
		}
	}
	// 只保护缓存检查与写入，数据库查询必须在锁外执行，避免慢 IO
	// 阻塞其他群消息的缓存访问。
	groupMembersCacheMu.Lock()
	if v, ok := groupMembersCache.Load(gid); ok {
		e := v.(cachedGroupMembersEntry)
		if time.Now().Before(e.expire) {
			groupMembersCacheMu.Unlock()
			return e.members
		}
	}
	groupMembersCacheMu.Unlock()

	var group model.GroupInfo
	if res := dao.GormDB.Where("uuid = ?", gid).First(&group); res.Error != nil {
		zlog.Error(res.Error.Error())
		return nil
	}
	var members []string
	if err := json.Unmarshal(group.Members, &members); err != nil {
		zlog.Error(err.Error())
		return nil
	}

	groupMembersCacheMu.Lock()
	if v, ok := groupMembersCache.Load(gid); ok {
		e := v.(cachedGroupMembersEntry)
		if time.Now().Before(e.expire) {
			groupMembersCacheMu.Unlock()
			return e.members
		}
	}
	groupMembersCache.Store(gid, cachedGroupMembersEntry{members: members, expire: time.Now().Add(30 * time.Second)})
	groupMembersCacheMu.Unlock()
	return members
}

// push 非阻塞下行推送：
//   - SendBack 有空位 → 正常写入并清零丢弃计数；
//   - SendBack 已满 → 丢弃本次推送（消息保持 Unsent），dropCount++；
//   - dropCount 连续达到阈值 → 判定慢客户端，投递 Logout 通道走统一回收路径并断开连接。
//
// 分发循环永不因单个慢客户端阻塞（见 docs/design/messaging.md）。
func (s *Server) push(client *Client, back *MessageBack) {
	// closed 检查与 close(SendBack) 之间仍可能发生竞态，因此 select
	// 必须由 recover 兜底；panic 只能被视为本次发送失败，不能逃出分发循环。
	uuid := ""
	if client != nil {
		uuid = client.Uuid
	}
	defer func() {
		if r := recover(); r != nil {
			zlog.Error("下行推送失败（连接已关闭）", zap.String("uuid", uuid), zap.Any("panic", r))
		}
	}()
	if client == nil || back == nil || client.closed.Load() {
		return
	}
	client.dropMu.Lock()
	defer client.dropMu.Unlock()
	select {
	case client.SendBack <- back:
		client.dropCount = 0
	default:
		client.dropCount++
		zlog.Warn("慢客户端丢弃推送", zap.String("uuid", client.Uuid), zap.Int("dropCount", client.dropCount))
		if client.dropCount >= constants.SLOW_CLIENT_DROP_LIMIT {
			zlog.Warn("慢客户端连续超限，断开连接", zap.String("uuid", client.Uuid))
			client.dropCount = 0
			select {
			case s.Logout <- client:
			default:
			}
			client.closeConn()
		}
	}
}

func (s *Server) Close() {
	close(s.Login)
	close(s.Logout)
	close(s.Transmit)
}

// 下面的发送辅助函数【不能】在持有 mutex 时阻塞发送：
// channel 本身线程安全，mutex 只保护 Clients map；
// 若在锁内阻塞（如 Login/Transmit 缓冲满），分发循环将无法获取锁而全局死锁。
func (s *Server) SendClientToLogin(client *Client) {
	s.Login <- client
}

func (s *Server) SendClientToLogout(client *Client) {
	s.Logout <- client
}

// SendMessageToTransmit 把一条上行消息投递到分发循环。Kafka 模式 Transmit
// 路径携带原 Kafka 消息（km 非 nil，8m 节：落库成功才提交 offset）；
// channel 模式 km 为 nil。
func (s *Server) SendMessageToTransmit(message []byte, kafkaKey string, km *kafka.Message) {
	s.Transmit <- &TransmitData{Data: message, KafkaKey: kafkaKey, KafkaMsg: km}
}

func (s *Server) RemoveClient(uuid string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.Clients, uuid)
}

// KickOut 主动断开指定用户的连接（管理员禁用 / 全量登出）。
// 只关闭 TCP 连接，不操作 channel；连接回收仍走既有登出路径。
func KickOut(uuid string) {
	var client *Client
	func() {
		ChatServer.mutex.Lock()
		defer ChatServer.mutex.Unlock()
		client = ChatServer.Clients[uuid]
	}()
	if client != nil {
		zlog.Info("主动断开用户连接", zap.String("uuid", uuid))
		_ = client.Conn.Close()
	}
	var kafkaClient *Client
	func() {
		KafkaChatServer.server.mutex.Lock()
		defer KafkaChatServer.server.mutex.Unlock()
		kafkaClient = KafkaChatServer.server.Clients[uuid]
	}()
	if kafkaClient != nil {
		zlog.Info("主动断开用户连接(kafka)", zap.String("uuid", uuid))
		_ = kafkaClient.Conn.Close()
	}
}

// MarkOffline 记录用户最近离线时间（连接断开时调用）。
func MarkOffline(uuid string) {
	if err := dao.GormDB.Model(&model.UserInfo{}).Where("uuid = ?", uuid).Update("last_offline_at", time.Now()).Error; err != nil {
		zlog.Error(err.Error())
	}
}
