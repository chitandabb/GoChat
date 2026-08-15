package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gochat/internal/dao"
	"gochat/internal/dto/request"
	"gochat/internal/model"
	mykafka "gochat/internal/service/kafka"
	myredis "gochat/internal/service/redis"
	"gochat/pkg/constants"
	"gochat/pkg/enum/message/message_status_enum"
	"gochat/pkg/enum/message/message_type_enum"
	"gochat/pkg/util/random"
	"gochat/pkg/zlog"
	"sync"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// KafkaServer 是 Kafka 模式的消息服务器。
//
// 设计（见 docs/design/messaging.md）：Kafka 模式与 channel 模式必须满足同一套
// 投递语义。因此这里不再复制一份分发逻辑，而是让 Kafka 消费循环把消息投递到
// 内部 *Server 的推送路径，登录 / 登出 / 下行推送 / 慢客户端治理全部复用
// Server 的既有实现。
//
// 批量落库（2026-08-15）：分发循环的串行落库是单实例吞吐上限的瓶颈（本机
// Docker Desktop MySQL 单条 INSERT ≈6.8ms，消费上限 ≈147 msg/s，实测见
// docs/notes/压测报告.md 8e 节）。消费端按批攒集、批量 INSERT 落库，落库成功后
// 经 chat_push 广播进入推送（8k 节，跨实例可达），把"落库"从串行瓶颈中移出；
// "先落库、后推送"语义不变。
type KafkaServer struct {
	server *Server

	// 分区级 in-flight 记账（8m 节）：从幂等认领（claimOnce）到落库完成
	// （含失败重试）期间，消息 offset 计入本集合；提交时该分区只允许提交
	// "最早 in-flight offset 之前"的位置（连续分区水位）——Kafka 提交是分区级
	// 单调的（提交 N 即标记 0..N 全部已消费），若允许越过未完成消息提交，
	// 失败/在途消息会被同分区后续成功消息的提交静默"带过"永久丢失
	// （实测 MySQL 故障恢复后 10000 丢 1，见 docs/notes/压测报告.md 8l 节；
	// claim 即记账封死 msgCh 在途窗口，见 8m 节）。
	mu              sync.Mutex
	inFlightOffsets map[int]map[int64]struct{}
	// 被连续水位暂扣、等待提交的已完成消息（done 跳过 / 批量成功被在途消息
	// 拖住），由 consumeLoop 的 ticker 驱动 commitPending 重试提交。
	pendingCommits []kafka.Message

	// Transmit 路径（群聊/文件/音视频）落库失败消息的重试队列（8m 节）：
	// dispatchOnce 落库失败后由 KafkaRetry 回调追加（非阻塞，不占分发循环），
	// consumeLoop 每个 tick 全部重投递到分发循环重试——失败消息不丢、不依赖
	// 重启重放；MySQL 恢复后自动追平。消息在重试期间保持 in-flight，
	// offset 由连续水位保护，不会越过它提交。
	retryMu    sync.Mutex
	retryQueue []kafka.Message
}

var KafkaChatServer *KafkaServer

// 推送事件异步通道（8n 节）：publishPush 只入队，drainer 后台统一写
// chat_push。容量 4096 ≈ 64 批积压余量；满则丢弃（语义同同步版失败）。
var (
	pushCh     = make(chan *PersistedText, 4096)
	pushChOnce sync.Once
)

func init() {
	if KafkaChatServer == nil {
		k := &KafkaServer{
			server: &Server{
				Clients:  make(map[string]*Client),
				mutex:    &sync.Mutex{},
				Transmit: make(chan *TransmitData, constants.CHANNEL_SIZE),
				Login:    make(chan *Client, constants.CHANNEL_SIZE),
				Logout:   make(chan *Client, constants.CHANNEL_SIZE),
			},
			inFlightOffsets: make(map[int]map[int64]struct{}),
		}
		// 落库成功（含 duplicate）：清除 in-flight 并提交 offset（manual commit）。
		// 落库失败：登记重试队列（保持 in-flight，offset 不提交，水位不越过）。
		// 由此 Transmit 路径与文本批量路径共享同一套"落库成功才提交 + 失败重试"
		// 语义（8m 节修复：此前 Transmit 路径进通道即提交 offset，"已读未落库"
		// 窗口依然存在，见 docs/notes/压测报告.md 8m 节）。
		k.server.KafkaCommit = func(m kafka.Message) {
			k.clearInFlight(m)
			k.commitSafe([]kafka.Message{m})
		}
		k.server.KafkaRetry = func(m kafka.Message) {
			// 落库失败登记重试（保持 in-flight、offset 不提交，连续水位不越过）；
			// 失败原因已由 dispatchOnce 的 error 日志记录，这里不重复刷屏。
			k.retryMu.Lock()
			k.retryQueue = append(k.retryQueue, m)
			k.retryMu.Unlock()
		}
		KafkaChatServer = k
	}
}

