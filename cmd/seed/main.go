// cmd/seed —— 演示数据重建工具。
//
// 目标:把数据库重置成一套"像真实在生产使用"的演示数据:
//   - 14 个拟真用户(图库照片/生成插画头像混合,密码统一 123456,bcrypt 落库)
//   - 3 个群聊(项目组 / 羽毛球俱乐部 / 家人群,公告、加群方式、成员齐全)
//   - 联系人关系图 + 1 条待处理好友申请(前端有红点)
//   - 单聊 + 群聊的中文对话剧本,时间戳摊在最近两周,穿插图片 / PDF 消息
//
// 所有行的写入口径与业务代码保持同构:
//   - 好友关系 = user_contact 双向两行(见 PassContactApply)
//   - 群成员 = group_info.members JSON + 每人一条 user_contact(type=1)
//   - 群会话 = 每个成员一条 session(send_id=成员, receive_id=群)
//   - 单聊消息 receive_id=对端用户,群消息 receive_id=群 uuid
//   - 文件/图片消息 url 存后端绝对 URL(与前端上传后拼 backendUrl 的行为一致)
//
// 用法(仓库根目录执行;docker 部署时把静态目录指到 bind mount 宿主机路径):
//
//	go run ./cmd/seed --force \
//	  --avatar-dir D:/develop/docker_workspace/gochat/avatars \
//	  --files-dir  D:/develop/docker_workspace/gochat/files
//
// --force 之前是 dry-run:只打印将要写入的内容,不动数据库。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/go-redis/redis/v8"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"gochat/internal/model"
)

// ---------- 命令行 ----------

var (
	mysqlDSN   = flag.String("mysql-dsn", "root:123456@tcp(127.0.0.1:3306)/gochat?charset=utf8mb4&parseTime=True&loc=Local", "MySQL DSN")
	redisAddr  = flag.String("redis-addr", "127.0.0.1:6381", "Redis 地址(seed 后 FLUSHDB,清掉旧缓存)")
	avatarDir  = flag.String("avatar-dir", "./static/avatars", "头像静态目录(docker 部署传 bind mount 宿主机路径)")
	filesDir   = flag.String("files-dir", "./static/files", "聊天文件静态目录(docker 部署传 bind mount 宿主机路径)")
	publicBase = flag.String("public-base", "http://localhost:8000", "后端对外地址,文件/图片消息 url 的前缀")
	force      = flag.Bool("force", false, "真正执行清库 + 写入;缺省为 dry-run")
	skipRedis  = flag.Bool("skip-redis", false, "跳过 Redis FLUSHDB(不建议:旧缓存会导致列表读到旧数据)")
)

// ---------- 时间锚点 ----------

var now = time.Now()

// d 返回 n 天前的指定时刻(仅精确到分钟,和真实聊天节奏一致)。
func d(n int, clock string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", now.Format("2006-01-02")+" "+clock, time.Local)
	if err != nil {
		panic(err)
	}
	return t.AddDate(0, 0, -n)
}

// mo 返回 n 个月前。
func mo(n int) time.Time { return now.AddDate(0, -n, 0) }

// ---------- 用户 ----------

const seedPwd = "123456"

// 头像统一走仓库内素材(static/avatars/seed),前端按相对路径拼 backendUrl。
func av(name string) string { return "/static/avatars/seed/" + name }

type persona struct {
	u model.UserInfo
}

