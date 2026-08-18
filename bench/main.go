// bench 是 GoChat 的自研 WebSocket 压测客户端（见 docs/design/messaging.md）。
//
// 用法：
//
//	go run ./bench -mode conn  -n 1000 -t 60            # 在线规模：1k+ 并发长连接 + 心跳
//	go run ./bench -mode chat  -pairs 100 -rate 10 -t 30 # 单聊稳态：端到端延迟 P50/P99
//	go run ./bench -mode group -members 200 -senders 20 -burst 50  # 群聊风暴
//	go run ./bench -mode slow  -n 100 -burst 1000        # 慢客户端注入
//	go run ./bench -mode order -pairs 50 -burst 300      # 会话内顺序验证（Kafka 分区键保序）
//
// 前置条件：后端已启动（channel 模式），MySQL/Redis 可用；测试用户与群组由本工具直接写入
// 数据库（绕过注册接口，避免短信依赖），WS 连接使用本地签发的 Access Token。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"gochat/internal/config"
	"gochat/internal/dao"
	"gochat/internal/dto/request"
	myredis "gochat/internal/service/redis"
)

var (
	wsURL    = flag.String("ws", "ws://127.0.0.1:8000/wss", "后端 WebSocket 地址")
	host     = flag.String("host", "127.0.0.1", "后端地址")
	port     = flag.Int("port", 8000, "后端端口")
	mode     = flag.String("mode", "conn", "conn | chat | group | slow | order")
	n        = flag.Int("n", 100, "连接数（conn/slow）")
	duration = flag.Int("t", 30, "压测时长（秒）")
	pairs    = flag.Int("pairs", 100, "聊天对数（chat）")
	rate     = flag.Int("rate", 10, "每秒每对消息数（chat）")
	members  = flag.Int("members", 200, "群成员数（group）")
	senders  = flag.Int("senders", 20, "群发言人数（group）")
	burst    = flag.Int("burst", 50, "每人爆发条数（group/slow）")
	apiPath  = flag.String("api", "/api/v1/session/getUserSessionList", "接口路径（api 模式）")
	apiBody  = flag.String("body", `{"owner_id":"U10000000003"}`, "接口请求体（api 模式）")
	apiUser  = flag.String("apiUser", "13600000001", "api 模式登录手机号")
	flushPfx = flag.String("flushPrefix", "", "api 模式：每次请求前删除该前缀缓存（测冷路径）")
	sample   = flag.Int("sample", 0, "soak 采样间隔（秒），conn 模式输出时间序列（0=仅结束汇总）")
	verbose  = flag.Bool("v", false, "打印明细")
)

func main() {
	flag.Parse()
	conf := config.GetConfig()
	dao.MustInit()

	wsAddr := *wsURL
	if *host != "" {
		wsAddr = fmt.Sprintf("ws://%s:%d/wss", *host, *port)
	}
	_ = conf

	switch *mode {
	case "conn":
		scenarioConn(wsAddr, *n, *duration)
	case "chat":
		scenarioChat(wsAddr, *pairs, *rate, *duration)
	case "group":
		scenarioGroup(wsAddr, *members, *senders, *burst)
	case "slow":
		scenarioSlow(wsAddr, *n, *burst)
	case "order":
		scenarioOrder(wsAddr, *pairs, *burst)
	case "api":
		scenarioAPI(wsAddr, *apiPath, *apiBody, *apiUser, *n, *duration, *flushPfx)
	default:
		log.Fatalf("unknown mode %q", *mode)
	}
}

// ---------------- 场景：HTTP 接口延迟（进程内客户端，连接复用） ----------------