// 批量落库参数（与 status_updater 同一套攒批策略）：
// 单批最多 64 条，或最长 10ms 刷盘，二者先到先触发。
// 注：flush 窗口权衡吞吐与延迟——窗口越大批量收益越高但低流量下消息滞留越久；
// 压测显示 100ms 窗口会把容量内 P50 从 ~29ms 抬到 ~76ms，10ms 窗口在 200 msg/s
// 下仍能追平消费（实测见 docs/notes/压测报告.md 8e 节），故取 10ms。
const (
	persistBatchSize  = 64
	persistFlushEvery = 10 * time.Millisecond
	// flushTailEvery 8n 节：批次未攒满时的兜底刷盘时限。此前每个 tick
	// （10ms）都把未攒满的小批次刷掉：每次刷盘有 ~10ms 固定开销（INSERT
	// 调用 + done 键 Pipeline + 提交），实测 1000 msg/s 下 946 次刷盘中
	// 903 次 ≤15 条，消费被钳制在 ~800 msg/s、P50 虚高到 420ms。现在只有
	// 批次攒满（64）或等待超过本时限才刷盘——满载时全部整批刷（64 条/
	// ~16ms ≈ 4000 msg/s），低流量时单条消息延迟 ≤ 本时限。
	// 8n 复测：10ms 窗口（本值原为 30ms）在 1000 msg/s 下批均 ~10 条、
	// 刷盘 ~10ms 仍能追平消费（~100 批/s），端到端 P50 由 ~97ms 降至
	// ~75ms（压测实测）；低流量延迟上限同窗口收敛。
	flushTailEvery = 10 * time.Millisecond
	// claimBatchSize 8n 节：reader 批量认领大小——整批流水线 SETNX 认领，
	// 与批量落库同量级，不再逐条 Redis 往返。
	claimBatchSize = 64
	// groupFanoutInterval 群消息扇出节流间隔（8n 节）：见 pushConsumeLoop
	// 的节流注释——群事件逐事件分发上限实测 ~2000/s，远超慢客户端连接
	// 排空速度，需节流避免 SendBack 洪峰丢弃；同时 1000 事件需在压测
	// 10s 排空窗口内完成（含 ~1s 管线前置），间隔取 4ms（事件 6ms ≈ 167/s，
	// 实测 400 成员全量扇出 100% 送达）。
	groupFanoutInterval = 4 * time.Millisecond
)

// Start 启动 Kafka 消费循环、推送广播消费循环与内部 Server 分发循环。
func (k *KafkaServer) Start() {
	// 消费 Kafka chat 消息，攒批落库后投递到与 channel 模式相同的推送路径。
	// 崩溃恢复：未消费消息由 Kafka 保留；重复消费由消息 uuid 唯一键幂等去重。
	go k.consumeLoop()
	// 推送事件广播消费（8k 节）：每个实例消费全量 push topic，查本地
	// Clients map 推送——消费侧多实例分摊分区时跨实例推送也能送达。
	go k.pushConsumeLoop()
	k.server.Start()
}

// pushConsumeLoop 消费推送事件广播 topic：落库成功的消息（含批量路径与
// Transmit 路径）写回 chat_push，本实例消费后走与 channel 模式相同的
// dispatchPersistedText 推送路径。每个实例独立消费组（进程唯一）→ 广播语义：
// 一条推送事件被所有实例消费，但只有持有目标用户连接的实例真正下发
// （查本地 Clients map 命中才推，miss 自然跳过）——多实例下消息不重不漏。
func (k *KafkaServer) pushConsumeLoop() {
	defer func() {
		if r := recover(); r != nil {
			zlog.Error(fmt.Sprintf("kafka push consume panic: %v", r))
		}
	}()
	// 8n 复测：ReadMessage 改为 FetchMessage + 批量提交。ReadMessage 每条
	// 自动提交一次 offset（OffsetCommit 往返 ~1.5ms，机器负载下更高），
	// 把 push 消费速率钳制在 ~600 事件/s（chat 侧 FetchMessage 不提交、
	// 攒批提交，同 broker 下稳定 ~1000 msg/s——对照实验确认瓶颈在提交
	// 往返而非 fetch）。批量提交每 64 条一次往返，速率恢复由 fetch+
	// dispatch 主导（实测 ~2000 事件/s）；崩溃重放窗口从 1 条扩大到
	// ≤64 条，与 chat 侧批量提交窗口一致（推送无幂等键，重放产生重复
	// 推送，语义与 ReadMessage 的自动提交窗口同类，可接受）。
	var pending []kafka.Message
	const pushCommitBatch = 64
	for {
		msg, err := mykafka.KafkaService.PushReader.FetchMessage(context.Background())
		if err != nil {
			zlog.Error("推送事件消费失败: " + err.Error())
			continue
		}
		pending = append(pending, msg)
		var pt PersistedText
		if err := json.Unmarshal(msg.Value, &pt); err != nil {
			zlog.Error(fmt.Sprintf("推送事件 unmarshal: %v", err))
			continue
		}
		// 与 channel 模式同一推送路径：查本地 Clients 命中才下发，
		// 缓存失效删除同源（写路径统一 Cache-Aside）。
		k.server.dispatchPersistedText(time.Now(), pt.Req, pt.Msg)
		// 8n 节：群消息扇出节流——群事件（400 成员扇出）逐事件分发上限实测
		// ~2000/s，而慢客户端连接排空仅 ~450-500 msg/s（TCP_NODELAY 后），
		// 洪峰会打满 SendBack 队列误判慢客户端丢弃（实测 500/s 扇出 90.89%、
		// 无节流 24.38%）。单聊事件（2 条推送）无需节流。
		if pt.Msg.ReceiveId != "" && pt.Msg.ReceiveId[0] == 'G' {
			time.Sleep(groupFanoutInterval)
		}
		if len(pending) >= pushCommitBatch {
			if err := mykafka.KafkaService.PushReader.CommitMessages(context.Background(), pending...); err != nil {
				zlog.Error("推送事件提交 offset 失败: " + err.Error())
			}
			pending = pending[:0]
		}
	}
}

