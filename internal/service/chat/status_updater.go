package chat

import (
	"sync"
	"time"

	"gochat/internal/dao"
	"gochat/internal/model"
	"gochat/pkg/enum/message/message_status_enum"
	"gochat/pkg/zlog"
)

// 消息状态（Unsent -> Sent）异步批量落库。
//
// 背景（见 docs/design/messaging.md"每条推送的状态写放大"）：Write goroutine
// 每推送一条消息就同步 UPDATE 一次 message 表，单条 UPDATE 约 6.8ms（本机实测），
// 突发流量下 Write goroutine 会被拖成"慢客户端"，触发误杀。压测证明后改为：
// 已写入连接即视为 Sent（语义不变），落库交给后台 worker 批量 UPDATE。

const (
	statusBatchSize  = 64             // 单批最多累积条数
	statusFlushEvery = 100 * time.Millisecond // 最长刷盘间隔
)

var (
	statusCh      = make(chan string, 1024)
	statusUpdater sync.Once
)

// submitStatus 提交一条消息的 Sent 状态（非阻塞；通道满时丢弃，状态更新是尽力而为）。
func submitStatus(uuid string) {
	statusUpdater.Do(func() {
		go runStatusUpdater()
	})
	select {
	case statusCh <- uuid:
	default:
		zlog.Warn("状态更新通道已满，丢弃本次状态回写")
	}
}

// runStatusUpdater 消费 statusCh，批量 UPDATE message.status。
func runStatusUpdater() {
	batch := make([]string, 0, statusBatchSize)
	ticker := time.NewTicker(statusFlushEvery)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		uuids := batch
		batch = make([]string, 0, statusBatchSize)
		if err := dao.GormDB.Model(&model.Message{}).
			Where("uuid IN ?", uuids).
			Update("status", message_status_enum.Sent).Error; err != nil {
			zlog.Error(err.Error())
		}
	}

	for {
		select {
		case uuid := <-statusCh:
			batch = append(batch, uuid)
			if len(batch) >= statusBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