func scenarioAPI(wsAddr, path, body, telephone string, count, seconds int, flushPrefix string) {
	// 先登录拿 token（走 HTTP）：wsAddr 形如 ws://host:port/wss，去掉路径取源站
	u, err := url.Parse(wsAddr)
	if err != nil {
		log.Fatalf("parse ws addr: %v", err)
	}
	base := "http://" + u.Host
	loginBody := fmt.Sprintf(`{"telephone":"%s","password":"123456"}`, telephone)
	loginRsp, err := http.Post(base+"/api/v1/auth/login", "application/json", strings.NewReader(loginBody))
	if err != nil {
		log.Fatalf("login: %v", err)
	}
	var loginData struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(loginRsp.Body).Decode(&loginData); err != nil {
		log.Fatalf("decode login: %v", err)
	}
	_ = loginRsp.Body.Close()
	if loginData.Code != 0 {
		log.Fatalf("login failed: %v", loginData)
	}
	token := loginData.Data.AccessToken

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("POST", base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	var latencies []time.Duration
	var failures int64
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	if count > 0 {
		deadline = time.Now().Add(24 * time.Hour) // 用 count 限定
	}
	for i := 0; i < count || (count == 0 && time.Now().Before(deadline)); i++ {
		if flushPrefix != "" {
			myredis.DelKeysWithPrefix(flushPrefix)
		}
		start := time.Now()
		rsp, err := client.Do(req)
		if err != nil {
			failures++
			continue
		}
		_, _ = io.Copy(io.Discard, rsp.Body)
		_ = rsp.Body.Close()
		latencies = append(latencies, time.Since(start))
		if count == 0 && !time.Now().Before(deadline) {
			break
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	total := len(latencies)
	if total == 0 {
		fmt.Println("无样本")
		return
	}
	var sum time.Duration
	for _, l := range latencies {
		sum += l
	}
	fmt.Printf("API %s: %d 次请求, 失败 %d\n", path, total, failures)
	fmt.Printf("延迟: P50=%s P95=%s P99=%s max=%s avg=%s\n",
		latencies[total/2], latencies[total*95/100], latencies[total*99/100], latencies[total-1], sum/time.Duration(total))
}

// ---------------- 测试数据准备 ----------------

// ensureUsers 在数据库中准备 count 个测试用户，返回 uuid 列表。
func ensureUsers(count int) []string {
	uuids := make([]string, count)
	for i := 0; i < count; i++ {
		uuids[i] = fmt.Sprintf("U_BENCH_%06d", i)
		telephone := fmt.Sprintf("188%08d", i)
		res := dao.GormDB.Exec(
			"INSERT INTO user_info (uuid, nickname, telephone, avatar, password, created_at, is_admin, status) "+
				"VALUES (?, ?, ?, ?, '$2a$10$1234567890123456789012345678901234567890123456789012', NOW(), 0, 0) "+
				"ON DUPLICATE KEY UPDATE nickname = VALUES(nickname)",
			uuids[i], "bench"+fmt.Sprint(i), telephone,
			"https://cube.elemecdn.com/0/88/03b0d39583f48206768a7534e55bcpng.png",
		)
		if res.Error != nil {
			log.Fatalf("insert bench user: %v", res.Error)
		}
	}
	return uuids
}

// ensureGroup 创建 / 复用包含指定成员的群，返回群 uuid。
func ensureGroup(memberUUIDs []string) string {
	groupID := "G_BENCH_GROUP_001"
	membersJSON, _ := json.Marshal(memberUUIDs)
	res := dao.GormDB.Exec(
		"INSERT INTO group_info (uuid, name, owner_id, member_cnt, members, status, created_at, updated_at) "+
			"VALUES (?, 'bench-group', ?, ?, ?, 0, NOW(), NOW()) "+
			"ON DUPLICATE KEY UPDATE members = VALUES(members), member_cnt = VALUES(member_cnt)",
		groupID, memberUUIDs[0], len(memberUUIDs), string(membersJSON),
	)
	if res.Error != nil {
		log.Fatalf("insert bench group: %v", res.Error)
	}
	return groupID
}

// signToken 用与后端一致的 JWT 密钥本地签发 Access Token（仅压测用）。
func signToken(uuid string, isAdmin int8) (string, error) {
	cfg := config.GetConfig().JwtConfig
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		ID:        "bench",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uuid":     uuid,
		"is_admin": isAdmin,
		"iat":      claims.IssuedAt,
		"exp":      claims.ExpiresAt,
		"jti":      claims.ID,
	})
	return token.SignedString([]byte(cfg.Secret))
}

// ---------------- 连接与消息 ----------------

type benchConn struct {
	ws   *websocket.Conn
	uuid string
}

func dial(wsAddr, uuid string) (*benchConn, error) {
	token, err := signToken(uuid, 0)
	if err != nil {
		return nil, err
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsAddr+"?token="+token, nil)
	if err != nil {
		return nil, err
	}
	return &benchConn{ws: conn, uuid: uuid}, nil
}

func (c *benchConn) readLoop(onMessage func([]byte), onClose func()) {
	defer func() {
		_ = c.ws.Close()
		if onClose != nil {
			onClose()
		}
	}()
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		if onMessage != nil {
			onMessage(data)
		}
	}
}

