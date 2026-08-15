package kafka

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
	myconfig "gochat/internal/config"
	"gochat/pkg/zlog"
	"time"
)

var ctx = context.Background()

type kafkaService struct {
	ChatWriter *kafka.Writer
	ChatReader *kafka.Reader
	// PushWriter / PushReader 是推送事件广播通道（8k 节）：
	// 消息落库后写回 push topic，每个实例用独立 GroupID 消费全量推送事件，
	// 查本地 Clients map 推送——消费侧多实例分摊分区时，推送不再只在本实例生效。
	PushWriter *kafka.Writer
	PushReader *kafka.Reader
	KafkaConn  *kafka.Conn
}

var KafkaService = new(kafkaService)

// KafkaInit 初始化kafka
func (k *kafkaService) KafkaInit() {
	kafkaConfig := myconfig.GetConfig().KafkaConfig
	k.ChatWriter = &kafka.Writer{
		Addr:         kafka.TCP(kafkaConfig.HostPort),
		Topic:        kafkaConfig.ChatTopic,
		Balancer:     &kafka.Hash{},
		WriteTimeout: kafkaConfig.Timeout * time.Second,
		// acks 级别可配置（none/one/all），默认 one：leader 写入即确认，吞吐与可靠性折中。
		RequiredAcks:           parseRequiredAcks(kafkaConfig.RequiredAcks),
		AllowAutoTopicCreation: false,
		// IM 消息对延迟敏感：小批量 + 短批量窗口。
		// 默认 BatchTimeout=1s 会让低流量下的消息在生产者缓冲里等批，端到端延迟被拉高到数百毫秒。
		BatchSize:    10,
		BatchTimeout: 10 * time.Millisecond,
	}
	k.ChatReader = kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{kafkaConfig.HostPort},
		Topic:          kafkaConfig.ChatTopic,
		CommitInterval: kafkaConfig.Timeout * time.Second,
		GroupID:        "chat",
		StartOffset:    kafka.LastOffset,
		// 拉取等待上限：kafka-go Reader 的 MaxWait 默认 10s，
		// 低流量下消息在 fetch 请求中被挂起等待，是 Kafka 模式延迟长尾的主要来源。
		MaxWait: 100 * time.Millisecond,
	})
	// 推送事件广播：writer 与 chat 相同参数；reader 用独立 GroupID
	// （进程唯一）保证广播语义——每个实例都消费全量推送事件。
	k.PushWriter = &kafka.Writer{
		Addr:         kafka.TCP(kafkaConfig.HostPort),
		Topic:        kafkaConfig.PushTopic,
		Balancer:     &kafka.Hash{},
		WriteTimeout: kafkaConfig.Timeout * time.Second,
		RequiredAcks: parseRequiredAcks(kafkaConfig.RequiredAcks),
		// 推送事件对延迟敏感且每个实例都要消费：与 chat 相同的小批窗口。
		BatchSize:    10,
		BatchTimeout: 10 * time.Millisecond,
	}
	k.PushReader = kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{kafkaConfig.HostPort},
		Topic:          kafkaConfig.PushTopic,
		CommitInterval: kafkaConfig.Timeout * time.Second,
		// 广播语义：每个实例独立消费组（进程唯一后缀），互不干扰。
		GroupID:     fmt.Sprintf("chat_push_%d", os.Getpid()),
		StartOffset: kafka.FirstOffset,
		MaxWait:     100 * time.Millisecond,
	})
	// 启动时确保 topic 存在（分区数来自配置）。
	k.CreateTopic()
	// 8n 节：等 topic 分区元数据就绪。CreateTopics 返回后分区元数据在集群内
	// 异步传播，reader 首次 JoinGroup 若撞上"0 分区"的陈旧元数据会被分配
	// 0 个分区（实测：push 消费组空转、推送全丢，仅重启可恢复）；轮询直到
	// 分区数与配置一致（幂等：已存在的 topic 立即通过，无额外开销）。
	k.waitTopicsReady(kafkaConfig.ChatTopic, kafkaConfig.PushTopic)
}

// waitTopicsReady 轮询等待 topic 分区元数据与配置一致（8n 节）。
func (k *kafkaService) waitTopicsReady(topics ...string) {
	want := myconfig.GetConfig().KafkaConfig.Partition
	if want < 1 {
		want = 1
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := kafka.Dial("tcp", myconfig.GetConfig().KafkaConfig.HostPort)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		all := true
		for _, topic := range topics {
			parts, err := conn.ReadPartitions(topic)
			if err != nil || len(parts) != want {
				all = false
				break
			}
		}
		conn.Close()
		if all {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	zlog.Warn("topic 元数据等待超时", zap.Int("wantPartitions", want), zap.Strings("topics", topics))
}

// parseRequiredAcks 把配置字符串映射为 kafka-go 的确认级别。
func parseRequiredAcks(value string) kafka.RequiredAcks {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none":
		return kafka.RequireNone
	case "all":
		return kafka.RequireAll
	case "one", "":
		return kafka.RequireOne
	default:
		zlog.Warn("未知的 requiredAcks 配置，回退为 one", zap.String("value", value))
		return kafka.RequireOne
	}
}

func (k *kafkaService) KafkaClose() {
	if err := k.ChatWriter.Close(); err != nil {
		zlog.Error(err.Error())
	}
	if err := k.ChatReader.Close(); err != nil {
		zlog.Error(err.Error())
	}
	if k.PushWriter != nil {
		if err := k.PushWriter.Close(); err != nil {
			zlog.Error(err.Error())
		}
	}
	if k.PushReader != nil {
		if err := k.PushReader.Close(); err != nil {
			zlog.Error(err.Error())
		}
	}
}

// CreateTopic 创建topic（幂等：已存在则跳过）
func (k *kafkaService) CreateTopic() {
	// 如果已经有topic了，就不创建了
	kafkaConfig := myconfig.GetConfig().KafkaConfig

	chatTopic := kafkaConfig.ChatTopic
	pushTopic := kafkaConfig.PushTopic

	// 连接至任意kafka节点
	var err error
	k.KafkaConn, err = kafka.Dial("tcp", kafkaConfig.HostPort)
	if err != nil {
		zlog.Error(err.Error())
		return
	}
	defer k.KafkaConn.Close()

	numPartitions := kafkaConfig.Partition
	if numPartitions < 1 {
		numPartitions = 1
	}
	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             chatTopic,
			NumPartitions:     numPartitions,
			ReplicationFactor: 1,
		},
		{
			Topic:             pushTopic,
			NumPartitions:     numPartitions,
			ReplicationFactor: 1,
		},
	}

	// 创建topic
	if err = k.KafkaConn.CreateTopics(topicConfigs...); err != nil {
		zlog.Error(err.Error())
	}

}
