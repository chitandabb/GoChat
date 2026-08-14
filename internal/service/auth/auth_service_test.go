package auth

import (
	"testing"

	"gochat/internal/config"
	"gochat/internal/dao"
	myredis "gochat/internal/service/redis"
)

// setupAuthTest 初始化依赖（配置 + 数据库，均需本地 MySQL/Redis）。
func setupAuthTest(t *testing.T) {
	t.Helper()
	_ = config.GetConfig()
	dao.MustInit()
}

// TestRefreshRotationAndReuseDetection 验证双 Token 旋转与重放检测：
//   - 旧 refresh 续期后仍可旋转（正常续期）；
//   - 已被旋转过的旧 refresh 再次使用 → 401 且撤销该用户全部登录态。
func TestRefreshRotationAndReuseDetection(t *testing.T) {
	setupAuthTest(t)

	// 清理历史测试数据，保证白名单扫描干净。
	keys, err := myredis.ScanKeys(refreshKeyPrefix + "*")
	if err != nil {
		t.Fatalf("scan keys: %v", err)
	}
	if len(keys) > 0 {
		_ = myredis.DelKeys(keys...)
	}

	uuid := "U_TEST_AUTH_001"
	access1, refresh1, err := IssueTokens(uuid, 0)
	if err != nil {
		t.Fatalf("issue tokens: %v", err)
	}
	if access1 == "" || refresh1 == "" {
		t.Fatal("empty token")
	}

	// Access Token 应可解析且载荷正确。
	claims, err := ParseAccessToken(access1)
	if err != nil {
		t.Fatalf("parse access: %v", err)
	}
	if claims.UUID != uuid {
		t.Fatalf("claims uuid mismatch: %s", claims.UUID)
	}

	// 正常续期：旧 refresh 换新对。
	access2, refresh2, err := Refresh(refresh1)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if access2 == "" || refresh2 == "" || refresh2 == refresh1 {
		t.Fatal("rotation failed")
	}

	// 旋转后的新 refresh 应仍然有效（可再次续期）。
	if _, _, err := Refresh(refresh2); err != nil {
		t.Fatalf("second refresh: %v", err)
	}

	// 重放检测：再次使用已被旋转的旧 refresh → 必须拒绝。
	if _, _, err := Refresh(refresh1); err == nil {
		t.Fatal("expected reuse rejection, got success")
	}

	// 重放触发全量撤销：此时新 refresh 也应全部失效。
	if _, _, err := Refresh(refresh2); err == nil {
		t.Fatal("expected revocation of all tokens after reuse, got success")
	}

	// 清理。
	_ = RevokeAll(uuid)
}

// TestRevokeAll 验证管理员禁用场景的全量撤销。
func TestRevokeAll(t *testing.T) {
	setupAuthTest(t)
	uuid := "U_TEST_AUTH_002"

	_, refresh1, err := IssueTokens(uuid, 0)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := RevokeAll(uuid); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	if _, _, err := Refresh(refresh1); err == nil {
		t.Fatal("expected revoked refresh to fail")
	}
}

// TestParseAccessTokenInvalid 验证无效 / 篡改 token 被拒绝。
func TestParseAccessTokenInvalid(t *testing.T) {
	setupAuthTest(t)
	if _, err := ParseAccessToken("not-a-token"); err == nil {
		t.Fatal("expected invalid token error")
	}
}