func personas() []persona {
	mk := func(uuid, nick, phone, email, avatar string, gender int8, sign, birthday string, createdAgoMonths int, admin bool) persona {
		return persona{u: model.UserInfo{
			Uuid:      uuid,
			Nickname:  nick,
			Telephone: phone,
			Email:     email,
			Avatar:    avatar,
			Gender:    gender,
			Signature: sign,
			Birthday:  birthday,
			CreatedAt: mo(createdAgoMonths),
			IsAdmin:   boolToInt8(admin),
			Status:    0,
		}}
	}
	return []persona{
		// 两位"主演示账号"(真实手机号;演示环境短信为开发模式,验证码只写 Redis/日志):
		mk("U10000000001", "陈默", "18387172912", "chenmo1992@163.com", av("u_chenmo.jpg"), 0, "把系统做简单,把细节做扎实", "19920318", 14, true),
		mk("U10000000002", "林晚晴", "15621173723", "wanqing.l@vip.qq.com", av("u_linwanqing.jpg"), 1, "像素眼,细节控", "19950722", 13, false),
		// 其余为虚构号码(仅密码登录):
		mk("U10000000003", "周子航", "13812763345", "zhouzihang@outlook.com", av("u_zhouzihang.jpg"), 0, "Kafka 与我同在", "19931105", 13, false),
		mk("U10000000004", "苏念", "15902461877", "sunian0724@126.com", av("u_sunian.jpg"), 1, "组件比我整齐", "19970914", 12, false),
		mk("U10000000005", "江屿", "18653104082", "jiangyu.qa@gmail.com", av("u_jiangyu.jpg"), 0, "所以,压测跑了吗", "19920601", 12, false),
		mk("U10000000006", "顾云帆", "17710429653", "guyunfan@163.com", av("u_guyunfan.jpg"), 0, "一切皆可 compose", "19941220", 11, false),
		mk("U10000000007", "沈亦舒", "15076082931", "shenyishu@foxmail.com", av("u_shenyishu.jpg"), 1, "需求不砍,排期不加", "19920409", 13, false),
		mk("U10000000008", "韩梓萌", "13031849570", "hanzimeng@qq.com", av("u_hanzimeng.jpg"), 1, "在学习,勿cue(可以cue)", "20020101", 3, false),
		mk("U10000000009", "陆则鸣", "19924570618", "luzeming@163.com", av("u_luzeming.jpg"), 0, "周六不见不散", "19890808", 10, false),
		mk("U10000000010", "白若溪", "16602851473", "bairuoxi@qq.com", av("u_bairuoxi.jpg"), 1, "混双带飞", "19961030", 9, false),
		mk("U10000000011", "陈鹤鸣", "13572184960", "chenheming@163.com", av("u_chenheming.jpg"), 0, "出差ing", "19881001", 8, false),
		mk("U10000000012", "王秀兰", "15103278094", "wxl1965@126.com", av("u_wangxiulan.jpg"), 1, "记得吃饭", "19650512", 6, false),
		mk("U10000000013", "许清如", "18630175492", "xuqingru@qq.com", av("u_xuqingru.jpg"), 1, "带娃ing", "19910823", 6, false),
		mk("U10000000014", "何笑寒", "15020468137", "hexiaohan@163.com", av("u_hexiaohan.jpg"), 0, "产品经理,交个朋友", "19930327", 0, false),
	}
}

// 用户昵称 -> uuid 速查(剧本里用昵称引用发送者,可读性好)。
var uid = map[string]string{}

func init() {
	for _, p := range personas() {
		uid[p.u.Nickname] = p.u.Uuid
	}
}

// ---------- 群聊 ----------

type seedGroup struct {
	g        model.GroupInfo
	joinAgo  int // 建群时间(月前)
	members  []string
	joinDays map[string]int // 成员入群时间(天前,缺省=建群时)
}

