package dao

import (
	"gochat/internal/dao"
	"gochat/internal/model"
	"gochat/pkg/util/random"
	"strconv"
	"testing"
	"time"
)

func TestCreate(t *testing.T) {
	if err := dao.Init(); err != nil {
		t.Skipf("skip integration test without database: %v", err)
	}

	userInfo := &model.UserInfo{
		Uuid:      "U" + strconv.Itoa(random.GetRandomInt(11)),
		Nickname:  "apylee",
		Telephone: "1390000" + strconv.Itoa(random.GetRandomInt(4)), // 手机号唯一索引，用随机号避免撞库
		Email:     "1212312312@qq.com",
		Password:  "123456",
		CreatedAt: time.Now(),
		IsAdmin:   1,
	}
	if err := dao.GormDB.Create(userInfo).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
}
