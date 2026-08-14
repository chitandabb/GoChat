package kafka

import (
	"context"
	"strings"

	"github.com/segmentio/kafka-go"
	myconfig "gochat/internal/config"
	"gochat/pkg/zlog"
	"go.uber.org/zap"
	"time"
)

var ctx = context.Background()

type kafkaService struct {
	ChatWriter *kafka.Writer
	ChatReader *kafka.Reader
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
	// 启动时确保 topic 存在（分区数来自配置）。
	k.CreateTopic()
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
}

// CreateTopic 创建topic（幂等：已存在则跳过）
func (k *kafkaService) CreateTopic() {
	// 如果已经有topic了，就不创建了
	kafkaConfig := myconfig.GetConfig().KafkaConfig

	chatTopic := kafkaConfig.ChatTopic

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
	}

	// 创建topic
	if err = k.KafkaConn.CreateTopics(topicConfigs...); err != nil {
		zlog.Error(err.Error())
	}

}