// consumeLoop 消费 chat topic 并攒批落库。落库成功（非重复）的文本消息经
// chat_push 广播进入推送（8k 节）；其余类型（文件 / 音视频信令等）走原
// Transmit 路径，由分发循环按既有逻辑落库 / 转发。
//
// 实现要点（2026-08-15 修复）：读取与攒批解耦。此前 ReadMessage 与 ticker
// flush 在同一循环内串行：消息流停止时 ReadMessage 阻塞在 fetch 上，ticker
// 分支永远等不到执行，最后一批（<64 条）消息无限期滞留内存——不落库、不推送，
// 且 kafka-go 的 ReadMessage 返回消息即提交 offset（FetchMessage + Commit），
// 若此刻进程崩溃，这批消息既不在 DB、offset 又已提交，将永久丢失（实测见
// docs/notes/压测报告.md 8i 节：30 对 × 50 条尾部 3 条滞留，被后续消息唤醒
// 后才落库）。改为独立 Reader goroutine + select 双路监听后，无论消息流是否
// 停止，ticker 都能按时触发 flush，滞留窗口收缩到 10ms 以内。
func (k *KafkaServer) consumeLoop() {
	defer func() {
		if r := recover(); r != nil {
			zlog.Error(fmt.Sprintf("kafka consume panic: %v", r))
		}
	}()

	// Reader goroutine：FetchMessage 不自动提交 offset（8k 节修复），读出后
	// 立即移交认领队列（fetch 与认领解耦，8n 节修复，见下）；已落库完成
	// （done）的直接跳过并提交 offset；放行的消息登记 in-flight（8m 节：
	// 从认领起即受连续水位保护，封死 "msgCh 在途未落库" 窗口）后进入处理
	// 管线，offset 由落库/转发成功后才显式提交（manual commit）——"已读
	// 未落库"的消息 offset 未提交，崩溃后重放按 pending 重试，不丢失
	// （此前 ReadMessage 返回即提交 offset，双实例 kill 实测丢 68/1500 条，
	// 见 docs/notes/压测报告.md 8k 节）。
	msgCh := make(chan kafka.Message, persistBatchSize)
	claimCh := make(chan kafka.Message, claimBatchSize)
	// fetch goroutine：持续 FetchMessage，读出后立即移交认领队列，不在
	// 读取侧攒批等待。8n 修复（延迟回归根因）：此前在同一 goroutine 里先
	// 攒满 64 条再统一认领——低流量下首条消息被后续消息拖住最长
	// 64×10ms≈640ms（100 msg/s 实测端到端 P50 151ms、max 686ms 恰为整批
	// 等待；pushAge/wsWrite 各段仅 ~29ms，缺口全在 reader 攒批窗口）。
	// 拆开后 fetch→认领 ≤1-2ms，认领批的大小由认领侧的实际到达决定。
	go func() {
		for {
			m, err := mykafka.KafkaService.ChatReader.FetchMessage(context.Background())
			if err != nil {
				zlog.Error(err.Error())
				continue
			}
			claimCh <- m // 通道满时阻塞（msgCh→consumeLoop 反压链），不再拉取新消息
		}
	}()
	// claim goroutine：攒批流水线 SETNX 认领（pending 状态机语义与逐条
	// claimOnce 完全一致：首次 SETNX 成功放行；已 done 跳过并提交 offset；
	// pending 残留放行重试，DB kafka_key 唯一索引兜底不重复落库）。
	// 批大小 = 认领队列当前积压（低流量 1-2 条/次，高流量接近 64 条/次），
	// 流水线 64 条 = 1 次往返（实测不再钳制 reader，与批量落库 ~4000 msg/s
	// 同量级），低流量下每次认领仅多 1 次 ~1-2ms 往返。
	go func() {
		for {
			p := <-claimCh
			batch := make([]kafka.Message, 0, claimBatchSize)
			batch = append(batch, p)
		collect:
			for len(batch) < claimBatchSize {
				select {
				case q := <-claimCh:
					batch = append(batch, q)
				default:
					break collect
				}
			}
			keys := make([]string, len(batch))
			for i := range batch {
				keys[i] = "kafka_dedup:" + dedupKey(batch[i])
			}
			claimed, claimErr := myredis.SetNXPipelined(keys, "pending", 24*time.Hour)
			if claimErr != nil {
				// 幂等键写入失败时保守放行（宁可重复也不丢消息）；重复由 DB 唯一索引兜底。
				zlog.Error("消费幂等键批量写入失败，放行", zap.Error(claimErr))
			}
			for i, m := range batch {
				ok := true
				if claimErr == nil && !claimed[i] {
					// 键已存在：区分 done（已落库完成）与 pending（崩溃残留，需重试）。
					state, err := myredis.GetKey(keys[i])
					if err != nil {
						zlog.Error("消费幂等键查询失败，放行", zap.String("key", keys[i]), zap.Error(err))
					} else if state == "done" || state == "1" {
						// done / 8g 时代旧格式键：已落库完成，重复消费跳过。
						ok = false
					}
					// pending（或其他残留值）：上次认领后未完成落库（崩溃），放行重试；
					// DB kafka_key 唯一索引保证同一条消息不会重复落库。
				}
				if !ok {
					zlog.Info("重复消费跳过", zap.String("topic", m.Topic),
						zap.Int("partition", m.Partition), zap.Int64("offset", m.Offset))
					// 已落库完成：跳过并提交 offset，避免重放风暴。
					// 提交同样受连续分区水位保护（同分区存在 in-flight 消息时被
					// 暂扣进 pendingCommits，水位推进后由 commitPending 补提交）。
					k.commitSafe([]kafka.Message{m})
					continue
				}
				k.registerInFlight(m) // 8m 节：认领即记账（连续水位起点）
				msgCh <- m            // 通道满时阻塞认领 goroutine，形成天然反压（不再认领新消息）
			}
		}
	}()

	batch := make([]kafka.Message, 0, persistBatchSize)
	batchStart := time.Now()
	ticker := time.NewTicker(persistFlushEvery)
	defer ticker.Stop()

	flush := func() {
		// 1) 水位推进后，补提交被暂扣的已完成消息（done 跳过/批量成功）。
		k.commitPending()
		// 2) 重投 Transmit 路径落库失败的消息（8m 节重试闭环，保持 in-flight
		//    不丢；MySQL 恢复后自动追平）。
		k.retryMu.Lock()
		retries := k.retryQueue
		k.retryQueue = nil
		k.retryMu.Unlock()
		if len(retries) > 0 {
			zlog.Info("重投 Transmit 失败消息", zap.Int("count", len(retries)))
		}
		for _, m := range retries {
			// 8m 节：go.mod 为 go 1.20，range 循环变量复用同一地址——直接传 &m
			// 会让队列里所有 TransmitData.KafkaMsg 指向同一条消息（实测：重投
			// 循环中 retryQueue 被同一 offset 刷屏、真实消息 in-flight 残留、
			// 水位卡死、LAG 不降）。显式拷贝保证每条消息的 KafkaMsg 唯一。
			km := m
			k.server.SendMessageToTransmit(km.Value, dedupKey(km), &km)
		}
		// 3) 攒批落库；失败消息重新入队重试（8l 节：offset 提交受连续水位
		//    保护，重试成功后才允许推进）。
		if len(batch) == 0 {
			return
		}
		// 8n 节：批次未攒满且未超尾部时限时继续攒批——10ms tick 逐次刷小批次
		// 的固定开销（~10ms/次）实测把消费钳制在 ~800 msg/s；满载时全部以
		// 整批（64 条）落库，低流量时单条消息延迟 ≤ flushTailEvery。
		if len(batch) < persistBatchSize && time.Since(batchStart) < flushTailEvery {
			return
		}
		msgs := batch
		batch = make([]kafka.Message, 0, persistBatchSize)
		batchStart = time.Now()
		batch = append(k.persistBatch(msgs), batch...)
	}

	for {
		select {
		case <-ticker.C:
			// 独立于消息到达触发刷盘：低流量 / 无流量时也能及时落库。
			flush()
		case kafkaMessage := <-msgCh:
			if len(batch) == 0 {
				batchStart = time.Now()
			}
			// 消息已通过幂等认领（Reader goroutine 提前 claim）。文本消息（单聊 +
			// 群聊）统一走批量落库（吞吐瓶颈在落库，批量收益最大；单条 INSERT
			// 每次提交一次 fsync ~10ms，批量 64 条分摊到 ~0.25ms/条——实测群测
			// 1000 条串行 Transmit 需 12.4s，超压测 10s 排空窗口，见 8n 节）。
			// 群聊不再走 Transmit 串行路径：8e 节"批量洪峰打满 SendBack 队列"
			// 的前提是旧同步推送路径（每消息 ~10ms 写 chat_push），现已改为异步
			// drainer + push 消费者串行分发（~400 事件/s），扇出天然限速，
			// 洪峰不再存在。
			// 文件 / 音视频信令保持原 Transmit 路径。
			var req request.ChatMessageRequest
			if err := json.Unmarshal(kafkaMessage.Value, &req); err == nil &&
				req.Type == message_type_enum.Text {
				batch = append(batch, kafkaMessage)
				if len(batch) >= persistBatchSize {
					flush()
				}
			} else {
				// 8m 节：不再在这里提交 offset——落库发生在分发循环（server.go
				// dispatchOnce），落库成功由 KafkaCommit 回调提交，失败由
				// KafkaRetry 回调登记重试。"先落库、后提交"覆盖全部消息类型。
				k.server.SendMessageToTransmit(kafkaMessage.Value, dedupKey(kafkaMessage), &kafkaMessage)
			}
		}
	}
}