func buildMsg(sessionID, content, sendID, receiveID string) []byte {
	req := request.ChatMessageRequest{
		SessionId:  sessionID,
		Type:       0, // Text
		Content:    content,
		SendId:     sendID,
		SendName:   "bench",
		SendAvatar: "https://cube.elemecdn.com/0/88/03b0d39583f48206768a7534e55bcpng.png",
		ReceiveId:  receiveID,
		FileSize:   "0B",
	}
	data, _ := json.Marshal(req)
	return data
}

// benchSessionID 生成不超过 char(20) 的会话 id（真实会话为 S + 11 位）。
func benchSessionID(prefix string, idx int) string {
	return fmt.Sprintf("S%s%05d", prefix, idx)
}

// ---------------- 场景：在线规模 ----------------

func scenarioConn(wsAddr string, count, seconds int) {
	fmt.Printf("== conn: 建立 %d 条连接并保持 %ds ==\n", count, seconds)
	uuids := ensureUsers(count)

	var wg sync.WaitGroup
	var connected int64
	var failed int64
	var errMu sync.Mutex
	var dialErrs = make(map[string]int)
	conns := make([]*benchConn, 0, count)
	start := time.Now()
	// 拨号并发限流：真实用户不会在同一毫秒拨号，按波次建立连接，
	// 避免瞬时并发拨号击穿 listen backlog（那属于客户端行为而非服务器容量）。
	dialSem := make(chan struct{}, 100)
	for _, uuid := range uuids {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			dialSem <- struct{}{}
			defer func() { <-dialSem }()
			conn, err := dial(wsAddr, u)
			if err != nil {
				atomic.AddInt64(&failed, 1)
				errMu.Lock()
				dialErrs[err.Error()]++
				errMu.Unlock()
				return
			}
			atomic.AddInt64(&connected, 1)
			connsMu.Lock()
			conns = append(conns, conn)
			connsMu.Unlock()
			go conn.readLoop(nil, func() { atomic.AddInt64(&connected, -1) }) // 保持期掉线即计数衰减
		}(uuid)
	}
	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("连接完成: 成功 %d, 失败 %d, 耗时 %.2fs, 成功率 %.2f%%\n",
		connected, failed, elapsed.Seconds(), float64(connected)/float64(count)*100)
	errMu.Lock()
	for msg, cnt := range dialErrs {
		fmt.Printf("  拨号失败 %d 次: %s\n", cnt, msg)
	}
	errMu.Unlock()

	// 持续保持连接，观察稳定性（soak：按 -sample 间隔采样，输出时间序列）
	keep := time.Second * time.Duration(seconds)
	interval := keep / 10
	if *sample > 0 {
		interval = time.Duration(*sample) * time.Second
	}
	points := 0
	if interval > 0 {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for i := 0; i < 10 || (*sample > 0 && i < seconds/(*sample)+1); i++ {
			<-ticker.C
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			alive := atomic.LoadInt64(&connected)
			fmt.Printf("t=%ds 连接=%d goroutine=%d 堆内存=%.1fMB 系统内存=%.1fMB\n",
				int64(time.Since(start).Seconds()), alive, runtime.NumGoroutine(),
				float64(m.HeapAlloc)/1024/1024, float64(m.Sys)/1024/1024)
			points++
		}
	}

	connsMu.Lock()
	for _, c := range conns {
		_ = c.ws.Close()
	}
	connsMu.Unlock()
	fmt.Println("== conn 场景结束 ==")
}

