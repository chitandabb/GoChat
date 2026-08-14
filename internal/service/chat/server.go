package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
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
	"go.uber.org/zap"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Clients  map[string]*Client
	mutex    *sync.Mutex
	Transmit chan []byte  // 转发通道
	Login    chan *Client // 登录通道
	Logout   chan *Client // 退出登录通道
}

var ChatServer *Server

func init() {
	if ChatServer == nil {
		ChatServer = &Server{
			Clients:  make(map[string]*Client),
			mutex:    &sync.Mutex{},
			Transmit: make(chan []byte, constants.CHANNEL_SIZE),
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
				s.mutex.Lock()
				s.Clients[client.Uuid] = client
				s.mutex.Unlock()
				zlog.Debug(fmt.Sprintf("欢迎来到GoChat聊天服务器，亲爱的用户%s\n", client.Uuid))
				// 欢迎消息经 SendBack 下发（连接唯一写者在 Write goroutine，避免多写者竞态）
				client.SendBack <- &MessageBack{Message: []byte("欢迎来到GoChat聊天服务器")}
			}

		case client := <-s.Logout:
			{
				s.mutex.Lock()
				delete(s.Clients, client.Uuid)
				s.mutex.Unlock()
				zlog.Info(fmt.Sprintf("用户%s退出登录\n", client.Uuid))
				// 关闭连接触发 Read/Write goroutine 退出，回收路径统一清理 channel 并记录离线时间
				client.closeConn()
				MarkOffline(client.Uuid)
			}

		case data := <-s.Transmit:
			{
				msgStart := time.Now()
				var chatMessageReq request.ChatMessageRequest
				if err := json.Unmarshal(data, &chatMessageReq); err != nil {
					zlog.Error(err.Error())
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
					duplicate, err := persistMessage(&message)
					if err != nil {
						zlog.Error(err.Error())
						return
					}
					if duplicate {
						return // 重复投递（如 Kafka 重复消费），已处理过，跳过
					}
					if message.ReceiveId[0] == 'U' { // 发送给User
						// 如果能找到ReceiveId，说明在线，可以发送，否则存表后跳过
						// 因为在线的时候是通过websocket更新消息记录的，离线后通过存表，登录时只调用一次数据库操作
						// 切换chat对象后，前端的messageList也会改变，获取messageList从第二次就是从redis中获取
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
						s.mutex.Lock()
						if receiveClient, ok := s.Clients[message.ReceiveId]; ok {
							s.push(receiveClient, messageBack)
						}
						// 因为send_id肯定在线，所以这里在后端进行在线回显message，其实优化的话前端可以直接回显
						// 问题在于前后端的req和rsp结构不同，前端存储message的messageList不能存req，只能存rsp
						// 所以这里后端进行回显，前端不回显
						if sendClient := s.Clients[message.SendId]; sendClient != nil {
							s.push(sendClient, messageBack)
						}
						s.mutex.Unlock()

						// Cache-Aside：写路径只做失效删除（读 miss 时回源重建），
						// 不再逐条读改写（RMW）。删除双向 key，避免对方缓存脏读。
						if err := myredis.DelKeys("message_list_"+message.SendId+"_"+message.ReceiveId, "message_list_"+message.ReceiveId+"_"+message.SendId); err != nil {
							zlog.Error(err.Error())
						}

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
						var group model.GroupInfo
						if res := dao.GormDB.Where("uuid = ?", message.ReceiveId).First(&group); res.Error != nil {
							zlog.Error(res.Error.Error())
						}
						var members []string
						if err := json.Unmarshal(group.Members, &members); err != nil {
							zlog.Error(err.Error())
						}
						s.mutex.Lock()
						for _, member := range members {
							if member != message.SendId {
								if receiveClient, ok := s.Clients[member]; ok {
									s.push(receiveClient, messageBack)
								}
							} else {
								if sendClient := s.Clients[message.SendId]; sendClient != nil {
									s.push(sendClient, messageBack)
								}
							}
						}
						s.mutex.Unlock()

						// Cache-Aside：写路径只做失效删除
						if err := myredis.DelKeys("group_messagelist_" + message.ReceiveId); err != nil {
							zlog.Error(err.Error())
						}
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
					duplicate, err := persistMessage(&message)
					if err != nil {
						zlog.Error(err.Error())
						return
					}
					if duplicate {
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
						s.mutex.Lock()
						if receiveClient, ok := s.Clients[message.ReceiveId]; ok {
							s.push(receiveClient, messageBack)
						}
						if sendClient := s.Clients[message.SendId]; sendClient != nil {
							s.push(sendClient, messageBack)
						}
						s.mutex.Unlock()

						// Cache-Aside：写路径只做失效删除（读 miss 时回源重建），
						// 不再逐条读改写（RMW）。删除双向 key，避免对方缓存脏读。
						if err := myredis.DelKeys("message_list_"+message.SendId+"_"+message.ReceiveId, "message_list_"+message.ReceiveId+"_"+message.SendId); err != nil {
							zlog.Error(err.Error())
						}
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
						var group model.GroupInfo
						if res := dao.GormDB.Where("uuid = ?", message.ReceiveId).First(&group); res.Error != nil {
							zlog.Error(res.Error.Error())
						}
						var members []string
						if err := json.Unmarshal(group.Members, &members); err != nil {
							zlog.Error(err.Error())
						}
						s.mutex.Lock()
						for _, member := range members {
							if member != message.SendId {
								if receiveClient, ok := s.Clients[member]; ok {
									s.push(receiveClient, messageBack)
								}
							} else {
								if sendClient := s.Clients[message.SendId]; sendClient != nil {
									s.push(sendClient, messageBack)
								}
							}
						}
						s.mutex.Unlock()

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
						duplicate, err := persistMessage(&message)
						if err != nil {
							zlog.Error(err.Error())
							return
						}
						_ = duplicate
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
						s.mutex.Lock()
						if receiveClient, ok := s.Clients[message.ReceiveId]; ok {
							s.push(receiveClient, messageBack)
						}
						// 通话这不能回显，发回去的话就会出现两个start_call。
						//sendClient := s.Clients[message.SendId]
						//sendClient.SendBack <- messageBack
						s.mutex.Unlock()
					}
				}

			}
	}
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

// push 非阻塞下行推送：
//   - SendBack 有空位 → 正常写入并清零丢弃计数；
//   - SendBack 已满 → 丢弃本次推送（消息保持 Unsent），dropCount++；
//   - dropCount 连续达到阈值 → 判定慢客户端，投递 Logout 通道走统一回收路径并断开连接。
//
// 分发循环永不因单个慢客户端阻塞（见 docs/design/messaging.md）。
func (s *Server) push(client *Client, back *MessageBack) {
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

func (s *Server) SendMessageToTransmit(message []byte) {
	s.Transmit <- message
}

func (s *Server) RemoveClient(uuid string) {
	s.mutex.Lock()
	delete(s.Clients, uuid)
	s.mutex.Unlock()
}

// KickOut 主动断开指定用户的连接（管理员禁用 / 全量登出）。
// 只关闭 TCP 连接，不操作 channel；连接回收仍走既有登出路径。
func KickOut(uuid string) {
	ChatServer.mutex.Lock()
	client, ok := ChatServer.Clients[uuid]
	ChatServer.mutex.Unlock()
	if ok && client != nil {
		zlog.Info("主动断开用户连接", zap.String("uuid", uuid))
		_ = client.Conn.Close()
	}
	KafkaChatServer.server.mutex.Lock()
	kafkaClient, kafkaOk := KafkaChatServer.server.Clients[uuid]
	KafkaChatServer.server.mutex.Unlock()
	if kafkaOk && kafkaClient != nil {
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