// dedupKey 生成 Kafka 消息的幂等键（topic:partition:offset:消息时间戳）。
// 时间戳精确到毫秒（Kafka 消息时间戳粒度）；重放同一条消息时时间戳不变，
// topic 重建后新消息时间戳必然不同，两者不会互相误判（见 8i 节）。
// 该键同时写入 Redis 幂等键与 message.kafka_key 唯一索引，两者一致。
func dedupKey(msg kafka.Message) string {
	ts := msg.Time.UnixMilli()
	if ts <= 0 {
		// 异常情况（无时间戳）：退化为 (topic, partition, offset) 去重。
		ts = 0
	}
	return fmt.Sprintf("%s:%d:%d:%d", msg.Topic, msg.Partition, msg.Offset, ts)
}

// claimOnce 以 Redis 幂等键原子认领一条 Kafka 消息（pending/done 状态机）。
// 返回 true 表示需要落库处理（首次认领，或上次认领后崩溃残留 pending 需重试）；
// false 表示已落库完成（done），跳过。
//
// 8j 节修复：此前"SETNX 成功即放行、失败即跳过"的语义存在崩溃丢消息窗口——
// 消息已认领（键已写）但落库前进程崩溃，offset 已提交、DB 无记录、重启后重放
// 被幂等键误杀，消息永久丢失（实测分析见 docs/notes/压测报告.md 8j 节）。
// 状态机把"认领"与"完成"分离：落库成功后置 done；重放时 pending（未完成）
// 放行重试，由 message.kafka_key 唯一索引保证不重复落库——崩溃任意时刻，
// 要么 done 跳过、要么 pending 重试且 DB 索引兜底，丢消息窗口彻底封死。
// TTL 取 24h：覆盖 offset 回退窗口，且防止键无限膨胀；键过期后重放退化为
// "键不存在"→ 首次认领 → DB 唯一索引兜底（不重复、不丢失）。
func (k *KafkaServer) claimOnce(msg kafka.Message) bool {
	key := "kafka_dedup:" + dedupKey(msg)
	ok, err := myredis.SetNX(key, "pending", 24*time.Hour)
	if err != nil {
		// 幂等键写入失败时保守放行（宁可重复也不丢消息）；重复由 DB 唯一索引兜底。
		zlog.Error("消费幂等键写入失败，放行", zap.String("key", key), zap.Error(err))
		return true
	}
	if ok {
		return true // 首次认领：pending 已写入，放行落库
	}
	// 键已存在：区分 done（已落库）与 pending（崩溃残留，需重试）。
	state, err := myredis.GetKey(key)
	if err != nil {
		// 查询失败保守放行，DB 唯一索引兜底。
		zlog.Error("消费幂等键查询失败，放行", zap.String("key", key), zap.Error(err))
		return true
	}
	if state == "done" || state == "1" {
		// done：已落库完成，重复消费跳过。
		// "1"：8g 时代旧格式键（当时语义为"已认领即已处理"，实测重放零新增，
		// 见 docs/notes/压测报告.md 8g 节）。旧键没有 done 标记，若按 pending
		// 放行重试会重复落库（实测见 8j 节：全量重放旧消息 2 分钟产生 24604
		// 个重复组，因为旧行的 kafka_key 为 NULL，DB 唯一索引拦不住），
		// 故旧键等价于 done。
		return false
	}
	// pending（或其他残留值）：上次认领后未完成落库（崩溃），放行重试；
	// DB kafka_key 唯一索引保证同一条消息不会重复落库。
	zlog.Info("消费幂等键为 pending（上次未完成落库），重试", zap.String("key", key))
	return true
}

