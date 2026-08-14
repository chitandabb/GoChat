package gorm

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"gochat/internal/dao"
	"gochat/internal/dto/request"
	"gochat/internal/model"
	myredis "gochat/internal/service/redis"
)

// TestLoginPlaintextLazyUpgrade 验证存量明文密码的懒升级链路：
//   - 明文密码用户可正常登录（明文分支直接比较）；
//   - 登录成功后密码被透明重哈希为 bcrypt（懒升级）；
//   - 升级后再次登录走 bcrypt 分支，仍成功。
func TestLoginPlaintextLazyUpgrade(t *testing.T) {
	if err := dao.Init(); err != nil {
		t.Skipf("skip integration test without database: %v", err)
	}

	const (
		uuid      = "U_TEST_PLAIN_001"
		telephone = "13700000001"
		password  = "123456"
	)

	// 清理历史残留（用户 + 登录失败计数），保证从干净状态开始。
	_ = dao.GormDB.Unscoped().Where("uuid = ? OR telephone = ?", uuid, telephone).Delete(&model.UserInfo{}).Error
	_ = myredis.DelKeys("login_fail_" + telephone)
	defer func() {
		_ = dao.GormDB.Unscoped().Where("uuid = ?", uuid).Delete(&model.UserInfo{}).Error
		_ = myredis.DelKeys("login_fail_" + telephone)
	}()

	// 模拟迁移前的存量用户：密码直接存明文（非 bcrypt）。
	plainUser := &model.UserInfo{
		Uuid:      uuid,
		Nickname:  "plaintext",
		Telephone: telephone,
		Password:  password, // 明文
		CreatedAt: time.Now(),
		Status:    0,
	}
	if err := dao.GormDB.Create(plainUser).Error; err != nil {
		t.Fatalf("create plaintext user: %v", err)
	}

	// 错误密码 → 明文分支必须拒绝。
	if _, err := UserInfoService.Login(request.LoginRequest{Telephone: telephone, Password: "wrong-pass"}); err == nil {
		t.Fatal("expected login failure with wrong password")
	}

	// 正确密码 → 登录成功（明文分支）。
	resp, err := UserInfoService.Login(request.LoginRequest{Telephone: telephone, Password: password})
	if err != nil {
		t.Fatalf("login with plaintext password: %v", err)
	}
	if resp.Uuid != uuid {
		t.Fatalf("uuid mismatch: %s", resp.Uuid)
	}

	// 懒升级：DB 中密码已被重哈希为 bcrypt 且校验通过。
	var upgraded model.UserInfo
	if err := dao.GormDB.First(&upgraded, "uuid = ?", uuid).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !strings.HasPrefix(upgraded.Password, "$2") {
		t.Fatalf("password not upgraded to bcrypt: %q", upgraded.Password)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(upgraded.Password), []byte(password)); err != nil {
		t.Fatalf("upgraded hash mismatch: %v", err)
	}

	// 升级后再次登录 → 走 bcrypt 分支，仍成功。
	if _, err := UserInfoService.Login(request.LoginRequest{Telephone: telephone, Password: password}); err != nil {
		t.Fatalf("login after upgrade: %v", err)
	}
}