func groups() []seedGroup {
	jsonArr := func(ids []string) []byte {
		out := []byte("[")
		for i, id := range ids {
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, '"')
			out = append(out, []byte(id)...)
			out = append(out, '"')
		}
		return append(out, ']')
	}
	members := func(names ...string) []string {
		var ids []string
		for _, n := range names {
			ids = append(ids, uid[n])
		}
		return ids
	}
	return []seedGroup{
		{
			g: model.GroupInfo{
				Uuid:      "G10000001001",
				Name:      "GoChat 项目组",
				Notice:    "v0.9 周五发版。发布清单与压测报告见群文件;周会周三 20:00,回归问题当天群里同步。",
				OwnerId:   uid["陈默"],
				AddMode:   1, // 需群主审核
				Avatar:    av("g_gochat.jpg"),
				Status:    0,
				Members:   jsonArr(members("陈默", "林晚晴", "周子航", "苏念", "江屿", "顾云帆", "沈亦舒", "韩梓萌")),
				MemberCnt: 8,
			},
			joinAgo:  10,
			members:  members("陈默", "林晚晴", "周子航", "苏念", "江屿", "顾云帆", "沈亦舒", "韩梓萌"),
			joinDays: map[string]int{"韩梓萌": 90, "沈亦舒": 290},
		},
		{
			g: model.GroupInfo{
				Uuid:      "G10000001002",
				Name:      "周末羽毛球俱乐部",
				Notice:    "每周六上午 9:00-11:00,奥体中心 6 号场,AA 制,带拍带水。",
				OwnerId:   uid["陆则鸣"],
				AddMode:   0, // 免审核直加
				Avatar:    av("g_badminton.jpg"),
				Status:    0,
				Members:   jsonArr(members("陆则鸣", "白若溪", "陈默", "林晚晴", "江屿", "陈鹤鸣")),
				MemberCnt: 6,
			},
			joinAgo:  9,
			members:  members("陆则鸣", "白若溪", "陈默", "林晚晴", "江屿", "陈鹤鸣"),
			joinDays: map[string]int{"陈鹤鸣": 60, "江屿": 120},
		},
		{
			g: model.GroupInfo{
				Uuid:      "G10000001003",
				Name:      "家人群·暖灯",
				Notice:    "周末回家吃饭要说一声,爸妈好买菜。",
				OwnerId:   uid["王秀兰"],
				AddMode:   1,
				Avatar:    av("g_family.jpg"),
				Status:    0,
				Members:   jsonArr(members("王秀兰", "陈默", "陈鹤鸣", "许清如")),
				MemberCnt: 4,
			},
			joinAgo: 6,
			members: members("王秀兰", "陈默", "陈鹤鸣", "许清如"),
		},
	}
}

// ---------- 消息剧本 ----------

type msg struct {
	from  string // 发送者昵称
	typ   int8   // 0 文本 / 2 文件
	text  string // 文本内容
	url   string // 文件相对 url(不含 publicBase)
	mime  string // file_type(image/png、application/pdf...)
	fname string // file_name
	fsize string // file_size
	at    time.Time
}

// img 便捷构造图片消息。
func img(from, file, name, size string, at time.Time) msg {
	return msg{from: from, typ: 2, url: "/static/files/seed/" + file, mime: "image/png", fname: name, fsize: size, at: at}
}

// pdfMsg 便捷构造 PDF 文件消息。
func pdfMsg(from, name, size string, at time.Time) msg {
	return msg{from: from, typ: 2, url: "/static/files/seed/kafka_idempotent_notes.pdf", mime: "application/pdf", fname: name, fsize: size, at: at}
}