// markDoneKey 落库成功后把幂等键置为 done，表示本条 Kafka 消息已完成处理。
// 失败仅记录日志：即使 done 未写入，重放也会由 DB kafka_key 唯一索引兜底（不重复）。
func markDoneKey(key string) {
	if err := myredis.SetKeyEx("kafka_dedup:"+key, "done", 24*time.Hour); err != nil {
		zlog.Error("置幂等键 done 失败", zap.String("key", key), zap.Error(err))
	}
}

// markDoneKeys 批量置幂等键 done（Pipeline 一次往返，8n 节）：批量落库
// 成功后整批置位，避免 64 次逐条 SETEX 串在落库主链路。语义与逐条一致：
// 失败仅记日志，DB kafka_key 唯一索引兜底。
func markDoneKeys(keys []string) {
	if len(keys) == 0 {
		return
	}
	if err := myredis.SetKeyExPipelined(keys, "done", 24*time.Hour); err != nil {
		zlog.Error("批量置幂等键 done 失败", zap.Int("n", len(keys)), zap.Error(err))
	}
}

// textWithRaw 配对保存一条文本消息的原始请求、构造好的实体与 Kafka 消息本身
// （后两者用于落库后置幂等键 done）。
type textWithRaw struct {
	req *request.ChatMessageRequest
	msg *model.Message
	km  kafka.Message
}

