package main

import (
	"fmt"
	"time"
	"gochat/internal/config"
	"gochat/internal/dao"
	"gochat/internal/model"
	myredis "gochat/internal/service/redis"
	"gochat/pkg/enum/message/message_status_enum"
)

func main() {
	config.GetConfig()
	dao.MustInit()
	// DB insert 200 次
	start := time.Now()
	for i := 0; i < 200; i++ {
		m := model.Message{Uuid: fmt.Sprintf("MT2%05d", i), SessionId: "S0000000000000000000", Type: 0, Content: "x", SendId: "U0000000000000000001", SendName: "t", SendAvatar: "/static/a.png", ReceiveId: "U0000000000000000002", Status: message_status_enum.Unsent, CreatedAt: time.Now()}
		if err := dao.GormDB.Create(&m).Error; err != nil {
			fmt.Println("ERR:", err)
		}
	}
	fmt.Printf("DB insert: %.2f ms/op\n", float64(time.Since(start).Microseconds())/200000)
	// Redis Get+Set 200 次
	start = time.Now()
	for i := 0; i < 200; i++ {
		_, _ = myredis.GetKeyNilIsErr("perf_key")
		_ = myredis.SetKeyEx("perf_key", `[]`, time.Minute)
	}
	fmt.Printf("Redis Get+Set: %.2f ms/op\n", float64(time.Since(start).Microseconds())/200000)
}