var connsMu sync.Mutex

// ---------------- 场景：单聊稳态延迟 ----------------

// runID 每轮测试唯一，写入消息内容避免跨轮 sentAt 匹配污染
// （相同 content 会被上一轮残留的发送时间戳匹配，虚高延迟）。
var runID = fmt.Sprintf("r%d", time.Now().UnixNano()%100000000)

// isBusinessFrame 判断一帧是否为业务消息（消息推送/回显 JSON，含 content 字段）。
// 8m 节：欢迎帧（纯文本"欢迎来到GoChat聊天服务器"）不是业务帧——此前接收端
// 对所有帧计数，欢迎帧把送达率/群送达次数虚高（实测 400 次推送统计成 410 次）。
// 8n 复测：改为 bytes.Contains 子串判定——完整 json.Unmarshal 每帧 ~3-5µs +
// map 分配，群测 80k 帧/s 下 bench 自身 GC 压力把读循环卡出数百毫秒停顿
// （成员被误判慢客户端丢弃，group 送达率 99.6-99.9% 抖动）。业务帧必含
// "content" 键（序列化后为 "content":），欢迎/拒绝帧为纯文本，判定集合等价。
func isBusinessFrame(data []byte) bool {
	return bytes.Contains(data, []byte("\"content\""))
}

func scenarioChat(wsAddr string, pairCount, ratePerPair, seconds int) {
	fmt.Printf("== chat: %d 对单聊, 每对 %d msg/s, %ds ==\n", pairCount, ratePerPair, seconds)
	uuids := ensureUsers(pairCount * 2)

	var sent int64
	var received int64
	var latencies []time.Duration
	var latMu sync.Mutex
	var closed int64
	var teardown int64

	// 每个连接一个读循环；发送方记录发送时间，收到回显（内容匹配）时记录延迟
	senders := make([]*benchConn, pairCount)
	receivers := make([]*benchConn, pairCount)
	var wg sync.WaitGroup
	for i := 0; i < pairCount; i++ {
		senderUUID := uuids[2*i]
		receiverUUID := uuids[2*i+1]

		recv, err := dial(wsAddr, receiverUUID)
		if err != nil {
			log.Fatalf("dial receiver: %v", err)
		}
		receivers[i] = recv
		go recv.readLoop(func(data []byte) {
			if !isBusinessFrame(data) {
				return // 8m 节：过滤欢迎帧等非业务帧
			}
			atomic.AddInt64(&received, 1)
		}, func() {
			if atomic.LoadInt64(&teardown) == 0 {
				atomic.AddInt64(&closed, 1)
			}
		})

		send, err := dial(wsAddr, senderUUID)
		if err != nil {
			log.Fatalf("dial sender: %v", err)
		}
		senders[i] = send
		go send.readLoop(func(data []byte) {
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				return
			}
			content, _ := msg["content"].(string)
			if ts, ok := sentAt.Load(content); ok {
				sendTime := ts.(time.Time)
				latMu.Lock()
				latencies = append(latencies, time.Since(sendTime))
				latMu.Unlock()
				sentAt.Delete(content)
			}
		}, func() {
			if atomic.LoadInt64(&teardown) == 0 {
				atomic.AddInt64(&closed, 1)
			}
		})
	}

	// 稳态流量：每对 sender 按 rate 发送（启动时加随机相位，避免同步突发排队）
	var sendWG sync.WaitGroup
	sendWG.Add(pairCount)
	for i := 0; i < pairCount; i++ {
		go func(idx int) {
			defer sendWG.Done()
			s := senders[idx]
			r := receivers[idx]
			interval := time.Second / time.Duration(ratePerPair)
			deadline := time.Now().Add(time.Duration(seconds) * time.Second)
			seq := 0
			// 相位抖动：让各对的发送在时间轴上均匀错开，模拟真实到达分布
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
			for time.Now().Before(deadline) {
				content := fmt.Sprintf("chat-%s-%d-%d", runID, idx, seq)
				sentAt.Store(content, time.Now())
				if err := s.ws.WriteMessage(websocket.TextMessage, buildMsg(benchSessionID("CHAT", idx), content, s.uuid, r.uuid)); err != nil {
					atomic.AddInt64(&closed, 1)
					return
				}
				atomic.AddInt64(&sent, 1)
				seq++
				time.Sleep(interval)
			}
		}(i)
	}
	sendWG.Wait()
	// 等尾部消息回显
	time.Sleep(2 * time.Second)
	atomic.StoreInt64(&teardown, 1)

	for _, c := range senders {
		_ = c.ws.Close()
	}
	for _, c := range receivers {
		_ = c.ws.Close()
	}
	wg.Wait()

	latMu.Lock()
	defer latMu.Unlock()
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	total := len(latencies)
	fmt.Printf("发送 %d 条, 收到回显 %d 条(丢失 %d), 连接异常 %d\n",
		sent, received, sent-received, closed)
	if total > 0 {
		p50 := latencies[total/2]
		p99 := latencies[total*99/100]
		var sum time.Duration
		for _, l := range latencies {
			sum += l
		}
		fmt.Printf("端到端延迟(发送→回显): P50=%s P99=%s max=%s avg=%s\n",
			p50, p99, latencies[total-1], sum/time.Duration(total))
	} else {
		fmt.Println("无延迟样本")
	}
	fmt.Println("== chat 场景结束 ==")
}