// 群消息剧本:群 uuid -> 消息(时间升序)。
func groupScripts() map[string][]msg {
	return map[string][]msg{
		"G10000001001": {
			{from: "沈亦舒", text: "v0.9 发布清单我整理好了,就差压测报告", at: d(6, "10:05")},
			{from: "陈默", text: "辛苦,报告江屿这两天出", at: d(6, "10:07")},
			{from: "江屿", text: "今天跑最后一轮 soak,明早给结论", at: d(6, "10:08")},
			{from: "江屿", text: "soak 结果:600s 零掉线,goroutine 恒定,堆内存无增长", at: d(5, "09:30")},
			{from: "周子航", text: "漂亮。Kafka 批量落库也合进去了,单实例能到 1000 msg/s", at: d(5, "09:31")},
			{from: "苏念", text: "前端 WS 重连优化也进了,v0.9 一起带上?", at: d(5, "09:33")},
			{from: "陈默", text: "带,都进 v0.9", at: d(5, "09:35")},
			{from: "沈亦舒", text: "那我更新发布清单和周会 agenda", at: d(5, "09:36")},
			{from: "林晚晴", text: "会话列表改版稿已交付,今天开始设计走查", at: d(2, "16:20")},
			{from: "沈亦舒", text: "明天提测,大家手头的活收个尾", at: d(2, "16:22")},
			{from: "韩梓萌", text: "收到~", at: d(2, "16:23")},
			{from: "陈默", text: "提测包已部署到演示环境,下午开始回归", at: d(0, "10:01")},
			{from: "顾云帆", text: "演示环境重启完了,可以直接用", at: d(0, "10:02")},
			{from: "陈默", text: "有问题群里说,周五发版 🚀", at: d(0, "10:03")},
		},
		"G10000001002": {
			{from: "陆则鸣", text: "周六场地订好了,奥体 6 号场,9:00-11:00", at: d(3, "12:10")},
			{from: "白若溪", text: "收到!这周混双还是随机配对?", at: d(3, "12:11")},
			{from: "江屿", text: "+1,上周那个反手还没练明白 😂", at: d(3, "12:12")},
			{from: "陈默", text: "我也来,正好试试新拍子", at: d(3, "12:15")},
			{from: "陈鹤鸣", text: "带我一个,周六刚好出差回来", at: d(3, "12:18")},
			{from: "陆则鸣", text: "好,6 个人两两轮换,时间够", at: d(3, "12:20")},
			{from: "林晚晴", text: "我的拍子断线了,有人带备用拍吗 🥲", at: d(3, "12:22")},
			{from: "陆则鸣", text: "我带两支,放心", at: d(3, "12:23")},
		},
		"G10000001003": {
			{from: "王秀兰", text: "周末回家吃饭吗?炖了排骨", at: d(6, "18:30")},
			{from: "陈默", text: "回,周六下午到", at: d(6, "18:32")},
			{from: "陈鹤鸣", text: "我也回", at: d(6, "18:33")},
			{from: "王秀兰", text: "都回来最好,你爸下午去买菜", at: d(6, "18:35")},
			{from: "许清如", text: "妈,我到的时候带点水果", at: d(6, "18:36")},
			img("陈鹤鸣", "drawing_robot.png", "小侄子画的机器人.png", "65 KB", d(4, "21:10")),
			{from: "陈默", text: "有点东西,画得比我好", at: d(4, "21:12")},
			{from: "许清如", text: "哈哈哈他就瞎画,天天画机器人", at: d(4, "21:15")},
			{from: "王秀兰", text: "画得好!周末带回来给奶奶看看", at: d(4, "21:18")},
			{from: "王秀兰", text: "粽子到了,回来记得拿", at: d(3, "09:20")},
		},
	}
}

// 单聊剧本:一屏对话 = 一条 session(a 发起, receive=b)。
type chat struct {
	a, b      string
	sessionId string
	msgs      []msg
}

