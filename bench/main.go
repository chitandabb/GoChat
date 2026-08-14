// bench 是 GoChat 的自研 WebSocket 压测客户端（见 docs/design/messaging.md）。
//
// 用法：
//
//	go run ./bench -mode conn  -n 1000 -t 60            # 在线规模：1k+ 并发长连接 + 心跳
//	go run ./bench -mode chat  -pairs 100 -rate 10 -t 30 # 单聊稳态：端到端延迟 P50/P99
//	go run ./bench -mode group -members 200 -senders 20 -burst 50  # 群聊风暴
//	go run ./bench -mode slow  -n 100 -burst 300         # 慢客户端注入
//
// 前置条件：后端已启动（channel 模式），MySQL/Redis 可用；测试用户与群组由本工具直接写入
// 数据库（绕过注册接口，避免短信依赖），WS 连接使用本地签发的 Access Token。
package main

import (
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
	mode     = flag.String("mode", "conn", "conn | chat | group | slow")
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
			go conn.readLoop(nil, nil) // 消费消息（含心跳 Pong 由库自动处理）
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

	// 持续保持连接，观察稳定性
	keep := time.Second * time.Duration(seconds)
	ticker := time.NewTicker(keep / 10)
	defer ticker.Stop()
	lastConnected := connected
	for i := 0; i < 10; i++ {
		<-ticker.C
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Printf("  t+%ds: 连接 %d, goroutine %d, 堆内存 %.1f MB\n",
			int64(keep.Seconds())*int64(i+1)/10, lastConnected, runtime.NumGoroutine(), float64(m.HeapAlloc)/1024/1024)
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
				content := fmt.Sprintf("chat-%d-%d", idx, seq)
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
	for i, uuid := range uuids {
		go func(idx int, u string) {
			defer wg.Done()
			conn, err := dial(wsAddr, u)
			if err != nil {
				atomic.AddInt64(&failedDial, 1)
				return
			}
			conns[idx] = conn
			go conn.readLoop(func(data []byte) {
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
				atomic.AddInt64(&delivered, 1)
			}, func() { atomic.AddInt64(&closedNormal, 1) })
		}(i)
	}
	wg.Wait()

	// 慢客户端：建立连接但完全不读
	slowConn, err := dial(wsAddr, slowUUID)
	if err != nil {
		log.Fatalf("dial slow client: %v", err)
	}
	slowClosed := make(chan struct{})
	go func() {
		_, _, _ = slowConn.ws.ReadMessage() // 阻塞直到被服务端断开
		close(slowClosed)
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
