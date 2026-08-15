package redis

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"gochat/internal/config"
	"gochat/pkg/zlog"
	"math/rand"
	"strconv"
	"time"
)

var redisClient *redis.Client
var ctx = context.Background()

func init() {
	conf := config.GetConfig()
	host := conf.RedisConfig.Host
	port := conf.RedisConfig.Port
	password := conf.RedisConfig.Password
	db := conf.RedisConfig.Db
	addr := host + ":" + strconv.Itoa(port)

	redisClient = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
}

func SetKeyEx(key string, value string, timeout time.Duration) error {
	err := redisClient.Set(ctx, key, value, timeout).Err()
	if err != nil {
		return err
	}
	return nil
}

// SetKeyExPipelined 批量写缓存键（Pipeline，合并为一次网络往返）：
// 用于"落库成功后批量置 Kafka 幂等键 done"等高频批量场景——逐条 SETEX
// 在批量路径上每消息一次往返，实测成为消费吞吐瓶颈的一部分（8n 节：
// 64 条/批 × ~1ms Redis + ~10ms Kafka 同步写 = ~770ms/批，消费上限 ~83 msg/s）。
func SetKeyExPipelined(keys []string, value string, timeout time.Duration) error {
	if len(keys) == 0 {
		return nil
	}
	pipe := redisClient.Pipeline()
	for _, key := range keys {
		pipe.Set(ctx, key, value, timeout)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// SetNX 原子"不存在才写入"：返回 true 表示本次调用成功写入（首次），
// false 表示 key 已存在（重复）。用于 Kafka 消费幂等键（topic:partition:offset）。
func SetNX(key string, value string, timeout time.Duration) (bool, error) {
	ok, err := redisClient.SetNX(ctx, key, value, timeout).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// SetNXPipelined 批量原子认领（Pipeline，一次网络往返）：按 keys 顺序
// 返回每个 key 的 SETNX 结果（true=本次写入成功/首次认领，false=键已存在）。
// 8n 节：消费 reader 逐条 SETNX 认领（每消息 1 次 Redis 往返 ~1-2ms）把
// 消费钳制在 ~800 msg/s；整批流水线认领 64 条 = 1 次往返，与批量落库同量级。
func SetNXPipelined(keys []string, value string, timeout time.Duration) ([]bool, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := redisClient.Pipeline()
	cmds := make([]*redis.BoolCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.SetNX(ctx, key, value, timeout)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	res := make([]bool, len(keys))
	for i, cmd := range cmds {
		res[i], _ = cmd.Result()
	}
	return res, nil
}

func GetKey(key string) (string, error) {
	value, err := redisClient.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			zlog.Info("该key不存在")
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func GetKeyNilIsErr(key string) (string, error) {
	value, err := redisClient.Get(ctx, key).Result()
	if err != nil {
		return "", err
	}
	return value, nil
}

func GetKeyWithPrefixNilIsErr(prefix string) (string, error) {
	// 使用 SCAN 迭代匹配的键（禁止 KEYS 全量扫描）
	keys, err := ScanKeys(prefix + "*")
	if err != nil {
		return "", err
	}
	switch len(keys) {
	case 0:
		zlog.Info("没有找到相关前缀key")
		return "", redis.Nil
	case 1:
		zlog.Info(fmt.Sprintln("成功找到了相关前缀key", keys))
		return keys[0], nil
	default:
		zlog.Error("找到了数量大于1的key，查找异常")
		return "", errors.New("找到了数量大于1的key，查找异常")
	}
}

func GetKeyWithSuffixNilIsErr(suffix string) (string, error) {
	// 使用 SCAN 迭代匹配的键（禁止 KEYS 全量扫描）
	keys, err := ScanKeys("*" + suffix)
	if err != nil {
		return "", err
	}
	switch len(keys) {
	case 0:
		zlog.Info("没有找到相关后缀key")
		return "", redis.Nil
	case 1:
		zlog.Info(fmt.Sprintln("成功找到了相关后缀key", keys))
		return keys[0], nil
	default:
		zlog.Error("找到了数量大于1的key，查找异常")
		return "", errors.New("找到了数量大于1的key，查找异常")
	}
}

func DelKeyIfExists(key string) error {
	exists, err := redisClient.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists == 1 { // 键存在
		delErr := redisClient.Del(ctx, key).Err()
		if delErr != nil {
			return delErr
		}
	}
	// 无论键是否存在，都不返回错误
	return nil
}

// Incr 对 key 自增，返回自增后的值。
func Incr(key string) (int64, error) {
	return redisClient.Incr(ctx, key).Result()
}

// Expire 为 key 设置过期时间。
func Expire(key string, timeout time.Duration) error {
	return redisClient.Expire(ctx, key, timeout).Err()
}

// DelKeys 批量删除指定 key（不存在的 key 忽略）。
func DelKeys(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return redisClient.Del(ctx, keys...).Err()
}

// ScanKeys 用 SCAN（非阻塞）遍历匹配 pattern 的 key。
// 生产环境禁止用 KEYS 全量扫描，这里统一走游标迭代。
func ScanKeys(pattern string) ([]string, error) {
	var keys []string
	var cursor uint64
	for {
		batch, nextCursor, err := redisClient.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}

// DelKeysWithPattern 删除匹配 pattern 的 key。
// 生产环境禁用 KEYS（O(N) 阻塞），统一走 SCAN 游标迭代。
func DelKeysWithPattern(pattern string) error {
	keys, err := ScanKeys(pattern)
	if err != nil {
		return err
	}
	return DelKeys(keys...)
}

// DelKeysWithPrefix 删除指定前缀的 key（SCAN 实现，非阻塞）。
func DelKeysWithPrefix(prefix string) error {
	return DelKeysWithPattern(prefix + "*")
}

// DelKeysWithSuffix 删除指定后缀的 key（SCAN 实现，非阻塞）。
func DelKeysWithSuffix(suffix string) error {
	return DelKeysWithPattern("*" + suffix)
}

// DelKeysPipelined 用 Pipeline 批量删除 key：多个 DEL 合并为一次网络往返。
// 用于"一次操作需要失效大量缓存键"的场景（如解散群聊后清理全体成员缓存）。
func DelKeysPipelined(keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	pipe := redisClient.Pipeline()
	for _, key := range keys {
		pipe.Del(ctx, key)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// SetKeyExJitter 写缓存并设置带随机抖动的 TTL。
// 抖动范围 ±20%，避免大量 key 同时过期导致缓存雪崩（cache stampede）。
func SetKeyExJitter(key string, value string, baseTTL time.Duration) error {
	jitter := time.Duration(rand.Int63n(int64(baseTTL)*2/5+1) - int64(baseTTL)/5)
	ttl := baseTTL + jitter
	if ttl <= 0 {
		ttl = baseTTL
	}
	return SetKeyEx(key, value, ttl)
}

func DeleteAllRedisKeys() error {
	var cursor uint64 = 0
	for {
		keys, nextCursor, err := redisClient.Scan(ctx, cursor, "*", 0).Result()
		if err != nil {
			return err
		}
		cursor = nextCursor

		if len(keys) > 0 {
			_, err := redisClient.Del(ctx, keys...).Result()
			if err != nil {
				return err
			}
		}

		if cursor == 0 {
			break
		}
	}
	return nil
}