func singleChats() []chat {
	return []chat{
		{
			a: "陈默", b: "林晚晴", sessionId: "S10000001001",
			msgs: []msg{
				{from: "林晚晴", text: "会话列表改版的第一稿画好了,共享盘里,明天你有空看看", at: d(13, "21:30")},
				{from: "陈默", text: "好,明天上午看完给你标注", at: d(13, "21:32")},
				{from: "林晚晴", text: "未读红点和时间的层级我重新排了,列表扫读舒服很多", at: d(10, "14:10")},
				{from: "陈默", text: "嗯,时间右上角是对的。分组折叠先不做,别把一期做重了", at: d(10, "14:15")},
				{from: "林晚晴", text: "空状态插画定稿了,一只打瞌睡的柯基 🐶", at: d(7, "20:02")},
				{from: "陈默", text: "哈哈这个好,比灰色占位图强多了", at: d(7, "20:05")},
				{from: "林晚晴", text: "那我就入库了~", at: d(7, "20:06")},
				{from: "林晚晴", text: "设计稿全部交付,明天可以提测 🎉", at: d(1, "21:40")},
				img("林晚晴", "design_delivery.png", "会话列表改版-最终稿.png", "58 KB", d(1, "21:41")),
				{from: "陈默", text: "收到,明天提测我拉上子航一起过一遍", at: d(1, "21:45")},
				{from: "陈默", text: "辛苦了,早点休息 🌙", at: d(1, "21:46")},
			},
		},
		{
			a: "陈默", b: "周子航", sessionId: "S10000001002",
			msgs: []msg{
				{from: "周子航", text: "Kafka 消费幂等的修复我合到 develop 了", at: d(1, "11:20")},
				{from: "陈默", text: "好,把重复消费那条压测用例再跑一遍", at: d(1, "11:21")},
				{from: "周子航", text: "跑完了,13438 条重复消息,DB 零新增", at: d(1, "15:45")},
				pdfMsg("周子航", "kafka消费幂等修复说明.pdf", "12 KB", d(1, "15:46")),
				{from: "陈默", text: "看了,没问题。这周发 v0.9.1 把它带上", at: d(1, "16:02")},
				{from: "周子航", text: "OK", at: d(1, "16:03")},
			},
		},
		{
			a: "陈默", b: "苏念", sessionId: "S10000001003",
			msgs: []msg{
				{from: "苏念", text: "WS 重连的退避改成随机抖动了,弱网模拟下好多了", at: d(4, "19:30")},
				{from: "陈默", text: "好,pending 队列上限也要有,别无限攒", at: d(4, "19:32")},
				{from: "苏念", text: "加了,100 条,超了丢最旧的并提示", at: d(4, "19:33")},
				{from: "陈默", text: "👍", at: d(4, "19:35")},
			},
		},
		{
			a: "陈默", b: "江屿", sessionId: "S10000001004",
			msgs: []msg{
				{from: "江屿", text: "soak 监控截图,600s 内连接数和内存都很平稳", at: d(5, "10:15")},
				img("江屿", "soak_monitor.png", "soak监控-600s.png", "43 KB", d(5, "10:16")),
				{from: "陈默", text: "报告我来写进压测文档,这张图给我用用", at: d(5, "10:18")},
				{from: "江屿", text: "拿去,原始数据在 bench 目录", at: d(5, "10:19")},
			},
		},
		{
			a: "陈默", b: "顾云帆", sessionId: "S10000001005",
			msgs: []msg{
				{from: "顾云帆", text: "compose 健康检查都补上了,backend 用端口探测", at: d(8, "15:00")},
				{from: "陈默", text: "行,镜像周四发版前打好标签", at: d(8, "15:02")},
				{from: "顾云帆", text: "好,发版我盯着监控", at: d(8, "15:03")},
			},
		},
		{
			a: "陈默", b: "韩梓萌", sessionId: "S10000001006",
			msgs: []msg{
				{from: "韩梓萌", text: "默哥,refresh token 轮换那块我有个地方没看懂", at: d(11, "10:20")},
				{from: "陈默", text: "你想成旧票换新票:旧票用第二次,说明泄露了,全部作废", at: d(11, "10:25")},
				{from: "韩梓萌", text: "懂了!我去画个时序图", at: d(11, "10:26")},
				{from: "陈默", text: "画完发群里,大家帮你看看", at: d(11, "10:30")},
			},
		},
		{
			a: "陈默", b: "白若溪", sessionId: "S10000001007",
			msgs: []msg{
				{from: "白若溪", text: "上周双打太尽兴了,下周还约吗", at: d(13, "22:40")},
				{from: "陈默", text: "约,群里刚定了周六奥体", at: d(13, "22:41")},
				{from: "白若溪", text: "看到啦,这次别又输给我们混双组合 😏", at: d(13, "22:42")},
			},
		},
		{
			a: "林晚晴", b: "苏念", sessionId: "S10000001008",
			msgs: []msg{
				{from: "苏念", text: "走查第三轮了,两处间距问题我记到清单里了", at: d(2, "17:00")},
				{from: "林晚晴", text: "辛苦,明早我改完你再验一遍", at: d(2, "17:02")},
				{from: "苏念", text: "好嘞", at: d(2, "17:03")},
			},
		},
		{
			a: "林晚晴", b: "江屿", sessionId: "S10000001009",
			msgs: []msg{
				{from: "江屿", text: "提测范围我按发布清单建好了用例集", at: d(3, "09:00")},
				{from: "林晚晴", text: "好,设计走查的问题我标给你", at: d(3, "09:01")},
				{from: "江屿", text: "👌", at: d(3, "09:02")},
			},
		},
	}
}

// ---------- 主流程 ----------