// publishPush 把一条已落库消息的推送事件写回 chat_push 广播（8k 节）。
// 每个实例消费全量推送事件、查本地 Clients map 下发；发送失败仅记日志
// （推送是加速通道，落库已是事实，重连后会话历史拉取兜底）。
//
// 8n 节修复（异步化 + drainer 批量提交）：此前同步 WriteMessages
// （BatchSize=10/BatchTimeout=10ms，acks=one）在空 writer 上每次约 10ms——
// 批量路径 64 条/批 = ~640ms 串在落库主链路里，消费吞吐被钉死在 ~83 msg/s
// （压测复现：P50=6.3s、回显率 15%）。现在只入队（非阻塞），后台 drainer
// 攒批后一次提交多条：主链路付出入队成本；drainer 若逐条提交仍会每个
// 10ms 批窗各等一次（~100 msg/s 上限），批量提交让 writer 的 BatchSize=10
// 真正生效（64 条 ≈ 7 次批量往返 ≈ 10-15ms）。
// 通道满（积压）时丢弃并记错误：与同步版本语义一致——推送失败不影响
// 消息已落库的事实，重连后会话历史拉取兜底（注释承诺"不能阻塞落库主链路"
// 现在才真正成立）。
func publishPush(pt *PersistedText) {
	pushChOnce.Do(func() {
		go pushDrainLoop()
	})
	select {
	case pushCh <- pt:
	default:
		zlog.Error("推送事件通道已满，丢弃推送（落库已完成，历史拉取兜底）")
	}
}

// pushDrainLoop 后台批量写 chat_push（8n 节）：一次 WriteMessages 提交
// 多条，让 kafka-go writer 的 BatchSize=10/BatchTimeout=10ms 真正生效。
func pushDrainLoop() {
	const maxBatch = 64
	for {
		p := <-pushCh
		batch := make([]*PersistedText, 0, maxBatch)
		batch = append(batch, p)
	collect:
		for len(batch) < maxBatch {
			select {
			case q := <-pushCh:
				batch = append(batch, q)
			default:
				break collect
			}
		}
		writePushBatch(batch)
	}
}

// writePushBatch 批量同步写入 chat_push（drainer 专用）：一次调用提交
// 多条消息，由 writer 内部按 BatchSize=10 分批发送。
// key 用 ReceiveId（8n 节）：此前用消息 Uuid 哈希分区——同一接收者的推送
// 事件散布在 3 个分区，push 消费者跨分区交错读取，会话内顺序被打乱
// （order 压测实测：乱序 1260、缺失 2249）。同会话消息同分区 → 分区内
// 保序 → 接收端回显严格递增。
func writePushBatch(pts []*PersistedText) {
	msgs := make([]kafka.Message, 0, len(pts))
	for _, pt := range pts {
		payload, err := json.Marshal(pt)
		if err != nil {
			zlog.Error(fmt.Sprintf("推送事件 marshal: %v", err))
			continue
		}
		key := pt.Req.ReceiveId
		if key == "" {
			key = pt.Req.SessionId
		}
		msgs = append(msgs, kafka.Message{Key: []byte(key), Value: payload})
	}
	if len(msgs) == 0 {
		return
	}
	if err := mykafka.KafkaService.PushWriter.WriteMessages(context.Background(), msgs...); err != nil {
		zlog.Error("推送事件批量写入失败: " + err.Error())
	}
}

