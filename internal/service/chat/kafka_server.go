package chat

import (
	"fmt"

	"gochat/internal/service/kafka"
	"gochat/pkg/constants"
	"gochat/pkg/zlog"
	"sync"
)

// KafkaServer 是 Kafka 模式的消息服务器。
//
// 设计（见 docs/design/messaging.md）：Kafka 模式与 channel 模式必须满足同一套
// 投递语义。因此这里不再复制一份分发逻辑，而是让 Kafka 消费循环把消息投递到
// 内部 *Server 的 Transmit 通道，登录 / 登出 / 下行推送 / 慢客户端治理全部复用
// Server 的既有实现。
type KafkaServer struct {
	server *Server
}

var KafkaChatServer *KafkaServer

func init() {
	if KafkaChatServer == nil {
		KafkaChatServer = &KafkaServer{
			server: &Server{
				Clients:  make(map[string]*Client),
				mutex:    &sync.Mutex{},
				Transmit: make(chan []byte, constants.CHANNEL_SIZE),
				Login:    make(chan *Client, constants.CHANNEL_SIZE),
				Logout:   make(chan *Client, constants.CHANNEL_SIZE),
			},
		}
	}
}

// Start 启动 Kafka 消费循环与内部 Server 分发循环。
func (k *KafkaServer) Start() {
	// 消费 Kafka chat 消息，投递到与 channel 模式相同的 Transmit 通道。
	// 崩溃恢复：未消费消息由 Kafka 保留；重复消费由消息 uuid 唯一键幂等去重。
	go func() {
		defer func() {
			if r := recover(); r != nil {
				zlog.Error(fmt.Sprintf("kafka consume panic: %v", r))
			}
		}()
		for {
			kafkaMessage, err := kafka.KafkaService.ChatReader.ReadMessage(ctx)
			if err != nil {
				zlog.Error(err.Error())
				continue
			}
			k.server.SendMessageToTransmit(kafkaMessage.Value)
		}
	}()
	k.server.Start()
}

func (k *KafkaServer) Close() {
	k.server.Close()
}

func (k *KafkaServer) SendClientToLogin(client *Client) {
	k.server.SendClientToLogin(client)
}

func (k *KafkaServer) SendClientToLogout(client *Client) {
	k.server.SendClientToLogout(client)
}

func (k *KafkaServer) RemoveClient(uuid string) {
	k.server.RemoveClient(uuid)
}