func main() {
	flag.Parse()

	db, err := gorm.Open(mysql.Open(*mysqlDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	must(err, "连接 MySQL")

	// 全量建表(全新 volume 时可以先于后端执行 seed)。
	must(db.AutoMigrate(
		&model.UserInfo{}, &model.UserContact{}, &model.ContactApply{},
		&model.GroupInfo{}, &model.Session{}, &model.Message{},
	), "AutoMigrate")

	ps := personas()
	gs := groups()
	gsMsgs := groupScripts()
	chats := singleChats()
	nMsg := chatsMsgCount(chats)
	for _, s := range gsMsgs {
		nMsg += len(s)
	}
	nGroupSession := 0
	for _, g := range gs {
		nGroupSession += len(g.members)
	}

	fmt.Printf(`GoChat 演示数据 seed
  用户 %d 个(密码统一 %s,管理员:陈默)
  群聊 %d 个 / 群成员会话 %d 条 / 单聊会话 %d 条 / 消息 %d 条 / 待处理好友申请 1 条
  静态资产 -> 头像目录 %s / 文件目录 %s
`, len(ps), seedPwd, len(gs), nGroupSession, len(chats), nMsg, *avatarDir, *filesDir)

	if !*force {
		fmt.Println("\n[dry-run] 加 --force 执行:清空 6 张业务表 -> Redis FLUSHDB -> 写入以上数据。")
		return
	}

	// 1. 清库(消息最先,其次申请/联系人/会话/群/用户)。
	for _, t := range []string{"message", "contact_apply", "user_contact", "session", "group_info", "user_info"} {
		must(db.Exec("DELETE FROM "+t).Error, "清空 "+t)
		must(db.Exec("ALTER TABLE "+t+" AUTO_INCREMENT = 1").Error, "重置自增 "+t)
	}

	// 2. Redis 整库清空(旧缓存会污染新数据,顺带清掉旧登录态)。
	if !*skipRedis {
		rdb := redis.NewClient(&redis.Options{Addr: *redisAddr})
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			fatalf("Redis 不可达(%v):请先启动,或用 --skip-redis 跳过(不建议)", err)
		}
		must(rdb.FlushDB(context.Background()).Err(), "FLUSHDB")
		fmt.Println("Redis FLUSHDB 完成")
	}

	// 3. 写用户(bcrypt 逐人哈希,和真实注册落库一致)。
	for _, p := range ps {
		u := p.u
		hash, err := bcrypt.GenerateFromPassword([]byte(seedPwd), bcrypt.DefaultCost)
		must(err, "bcrypt")
		u.Password = string(hash)
		u.LastOnlineAt = sqlNullTime(now.Add(-2 * time.Hour))
		u.LastOfflineAt = sqlNullTime(now.Add(-1 * time.Hour))
		must(db.Create(&u).Error, "写入用户 "+u.Nickname)
	}

	// 4. 写群 + 群成员 user_contact + 每成员一条群会话。
	// groupSessionOf["uuid|群uuid"] -> 该成员的群会话 uuid,群消息要引用。
	groupSessionOf := map[string]string{}
	gSessionSeq := 0
	for _, sg := range gs {
		g := sg.g
		g.CreatedAt = mo(sg.joinAgo)
		g.UpdatedAt = now.Add(-48 * time.Hour)
		must(db.Create(&g).Error, "写入群 "+g.Name)
		for _, m := range sg.members {
			join := mo(sg.joinAgo)
			for name, days := range sg.joinDays {
				if uid[name] == m {
					join = now.AddDate(0, 0, -days)
				}
			}
			must(db.Create(&model.UserContact{
				UserId: m, ContactId: g.Uuid, ContactType: 1, Status: 0,
				CreatedAt: join, UpdateAt: join,
			}).Error, "写入群成员关系 "+g.Name)

			gSessionSeq++
			sid := fmt.Sprintf("S20000001%03d", gSessionSeq)
			groupSessionOf[m+"|"+g.Uuid] = sid
			last := lastMsgOf(gsMsgs[g.Uuid])
			must(db.Create(&model.Session{
				Uuid: sid, SendId: m, ReceiveId: g.Uuid,
				ReceiveName: g.Name, Avatar: g.Avatar,
				LastMessage: displayText(last), LastMessageAt: sqlNullTime(last.at),
				CreatedAt: join,
			}).Error, "写入群会话 "+g.Name)
		}
	}

	// 5. 好友关系(双向两行,加好友时间错开)。
	friendPairs := [][2]string{
		{"陈默", "林晚晴"}, {"陈默", "周子航"}, {"陈默", "苏念"}, {"陈默", "江屿"},
		{"陈默", "顾云帆"}, {"陈默", "沈亦舒"}, {"陈默", "韩梓萌"}, {"陈默", "陆则鸣"},
		{"陈默", "白若溪"}, {"陈默", "陈鹤鸣"}, {"陈默", "王秀兰"}, {"陈默", "许清如"},
		{"林晚晴", "苏念"}, {"林晚晴", "江屿"}, {"周子航", "苏念"}, {"江屿", "陆则鸣"},
		{"林晚晴", "周子航"},
	}
	for i, pair := range friendPairs {
		t := now.AddDate(0, 0, -(150 + i*11))
		for _, dir := range [][2]string{pair, {pair[1], pair[0]}} {
			must(db.Create(&model.UserContact{
				UserId: uid[dir[0]], ContactId: uid[dir[1]], ContactType: 0, Status: 0,
				CreatedAt: t, UpdateAt: t,
			}).Error, "写入好友关系 "+dir[0]+"-"+dir[1])
		}
	}

	// 6. 待处理好友申请(陈默登录后联系人入口有红点)。
	must(db.Create(&model.ContactApply{
		Uuid: "A10000001001", UserId: uid["何笑寒"], ContactId: uid["陈默"],
		ContactType: 0, Status: 0,
		Message:     "我是晚晴的朋友何笑寒,做协同工具产品,想交流一下~",
		LastApplyAt: now.Add(-6 * time.Hour),
	}).Error, "写入好友申请")

	// 7. 单聊会话 + 消息(session.CreatedAt 取最后一条消息时间,会话列表按此排序)。
	seq := 0
	for _, c := range chats {
		a, b := uid[c.a], uid[c.b]
		var bUser model.UserInfo
		must(db.Where("uuid = ?", b).First(&bUser).Error, "查会话对端 "+c.b)
		last := c.msgs[len(c.msgs)-1]
		must(db.Create(&model.Session{
			Uuid: c.sessionId, SendId: a, ReceiveId: b,
			ReceiveName: bUser.Nickname, Avatar: bUser.Avatar,
			LastMessage: displayText(last), LastMessageAt: sqlNullTime(last.at),
			CreatedAt: last.at,
		}).Error, "写入会话 "+c.a+"-"+c.b)
		for _, m := range c.msgs {
			seq++
			// 接收方 = 发送者在会话里的对方(自己发的给对端,对端发的给自己)。
			recv := a
			if uid[m.from] == a {
				recv = b
			}
			mm := buildMessage(fmt.Sprintf("M1000001%04d", seq), c.sessionId, m, recv)
			must(db.Create(&mm).Error, "写入单聊消息")
		}
	}

	// 8. 群消息(receive_id = 群 uuid,session_id = 发送者自己的群会话)。
	for _, sg := range gs {
		for _, m := range gsMsgs[sg.g.Uuid] {
			seq++
			gm := buildMessage(fmt.Sprintf("M1000001%04d", seq), groupSessionOf[uid[m.from]+"|"+sg.g.Uuid], m, sg.g.Uuid)
			must(db.Create(&gm).Error, "写入群消息")
		}
	}

	// 9. 静态资产落盘(头像/文件目录可能指向 docker bind mount 的宿主机路径)。
	assets := buildAssets()
	for dst, content := range assets {
		must(os.MkdirAll(filepath.Dir(dst), 0o755), "建目录")
		must(os.WriteFile(dst, content, 0o644), "写文件 "+dst)
	}

	fmt.Printf(`
完成:用户 %d / 群 %d / 好友关系 %d 对 / 会话 %d / 消息 %d / 申请 1 / 资产 %d
演示账号:
  陈默(管理员) 18387172912 / %s
  林晚晴        15621173723 / %s
其余账号为虚构手机号(仅密码登录);演示环境短信为开发模式,验证码只写 Redis/日志,不真实发送。
`, len(ps), len(gs), len(friendPairs), len(chats)+nGroupSession, seq, len(assets), seedPwd, seedPwd)
}

// ---------- 辅助 ----------

func chatsMsgCount(chats []chat) (n int) {
	for _, c := range chats {
		n += len(c.msgs)
	}
	return n
}

func lastMsgOf(msgs []msg) msg {
	if len(msgs) == 0 {
		fatalf("群消息剧本为空")
	}
	return msgs[len(msgs)-1]
}

func buildMessage(uuid, sessionId string, m msg, receiveId string) model.Message {
	sender := personaByName(m.from)
	mm := model.Message{
		Uuid:       uuid,
		SessionId:  sessionId,
		Type:       m.typ,
		Content:    m.text,
		SendId:     sender.Uuid,
		SendName:   sender.Nickname,
		SendAvatar: sender.Avatar,
		ReceiveId:  receiveId,
		Status:     1,
		CreatedAt:  m.at,
		SendAt:     sqlNullTime(m.at),
	}
	if m.typ == 2 {
		mm.Url = *publicBase + m.url
		mm.FileType = m.mime
		mm.FileName = m.fname
		mm.FileSize = m.fsize
	}
	return mm
}

func displayText(m msg) string {
	if m.typ == 2 {
		return "[文件] " + m.fname
	}
	return m.text
}

func personaByName(nick string) model.UserInfo {
	for _, p := range personas() {
		if p.u.Nickname == nick {
			return p.u
		}
	}
	fatalf("剧本里引用了不存在的昵称:%s", nick)
	return model.UserInfo{}
}

// buildAssets 汇总要落盘的静态资产:仓库 seed 目录里的头像与图片原样复制,
// PDF 现场生成(内容固定,可复现)。
func buildAssets() map[string][]byte {
	out := map[string][]byte{}
	copyDir := func(src, dst string) {
		entries, err := os.ReadDir(src)
		if err != nil {
			fatalf("读取素材目录 %s 失败:%v(请在仓库根目录运行)", src, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(src, e.Name()))
			must(err, "读素材 "+e.Name())
			out[filepath.Join(dst, e.Name())] = data
		}
	}
	copyDir(filepath.Join(".", "static", "avatars", "seed"), filepath.Join(*avatarDir, "seed"))
	copyDir(filepath.Join(".", "static", "files", "seed"), filepath.Join(*filesDir, "seed"))
	out[filepath.Join(*filesDir, "seed", "kafka_idempotent_notes.pdf")] = minimalPDF()
	return out
}

// minimalPDF 手写一个单页 PDF(Helvetica,无外部依赖),作为群/单聊里的文件消息载体。
func minimalPDF() []byte {
	content := "" +
		"BT /F1 16 Tf 56 760 Td (GoChat - Kafka Consumption Idempotency Notes) Tj ET\n" +
		"BT /F1 11 Tf 56 730 Td (v0.9.1  ·  author: Zhou Zihang) Tj ET\n" +
		"BT /F1 11 Tf 56 700 Td (1. Dedup: SETNX redis key + message uuid unique index) Tj ET\n" +
		"BT /F1 11 Tf 56 680 Td (2. KafkaKey column: topic:partition:offset - atomic with insert) Tj ET\n" +
		"BT /F1 11 Tf 56 660 Td (3. Bench result: 13438 duplicated messages, 0 extra rows) Tj ET\n" +
		"BT /F1 11 Tf 56 640 Td (4. Rollout plan attached in release checklist) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		"<< /Length " + fmt.Sprint(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var buf []byte
	buf = append(buf, "%PDF-1.4\n"...)
	offsets := []int{0}
	for i, o := range objs {
		offsets = append(offsets, len(buf))
		buf = append(buf, fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, o)...)
	}
	xrefAt := len(buf)
	buf = append(buf, fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)...)
	for _, off := range offsets[1:] {
		buf = append(buf, fmt.Sprintf("%010d 00000 n \n", off)...)
	}
	buf = append(buf, fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefAt)...)
	return buf
}

func sqlNullTime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }

func boolToInt8(b bool) int8 {
	if b {
		return 1
	}
	return 0
}

func must(err error, what string) {
	if err != nil {
		fatalf("%s 失败:%v", what, err)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Printf("seed: "+format+"\n", args...)
	os.Exit(1)
}