var sentAt sync.Map

// ---------------- 场景：群聊风暴 ----------------

func scenarioGroup(wsAddr string, memberCount, senderCount, burstPerSender int) {
	fmt.Printf("== group: 群 %d 人, %d 人发言, 每人爆发 %d 条 ==\n", memberCount, senderCount, burstPerSender)
	uuids := ensureUsers(memberCount)
	groupID := ensureGroup(uuids)

	conns := make([]*benchConn, memberCount)
	var delivered int64
	var failedDial int64
	perMember := make([]int64, memberCount)
	var wg sync.WaitGroup
	wg.Add(memberCount)
	// 拨号并发限流（与 conn 场景一致）：400 成员同一毫秒并发拨号会击穿
	// listen backlog（实测 8n 节：117/400 连接失败），真实用户按波次接入。
	dialSem := make(chan struct{}, 100)
	for i, uuid := range uuids {
		go func(idx int, u string) {
			defer wg.Done()
			dialSem <- struct{}{}
			defer func() { <-dialSem }()
			conn, err := dial(wsAddr, u)
			if err != nil {
				atomic.AddInt64(&failedDial, 1)
				return
			}
			conns[idx] = conn
			go conn.readLoop(func(data []byte) {
				if !isBusinessFrame(data) {
					return // 8m 节：过滤欢迎帧等非业务帧
				}
				atomic.AddInt64(&delivered, 1)
				atomic.AddInt64(&perMember[idx], 1)
			}, nil)
		}(i, uuid)
	}
	wg.Wait()
	if failedDial > 0 {
		log.Fatalf("群成员连接失败 %d", failedDial)
	}
	time.Sleep(500 * time.Millisecond)

	// 群发言：senderCount 个成员各自爆发
	var sendWG sync.WaitGroup
	sendWG.Add(senderCount)
	start := time.Now()
	for s := 0; s < senderCount; s++ {
		go func(idx int) {
			defer sendWG.Done()
			sender := conns[idx]
			for b := 0; b < burstPerSender; b++ {
				content := fmt.Sprintf("group-%d-%d", idx, b)
				if err := sender.ws.WriteMessage(websocket.TextMessage, buildMsg(benchSessionID("GRP", 1), content, sender.uuid, groupID)); err != nil {
					return
				}
			}
		}(s)
	}
	sendWG.Wait()
	elapsed := time.Since(start)
	// 等推送落盘：串行落库 + 群扇出需要足够排空时间（300 条 × ~10ms ≈ 3s）
	time.Sleep(10 * time.Second)

	// 预期:每条群消息推送给除自己外的在线成员 + 自己回显 = memberCount 次
	expected := int64(senderCount * burstPerSender * memberCount)
	fmt.Printf("发言耗时 %.2fs; 预期推送 %d 次, 实际收到 %d 次, 送达率 %.2f%%\n",
		elapsed.Seconds(), expected, delivered, float64(delivered)/float64(expected)*100)
	// 每成员送达分布
	min, max := int64(1<<60), int64(0)
	sum := int64(0)
	zero := 0
	for _, c := range perMember {
		if c < min {
			min = c
		}
		if c > max {
			max = c
		}
		sum += c
		if c == 0 {
			zero++
		}
	}
	fmt.Printf("每成员送达: min=%d max=%d avg=%.1f 零送达成员=%d\n",
		min, max, float64(sum)/float64(memberCount), zero)

	for _, c := range conns {
		if c != nil {
			_ = c.ws.Close()
		}
	}
	fmt.Println("== group 场景结束 ==")
}

