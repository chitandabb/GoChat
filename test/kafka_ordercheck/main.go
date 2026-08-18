// kafka_ordercheck 验证 Kafka 模式的分区键保序承诺（见 docs/design/messaging.md：
// "以 receiveId 为分区键，保证同一会话/群的消息进入同一分区保序"）。
//
// 用法：go run ./test/kafka_ordercheck -broker 127.0.0.1:9092 -topic chat_message
//
// 独立 consumer group 从 earliest 全量消费，不干扰 chat 组 offset。输出：
//  1. 每个 receiveId(key) 落到的分区集合（期望：每个 key 恰好 1 个分区）
//  2. 每个 key 内 seq 严格递增性（按分区内 offset 顺序；content 格式 chat-{runID}-{idx}-{seq}）
//  3. 各分区消息数分布（Hash balancer 期望接近均匀）
//  4. 乱序 / 重复 / 跨分区漂移检测
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"

	"gochat/internal/dto/request"
)

func main() {
	broker := flag.String("broker", "127.0.0.1:9092", "kafka broker 地址")
	topic := flag.String("topic", "chat_message", "待检查 topic")
	timeout := flag.Duration("timeout", 30*time.Second, "消费超时（无新消息后判定结束）")
	flag.Parse()

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{*broker},
		Topic:       *topic,
		GroupID:     fmt.Sprintf("ordercheck-%d", time.Now().UnixNano()),
		StartOffset: kafka.FirstOffset,
		MaxWait:     100 * time.Millisecond,
	})
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	type keyStat struct {
		partitions map[int]int // partition -> 该 key 的消息数（期望只有 1 个分区）
		// 按 (runID) 分组的 seq 序列：content 形如 chat-{runID}-{idx}-{seq}，
		// 同一会话在每轮测试中 seq 从 0 重新计数，跨轮混检会误报断序。
		runs  map[string][]int
		count int
	}
	stats := make(map[string]*keyStat)
	var partitionCount = map[int]int{}
	var total int

	lastMsg := time.Now()
	for {
		msg, err := r.ReadMessage(ctx)
		if err != nil {
			break // 超时或 EOF：结束
		}
		lastMsg = time.Now()
		key := string(msg.Key)
		var req request.ChatMessageRequest
		if err := json.Unmarshal(msg.Value, &req); err != nil {
			fmt.Printf("[skip] 无法解析 value: %v\n", err)
			continue
		}
		st, ok := stats[key]
		if !ok {
			st = &keyStat{partitions: map[int]int{}, runs: map[string][]int{}}
			stats[key] = st
		}
		st.partitions[msg.Partition]++
		st.count++
		runID, seq := parseRunSeq(req.Content)
		if seq >= 0 {
			st.runs[runID] = append(st.runs[runID], seq)
		}
		partitionCount[msg.Partition]++
		total++

		// 无新消息判定：上次消息后 idle 超过 2s（低流量下也足够判定边界）
		if time.Since(lastMsg) > 2*time.Second && total > 0 {
			// 继续读到超时，保证尾部消息不漏
		}
	}

	fmt.Printf("== kafka_ordercheck: topic=%s 共消费 %d 条消息 ==\n", *topic, total)

	// 1. key -> 分区映射
	fmt.Println("\n[1] key(receiveId) -> 分区映射（期望每 key 恒 1 个分区）")
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var drifted int
	for _, k := range keys {
		st := stats[k]
		if len(st.partitions) != 1 {
			drifted++
			fmt.Printf("  [DRIFT] key=%s 出现在 %d 个分区: %v\n", k, len(st.partitions), st.partitions)
		}
	}
	if drifted == 0 {
		fmt.Printf("  全部 %d 个 key 均恒定落在单分区 ✓\n", len(keys))
	}

	// 2. key 内 seq 递增性（按 runID 分组：每轮内 seq 从 0 重新计数）
	fmt.Println("\n[2] 会话内 seq 严格递增（按分区内 offset 顺序，每轮 runID 独立检查）")
	var seqBad, seqCheck int
	for _, k := range keys {
		st := stats[k]
		// 同一 key 只在一个分区：读取顺序 == 分区内 offset 顺序
		for runID, seqs := range st.runs {
			if len(seqs) < 2 {
				continue
			}
			seqCheck++
			for i := 1; i < len(seqs); i++ {
				if seqs[i] != seqs[i-1]+1 {
					seqBad++
					if seqBad <= 5 {
						fmt.Printf("  [OUT-OF-ORDER] key=%s run=%s seq 序列在位置 %d 断序: ...%d, %d...\n",
							k, runID, i, seqs[i-1], seqs[i])
					}
				}
			}
		}
	}
	if seqBad == 0 {
		fmt.Printf("  %d 个会话轮的 seq 全部严格递增 ✓\n", seqCheck)
	} else {
		fmt.Printf("  发现 %d 处断序 ✗\n", seqBad)
	}

	// 3. 分区分布
	fmt.Println("\n[3] 分区消息数分布")
	ps := make([]int, 0, len(partitionCount))
	for p := range partitionCount {
		ps = append(ps, p)
	}
	sort.Ints(ps)
	var maxC, minC = -1, total + 1
	for _, p := range ps {
		fmt.Printf("  partition %d: %d 条 (%.1f%%)\n", p, partitionCount[p], float64(partitionCount[p])/float64(total)*100)
		if partitionCount[p] > maxC {
			maxC = partitionCount[p]
		}
		if partitionCount[p] < minC {
			minC = partitionCount[p]
		}
	}
	if len(ps) > 1 {
		fmt.Printf("  均匀度：最多/最少 = %d/%d (%.0f%%)\n", maxC, minC, float64(minC)/float64(maxC)*100)
	}

	// 4. 汇总判定
	fmt.Println("\n[4] 结论")
	if drifted == 0 && seqBad == 0 {
		fmt.Println("  PASS：分区键保序承诺成立（同会话同分区 + 会话内严格有序）")
	} else {
		fmt.Println("  FAIL：见上方明细")
		os.Exit(1)
	}
}

// parseRunSeq 从 content "chat-{runID}-{idx}-{seq}" 提取 runID 与末尾 seq；
// 非 chat 消息返回 ("", -1)。
func parseRunSeq(content string) (string, int) {
	parts := strings.Split(content, "-")
	if len(parts) < 3 || parts[0] != "chat" {
		return "", -1
	}
	seq, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return "", -1
	}
	return parts[1], seq
}