// persistBatch 批量落库一批文本消息：解析 → 构造 model.Message → 批量 INSERT；
// 批量失败（如含重复 kafka_key/uuid）时整批回退逐条 INSERT 以保持幂等语义。
// 落库成功的消息经 chat_push 广播进入推送（跨实例可达），并置幂等键 done
// （8j 节：done 表示已完成落库，重放时跳过；未完成则保持 pending 重试）。
//
// offset 提交（manual commit，8k 节）：只有落库成功的消息才提交 offset。
// 连续分区水位（8l/8m 节）：消息自认领起即登记 in-flight（Reader goroutine），
// 落库成功/duplicate 时清除并提交；失败保持 in-flight、返回重新入队重试——
// 提交只允许到达"最早 in-flight offset 之前"，防止后续成功消息的提交"带过"
// 失败消息导致其永久丢失（实测：MySQL 故障恢复后 10000 条丢 1 条，
// 见 docs/notes/压测报告.md 8l 节）。
func (k *KafkaServer) persistBatch(msgs []kafka.Message) []kafka.Message {
	failed := make([]kafka.Message, 0)
	pairs := make([]textWithRaw, 0, len(msgs))
	for _, km := range msgs {
		var req request.ChatMessageRequest
		if err := json.Unmarshal(km.Value, &req); err != nil {
			zlog.Error(fmt.Sprintf("kafka batch unmarshal: %v", err))
			continue
		}
		m := buildTextMessage(&req)
		kk := dedupKey(km)
		m.KafkaKey = &kk
		pairs = append(pairs, textWithRaw{req: &req, msg: m, km: km})
	}
	if len(pairs) == 0 {
		return failed
	}

	// 批量 INSERT；失败（如含重复 kafka_key 导致整批冲突）则逐条回退，
	// 与 channel 模式保持一致的幂等语义（duplicate 不进入推送）。
	toInsert := make([]*model.Message, 0, len(pairs))
	for _, p := range pairs {
		toInsert = append(toInsert, p.msg)
	}
	if err := dao.GormDB.CreateInBatches(toInsert, len(toInsert)).Error; err != nil {
		zlog.Warn("批量落库失败，回退逐条", zap.Int("batch", len(toInsert)), zap.Error(err))
		pairs = pairs[:0]
		committed := make([]kafka.Message, 0, len(msgs))
		for _, km := range msgs {
			var req request.ChatMessageRequest
			if err := json.Unmarshal(km.Value, &req); err != nil {
				continue
			}
			m := buildTextMessage(&req)
			kk := dedupKey(km)
			m.KafkaKey = &kk
			duplicate, err := persistMessage(m)
			if err != nil {
				// 落库失败：保持 in-flight（连续水位不越过）、不提交 offset，
				// 返回重新入队重试（8l 节）。
				failed = append(failed, km)
				continue
			}
			// 首次成功或 duplicate（DB 已有该 kafka_key/uuid）：处理完成，
			// 解除 in-flight 记账（无剩余在途时水位推进、恢复提交）。
			k.clearInFlight(km)
			markDoneKey(dedupKey(km))
			committed = append(committed, km)
			if duplicate {
				continue // 重复投递：不进入推送
			}
			pairs = append(pairs, textWithRaw{req: &req, msg: m, km: km})
		}
		k.commitSafe(committed)
	} else {
		// 批量 INSERT 原子成功：整批均落库，全部置 done（Pipeline 一次往返，
		// 8n 节：逐条 SETEX 是批量路径吞吐瓶颈之一）；
		// 若含重试消息则解除 in-flight 记账（8l 节）。
		doneKeys := make([]string, 0, len(pairs))
		for _, p := range pairs {
			k.clearInFlight(p.km)
			doneKeys = append(doneKeys, "kafka_dedup:"+dedupKey(p.km))
		}
		markDoneKeys(doneKeys)
		k.commitSafe(msgs)
	}

	for _, p := range pairs {
		publishPush(&PersistedText{Req: p.req, Msg: p.msg})
	}
	return failed
}

// registerInFlight 登记一条已认领、正在处理（含失败重试）的消息：该分区
// 提交水位被钳制在最早 in-flight offset 之前（8m 节，认领即记账）。
func (k *KafkaServer) registerInFlight(km kafka.Message) {
	k.mu.Lock()
	defer k.mu.Unlock()
	set, ok := k.inFlightOffsets[km.Partition]
	if !ok {
		set = make(map[int64]struct{})
		k.inFlightOffsets[km.Partition] = set
	}
	set[km.Offset] = struct{}{}
}