// ---------------- 场景：慢客户端注入 ----------------

func scenarioSlow(wsAddr string, normalCount, burstPerMsg int) {
	fmt.Printf("== slow: %d 个正常客户端 + 1 个不读数据的慢客户端 ==\n", normalCount)
	uuids := ensureUsers(normalCount + 1)
	slowUUID := uuids[normalCount]

	// 正常客户端两两聊天
	normalConns := make([]*benchConn, normalCount)
	var delivered int64
	var closedNormal int64
	var wg sync.WaitGroup
	wg.Add(normalCount)
	for i := 0; i < normalCount; i++ {
		go func(idx int) {
			defer wg.Done()
			conn, err := dial(wsAddr, uuids[idx])
			if err != nil {
				return
			}
			normalConns[idx] = conn
			go conn.readLoop(func(data []byte) {
				if !isBusinessFrame(data) {
					return
				}
				atomic.AddInt64(&delivered, 1)
			}, func() { atomic.AddInt64(&closedNormal, 1) })
		}(i)
	}
	wg.Wait()

	// 慢客户端：建立连接但彻底不读（8m 节修复：此前单次 ReadMessage 即判定
	// 断开，welcome 帧满足条件造成 0.00s 假断开）。
	// 阶段 1 读掉欢迎帧与首条业务帧（flood 开始到达）；阶段 2 彻底不读——
	// 推送在 SendBack 积压（容量 100）→ 服务端连续丢弃 ≥8 次判定慢客户端断开；
	// 断开后 TCP 关闭，Ping 控制帧写失败即感知（期间不读任何数据帧，
	// 保持积压判定条件）。flood 条数须 > 容量+阈值（8n 节：TCP_NODELAY 后
	// Write 排空很快，本机回环内核缓冲可吸收 ~300 帧/120KB，burst=300 打不满
	// SendBack；实测需 ≥1000 才能触发丢弃判定）。
	slowConn, err := dial(wsAddr, slowUUID)
	if err != nil {
		log.Fatalf("dial slow client: %v", err)
	}
	slowClosed := make(chan struct{})
	go func() {
		defer close(slowClosed)
		for {
			_, data, err := slowConn.ws.ReadMessage()
			if err != nil {
				return
			}
			if isBusinessFrame(data) {
				break // 业务帧开始到达：转入"彻底不读"阶段
			}
		}
		for {
			time.Sleep(500 * time.Millisecond)
			if err := slowConn.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(2*time.Second)); err != nil {
				return // 连接已被服务端断开
			}
		}
	}()
	fmt.Println("慢客户端已连接(不读数据)")

	// 攻击者：第 0 个正常客户端向慢客户端狂发消息（触发 SendBack 积压 → 丢弃 → 断开）
	attacker := normalConns[0]
	start := time.Now()
	for i := 0; i < burstPerMsg; i++ {
		if err := attacker.ws.WriteMessage(websocket.TextMessage, buildMsg(benchSessionID("SLOW", 1), fmt.Sprintf("flood-%d", i), attacker.uuid, slowUUID)); err != nil {
			break
		}
	}
	fmt.Printf("攻击者向慢客户端发了 %d 条\n", burstPerMsg)

	// 同时正常客户端之间互发消息，验证全局吞吐不受影响
	var sendWG sync.WaitGroup
	sendWG.Add(1)
	go func() {
		defer sendWG.Done()
		a := normalConns[1%normalCount]
		for i := 0; i < 100; i++ {
			peer := normalConns[(i+2)%normalCount]
			if err := a.ws.WriteMessage(websocket.TextMessage, buildMsg(benchSessionID("NRM", 1), fmt.Sprintf("n-%d", i), a.uuid, peer.uuid)); err != nil {
				return
			}
		}
	}()
	sendWG.Wait()

	// 等待慢客户端被断开
	select {
	case <-slowClosed:
		fmt.Printf("慢客户端已被服务端断开 (%.2fs 内)\n", time.Since(start).Seconds())
	case <-time.After(15 * time.Second):
		fmt.Println("警告: 慢客户端未被断开")
	}

	time.Sleep(2 * time.Second)
	fmt.Printf("正常客户端: 收到消息 %d 条, 异常断开 %d 个\n", delivered, closedNormal)

	for _, c := range normalConns {
		if c != nil {
			_ = c.ws.Close()
		}
	}
	_ = slowConn.ws.Close()
	fmt.Println("== slow 场景结束 ==")
}

