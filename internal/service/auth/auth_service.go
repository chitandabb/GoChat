// Package auth 实现双 Token 认证体系（见 docs/design/api.md M1 契约）。
//
//   - Access Token：短效 JWT（HS256，默认 15 分钟），无状态，载荷含 uuid / is_admin。
//   - Refresh Token：不透明随机串，仅以 sha256 哈希为键写入 Redis 白名单，可撤销。
//   - 旋转：续期时旧白名单条目标记 rotated 并签发新对；旧 token 重放触发该用户全量撤销。
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"

	"gochat/internal/config"
	"gochat/internal/dao"
	"gochat/internal/model"
	myredis "gochat/internal/service/redis"
	"gochat/pkg/apperr"
	"gochat/pkg/zlog"

	"go.uber.org/zap"
)

// Claims 是 Access Token 的 JWT 载荷。
type Claims struct {
	UUID    string `json:"uuid"`
	IsAdmin int8   `json:"is_admin"`
	jwt.RegisteredClaims
}

// refreshKeyPrefix 是 Refresh 白名单的 key 前缀（键为 refresh token 的 sha256）。
const refreshKeyPrefix = "auth_refresh:"

// refreshRecord 是 Redis 白名单条目。
type refreshRecord struct {
	UUID     string `json:"uuid"`
	TokenID  string `json:"token_id"`
	IssuedAt int64  `json:"issued_at"`
	Rotated  bool   `json:"rotated"`
}

// IssueTokens 为用户签发双 Token：
//   - accessToken：短效 JWT；
//   - refreshToken：64 位十六进制随机串，白名单写入 Redis（TTL = refreshTokenTTL 天）。
func IssueTokens(uuid string, isAdmin int8) (accessToken, refreshToken string, err error) {
	accessToken, err = signAccessToken(uuid, isAdmin)
	if err != nil {
		return "", "", apperr.SystemError(err)
	}

	refreshToken, err = newRandomToken()
	if err != nil {
		return "", "", apperr.SystemError(err)
	}

	record := refreshRecord{
		UUID:     uuid,
		TokenID:  refreshToken[:16],
		IssuedAt: time.Now().Unix(),
		Rotated:  false,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return "", "", apperr.SystemError(err)
	}
	if err := myredis.SetKeyEx(refreshKey(refreshToken), string(data), refreshTTL()); err != nil {
		zlog.Error(err.Error())
		return "", "", apperr.SystemError(err)
	}
	return accessToken, refreshToken, nil
}

// ParseAccessToken 校验并解析 Access Token（签名 + 过期时间）。
func ParseAccessToken(tokenStr string) (*Claims, error) {
	secret := config.GetConfig().JwtConfig.Secret
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// Refresh 旋转 Refresh Token，返回新的双 Token。
//
// 重放检测：续期时旧白名单条目被标记 rotated（不立即删除）；
// 若旧 token 再次出现，说明 refresh token 已泄露，撤销该用户全部登录态。
func Refresh(refreshToken string) (newAccess, newRefresh string, err error) {
	record, key, err := lookupRefresh(refreshToken)
	if err != nil {
		return "", "", err
	}
	if record.Rotated {
		zlog.Warn("检测到 refresh token 重放，撤销该用户全部登录态", zap.String("uuid", record.UUID))
		if revokeErr := RevokeAll(record.UUID); revokeErr != nil {
			zlog.Error(revokeErr.Error())
		}
		return "", "", apperr.Unauthorized("登录态异常，请重新登录")
	}

	// 标记旧条目已旋转（保留 TTL 以便在窗口内捕获重放），再签发新对。
	record.Rotated = true
	if data, marshalErr := json.Marshal(record); marshalErr == nil {
		_ = myredis.SetKeyEx(key, string(data), refreshTTL())
	}

	return IssueTokens(record.UUID, isAdminOf(record.UUID))
}

// Logout 撤销单个 Refresh Token（登出）。
func Logout(refreshToken string) error {
	record, key, err := lookupRefresh(refreshToken)
	if err != nil {
		return err
	}
	_ = record
	return myredis.DelKeys(key)
}

// RevokeAll 撤销指定用户全部 Refresh Token（登出全部 / 管理员禁用）。
func RevokeAll(uuid string) error {
	keys, err := myredis.ScanKeys(refreshKeyPrefix + "*")
	if err != nil {
		return apperr.SystemError(err)
	}
	var toDelete []string
	for _, key := range keys {
		value, err := myredis.GetKey(key)
		if err != nil {
			zlog.Error(err.Error())
			continue
		}
		var record refreshRecord
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			continue
		}
		if record.UUID == uuid {
			toDelete = append(toDelete, key)
		}
	}
	if len(toDelete) > 0 {
		if err := myredis.DelKeys(toDelete...); err != nil {
			return apperr.SystemError(err)
		}
	}
	return nil
}

// signAccessToken 签发短效 JWT。
func signAccessToken(uuid string, isAdmin int8) (string, error) {
	cfg := config.GetConfig().JwtConfig
	now := time.Now()
	claims := &Claims{
		UUID:    uuid,
		IsAdmin: isAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        newTokenID(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(cfg.AccessTokenTTL) * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.Secret))
}

// lookupRefresh 根据 refresh token 找到白名单记录与 key。
func lookupRefresh(refreshToken string) (*refreshRecord, string, error) {
	if len(refreshToken) < 32 {
		return nil, "", apperr.Unauthorized("登录已过期，请重新登录")
	}
	key := refreshKey(refreshToken)
	value, err := myredis.GetKeyNilIsErr(key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, "", apperr.Unauthorized("登录已过期，请重新登录")
		}
		zlog.Error(err.Error())
		return nil, "", apperr.SystemError(err)
	}
	var record refreshRecord
	if err := json.Unmarshal([]byte(value), &record); err != nil {
		zlog.Error(err.Error())
		return nil, "", apperr.SystemError(err)
	}
	return &record, key, nil
}

// refreshKey 根据 refresh token 的 sha256 生成白名单 key。
// key 不包含 uuid，保证重放场景下也能仅凭 token 定位到条目。
func refreshKey(refreshToken string) string {
	return refreshKeyPrefix + sha256Hex(refreshToken)
}

func refreshTTL() time.Duration {
	return time.Duration(config.GetConfig().JwtConfig.RefreshTokenTTL) * 24 * time.Hour
}

func newRandomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func newTokenID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// isAdminOf 查询用户是否为管理员（Refresh 旋转时重新取最新权限）。
// 续期频率低（正常 7 天一次），直接查库换取权限实时性。
func isAdminOf(uuid string) int8 {
	var user model.UserInfo
	if err := dao.GormDB.Select("is_admin").Where("uuid = ?", uuid).First(&user).Error; err != nil {
		zlog.Error(err.Error())
		return 0
	}
	return user.IsAdmin
}
