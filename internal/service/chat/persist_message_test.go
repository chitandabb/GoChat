package chat

import (
	"testing"
	"time"

	"gochat/internal/dao"
	"gochat/internal/model"
)

// TestPersistMessageIdempotent 验证 uuid 唯一键幂等：
// 同一条消息重复落库（Kafka 重复消费 / 重复投递 / 客户端重试）第二次应返回
// duplicate=true 且不报错，保证"先落库后推送"链路不会因重复投递产生重复消息。
func TestPersistMessageIdempotent(t *testing.T) {
	if err := dao.Init(); err != nil {
		t.Skipf("skip integration test without database: %v", err)
	}

	msg := &model.Message{
		Uuid:       "MSG_TEST_IDEM_001",
		SessionId:  "SES_TEST_IDEM_001",
		Type:       0,
		Content:    "idempotent test",
		SendId:     "U_TEST_SEND_001",
		SendName:   "sender",
		SendAvatar: "avatar",
		ReceiveId:  "U_TEST_RECV_001",
		Status:     0,
		CreatedAt:  time.Now(),
	}

	// 清理可能的历史残留，保证测试从干净状态开始。
	if err := dao.GormDB.Unscoped().Where("uuid = ?", msg.Uuid).Delete(&model.Message{}).Error; err != nil {
		t.Fatalf("cleanup before: %v", err)
	}
	defer func() {
		_ = dao.GormDB.Unscoped().Where("uuid = ?", msg.Uuid).Delete(&model.Message{}).Error
	}()

	// 第一次落库：正常插入，非重复。
	dup, err := persistMessage(msg)
	if err != nil {
		t.Fatalf("first persist: %v", err)
	}
	if dup {
		t.Fatal("first persist should not be duplicate")
	}

	// 同 uuid 再次落库（模拟重复投递）：幂等跳过，不报错。
	dup, err = persistMessage(msg)
	if err != nil {
		t.Fatalf("second persist: %v", err)
	}
	if !dup {
		t.Fatal("second persist should be duplicate (1062 unique key)")
	}

	// 数据库里应只有一条记录。
	var count int64
	if err := dao.GormDB.Model(&model.Message{}).Where("uuid = ?", msg.Uuid).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row, got %d", count)
	}
}