var _ = os.Exit
var _ = rand.Int

// ---------------- 场景：会话内顺序验证（Kafka 分区键保序） ----------------

// scenarioOrder 验证"同一会话消息顺序保持"（Kafka 模式以 receiveId 为分区键的承诺）：
// 每对 sender 快速连发 burst 条消息（seq 递增），receiver 与 sender（回显）两端分别记录
// 收到的 seq，断言严格递增、无乱序、无重复、无缺失。
func scenarioOrder(wsAddr string, pairCount, burst int) {
	fmt.Printf("== order: %d 对单聊, 每对连发 %d 条, 验证会话内顺序 ==\n", pairCount, burst)
	uuids := ensureUsers(pairCount * 2)

	type orderStat struct {
		mu   sync.Mutex
		seqs []int
	}
	recvStats := make([]*orderStat, pairCount)
	sendStats := make([]*orderStat, pairCount)
	for i := 0; i < pairCount; i++ {
		recvStats[i] = &orderStat{}
		sendStats[i] = &orderStat{}
	}
	runPfx := "chat-" + runID

	senders := make([]*benchConn, pairCount)
	receivers := make([]*benchConn, pairCount)
	for i := 0; i < pairCount; i++ {
		senderUUID := uuids[2*i]
		receiverUUID := uuids[2*i+1]
		recvIdx, sendIdx := i, i

		recv, err := dial(wsAddr, receiverUUID)
		if err != nil {
			log.Fatalf("dial receiver: %v", err)
		}
		receivers[i] = recv
		go recv.readLoop(func(data []byte) {
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				return
			}
			content, _ := msg["content"].(string)
			if !strings.HasPrefix(content, runPfx) {
				return // 只统计本轮消息
			}
			seq := parseSeqOf(content)
			if seq < 0 {
				return
			}
			recvStats[recvIdx].mu.Lock()
			recvStats[recvIdx].seqs = append(recvStats[recvIdx].seqs, seq)
			recvStats[recvIdx].mu.Unlock()
		}, nil)

		send, err := dial(wsAddr, senderUUID)
		if err != nil {
			log.Fatalf("dial sender: %v", err)
		}
		senders[i] = send
		go send.readLoop(func(data []byte) {
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				return
			}
			content, _ := msg["content"].(string)
			if !strings.HasPrefix(content, runPfx) {
				return
			}
			seq := parseSeqOf(content)
			if seq < 0 {
				return
			}
			sendStats[sendIdx].mu.Lock()
			sendStats[sendIdx].seqs = append(sendStats[sendIdx].seqs, seq)
			sendStats[sendIdx].mu.Unlock()
		}, nil)
	}

	// 每对 sender 连发 burst 条（无间隔快速连发，考验 broker 分区内顺序 + 消费保序）
	sendWG := sync.WaitGroup{}
	sendWG.Add(pairCount)
	for i := 0; i < pairCount; i++ {
		go func(idx int) {
			defer sendWG.Done()
			s := senders[idx]
			for seq := 0; seq < burst; seq++ {
				content := fmt.Sprintf("%s-%d-%d", runPfx, idx, seq)
				if err := s.ws.WriteMessage(websocket.TextMessage, buildMsg(benchSessionID("ORD", idx), content, s.uuid, uuids[2*idx+1])); err != nil {
					return
				}
			}
		}(i)
	}
	sendWG.Wait()
	sendDone := time.Now()
	// 轮询等待所有接收端收满 burst 条（消费是异步的：Kafka 积压 / 批量落库 / 推送
	// 全链路需要时间；固定 sleep 在 burst 超过消费容量时会提前断开连接）。
	// 超时上限：burst 总条数按 200 msg/s 兜底折算，最低 30s，最高 120s。
	waitCap := burst * pairCount / 200
	if waitCap < 30 {
		waitCap = 30
	}
	if waitCap > 120 {
		waitCap = 120
	}
	waitDeadline := sendDone.Add(time.Duration(waitCap) * time.Second)
	receivedAll := func() bool {
		for i := 0; i < pairCount; i++ {
			recvStats[i].mu.Lock()
			n := len(recvStats[i].seqs)
			recvStats[i].mu.Unlock()
			if n < burst {
				return false
			}
		}
		return true
	}
	for !receivedAll() && time.Now().Before(waitDeadline) {
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("等待收满: %.1fs（上限 %ds）\n", time.Since(sendDone).Seconds(), waitCap)

	for _, c := range senders {
		_ = c.ws.Close()
	}
	for _, c := range receivers {
		_ = c.ws.Close()
	}

	// 断言：每对接收端 seq 严格递增、无重复；发送端回显同理
	checkOrder := func(name string, stats []*orderStat, expected int) bool {
		ok := true
		var totalGot, totalDup, totalGap, totalReorder int
		for i, st := range stats {
			st.mu.Lock()
			seqs := append([]int(nil), st.seqs...)
			st.mu.Unlock()
			// 8m 节：不再 sort.Ints——排序会掩盖真实到达顺序，无法检测乱序。
			// 按接收顺序检查：严格递增 = 保序；非递增 = 乱序；跳号 = 缺失。
			pairDup, pairGap, pairReorder := 0, 0, 0
			for j := 1; j < len(seqs); j++ {
				switch {
				case seqs[j] == seqs[j-1]:
					pairDup++
				case seqs[j] < seqs[j-1]:
					pairReorder++
				case seqs[j] != seqs[j-1]+1:
					pairGap++
				}
			}
			totalGot += len(seqs)
			totalDup += pairDup
			totalGap += pairGap
			totalReorder += pairReorder
			if len(seqs) != expected || pairDup > 0 || pairGap > 0 || pairReorder > 0 {
				ok = false
				if *verbose || i < 3 {
					fmt.Printf("  [%s] 对 %d: 收到 %d/%d, 重复 %d, 乱序 %d, 缺失 %d\n",
						name, i, len(seqs), expected, pairDup, pairReorder, pairGap)
				}
			}
		}
		fmt.Printf("[%s] %d 对: 共收到 %d/%d, 重复 %d, 乱序 %d, 缺失 %d\n",
			name, len(stats), totalGot, expected*len(stats), totalDup, totalReorder, totalGap)
		return ok
	}

	recvOK := checkOrder("接收端", recvStats, burst)
	sendOK := checkOrder("发送端回显", sendStats, burst)
	if recvOK && sendOK {
		fmt.Println("== order 场景通过：会话内消息严格保序，无乱序/重复/缺失 ==")
	} else {
		fmt.Println("== order 场景失败：见上方明细 ==")
		os.Exit(1)
	}
}

// parseSeqOf 从 content "chat-{runID}-{idx}-{seq}" 提取末尾 seq。
func parseSeqOf(content string) int {
	idx := strings.LastIndex(content, "-")
	if idx < 0 {
		return -1
	}
	n, err := strconv.Atoi(content[idx+1:])
	if err != nil {
		return -1
	}
	return n
}