// clearInFlight 移除一条已处理完成（落库成功/duplicate）的消息；分区无
// 剩余 in-flight 时解除提交钳制（水位推进，commitPending 补提交暂扣消息）。
func (k *KafkaServer) clearInFlight(km kafka.Message) {
	k.mu.Lock()
	defer k.mu.Unlock()
	set, ok := k.inFlightOffsets[km.Partition]
	if !ok {
		return
	}
	delete(set, km.Offset)
	if len(set) == 0 {
		delete(k.inFlightOffsets, km.Partition)
	}
}

// commitSafe 显式提交一批已完成消息的消费 offset（8k 节 manual commit）：
// 落库成功后才提交，确保"已读未落库"的消息在崩溃后仍会重放；且受连续
// 分区水位保护——分区存在 in-flight（未落库/重试中）消息时，只提交最早
// in-flight offset 之前的位置，被扣消息暂存 pendingCommits，由 commitPending
// 在水位推进后补提交（8l/8m 节：防止后续成功提交"带过"失败消息丢失）。
// 提交失败仅记日志：未提交的 offset 会在下次启动/重平衡时重放，
// 由幂等键 + DB 唯一索引兜底（不重复落库）。
func (k *KafkaServer) commitSafe(msgs []kafka.Message) {
	k.mu.Lock()
	defer k.mu.Unlock()
	byPart := make(map[int][]kafka.Message)
	for _, m := range msgs {
		if set, ok := k.inFlightOffsets[m.Partition]; ok {
			if m.Offset >= firstKey(set) {
				// 分区存在更早的 in-flight 消息：本消息暂扣（已完成，等水位推进）。
				k.pendingCommits = append(k.pendingCommits, m)
				continue
			}
		}
		byPart[m.Partition] = append(byPart[m.Partition], m)
	}
	k.doCommit(byPart)
}

// commitPending 尝试提交被连续水位暂扣的已完成消息（由 consumeLoop 的
// ticker 驱动；水位推进后这些消息的 offset 低于最早 in-flight，可以提交）。
func (k *KafkaServer) commitPending() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.pendingCommits) == 0 {
		return
	}
	remaining := k.pendingCommits[:0]
	byPart := make(map[int][]kafka.Message)
	for _, m := range k.pendingCommits {
		if set, ok := k.inFlightOffsets[m.Partition]; ok {
			if m.Offset >= firstKey(set) {
				remaining = append(remaining, m)
				continue
			}
		}
		byPart[m.Partition] = append(byPart[m.Partition], m)
	}
	k.pendingCommits = remaining
	k.doCommit(byPart)
}

// doCommit 实际提交各分区的 offset（取传入消息的最大 offset，分区级单调）。
func (k *KafkaServer) doCommit(byPart map[int][]kafka.Message) {
	for _, partMsgs := range byPart {
		if err := mykafka.KafkaService.ChatReader.CommitMessages(context.Background(), partMsgs...); err != nil {
			zlog.Error("提交 offset 失败: " + err.Error())
		}
	}
}

// firstKey 返回集合中的最小 offset（分区最早 in-flight 点）；空集合返回 -1。
func firstKey(set map[int64]struct{}) int64 {
	var min int64 = -1
	for off := range set {
		if min == -1 || off < min {
			min = off
		}
	}
	return min
}

// buildTextMessage 由请求构造文本消息实体（与分发循环 Text 分支同一套字段）。
func buildTextMessage(chatMessageReq *request.ChatMessageRequest) *model.Message {
	message := &model.Message{
		Uuid:       fmt.Sprintf("M%s", random.GetNowAndLenRandomString(11)),
		SessionId:  chatMessageReq.SessionId,
		Type:       chatMessageReq.Type,
		Content:    chatMessageReq.Content,
		Url:        "",
		SendId:     chatMessageReq.SendId,
		SendName:   chatMessageReq.SendName,
		SendAvatar: normalizePath(chatMessageReq.SendAvatar),
		ReceiveId:  chatMessageReq.ReceiveId,
		FileSize:   "0B",
		FileType:   "",
		FileName:   "",
		Status:     message_status_enum.Unsent,
		CreatedAt:  time.Now(),
		AVdata:     "",
	}
	return message
}

func (k *KafkaServer) Close() {
	k.server.Close()
}

func (k *KafkaServer) SendClientToLogin(client *Client) {
	k.server.SendClientToLogin(client)
}

func (k *KafkaServer) SendClientToLogout(client *Client) {
	k.server.SendClientToLogout(client)
}

func (k *KafkaServer) RemoveClient(uuid string) {
	k.server.RemoveClient(uuid)
}
