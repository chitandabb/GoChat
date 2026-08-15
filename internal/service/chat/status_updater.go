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
	// statusDedup 状态提交去重（8n 节）：群消息扇出时同一条消息被推送给
	// 400 个成员，Write goroutine 每推一次就提交一次状态——400 倍重复
	// 提交瞬间打满 statusCh（实测 400k 次提交、21.4 万次丢弃告警，日志
	// 洪峰把磁盘写死、群分发被拖慢到 60.8% 送达）。状态是"消息级"语义，
	// 同一条消息只需提交一次；条目在 worker 批量刷盘后删除。
	statusDedup sync.Map // uuid -> struct{}
)

// submitStatus 提交一条消息的 Sent 状态（非阻塞；同一条消息的重复推送
// 只提交一次；通道满时丢弃，状态更新是尽力而为）。
func submitStatus(uuid string) {
	statusUpdater.Do(func() {
		go runStatusUpdater()
	})
	if _, loaded := statusDedup.LoadOrStore(uuid, struct{}{}); loaded {
		return // 同一条消息的重复推送：状态已在途
	}
	select {
	case statusCh <- uuid:
	default:
		// 通道满丢弃：删除去重条目，允许后续推送重试提交。
		statusDedup.Delete(uuid)
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
		// 8n 节：刷盘后释放去重条目（成功失败都释放——失败仅记日志，
		// 状态更新是尽力而为，与去重前语义一致）。
		for _, u := range uuids {
			statusDedup.Delete(u)
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
