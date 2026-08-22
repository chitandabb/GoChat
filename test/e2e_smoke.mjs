// test/e2e_smoke.mjs —— 前后端全链路冒烟(Node >= 22,原生 fetch/WebSocket,零依赖)。
//
// 前置:docker compose up -d 且已执行 `go run ./cmd/seed --force`。
// 用法:node test/e2e_smoke.mjs [apiBase]   (默认 http://localhost:8000)
//
// 覆盖:密码登录、联系人/会话/消息/群消息/好友申请列表、WebSocket 双账号实时互发、
//       管理端用户列表、静态资源(头像/图片/PDF)、开发模式短信验证码(仅触达允许的真实号码)。
// 注意:WS 互发使用"何笑寒 × 白若溪"(无演示剧本的账号),测试消息以 e2e: 前缀落库,
//       跑完执行本文件末尾打印的清理 SQL 即可还原演示数据。
import { execSync } from "node:child_process";

const API = process.argv[2] || "http://localhost:8000";
const results = [];
const ok = (name, cond, extra = "") => {
  results.push({ name, pass: !!cond });
  console.log(`${cond ? "PASS" : "FAIL"}  ${name}${extra ? "  -> " + extra : ""}`);
};

const post = async (path, body, token) => {
  const rsp = await fetch(API + path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: "Bearer " + token } : {}),
    },
    body: JSON.stringify(body ?? {}),
  });
  return rsp.json();
};
const login = async (tel) => {
  const r = await post("/api/v1/auth/login", { telephone: tel, password: "123456" });
  if (r.code !== 0) throw new Error("login failed: " + tel + " " + r.message);
  return r.data;
};
const waitOpen = (ws) => new Promise((res, rej) => { ws.onopen = res; ws.onerror = (e) => rej(new Error("ws error")); });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const cm = await login("18387172912"); // 陈默·管理员
const lq = await login("15621173723"); // 林晚晴

// ---------- 1. 密码登录 ----------
ok("登录·陈默(管理员)", cm.user_info.is_admin === 1, "uuid=" + cm.user_info.uuid);
ok("登录·林晚晴", !!lq.access_token, "uuid=" + lq.user_info.uuid);
{
  const bad = await post("/api/v1/auth/login", { telephone: "18387172912", password: "wrong" });
  ok("登录·错误密码被拒", bad.code !== 0, "code=" + bad.code);
}

// ---------- 2. 列表类接口(seed 数据形状) ----------
{
  const contacts = await post("/api/v1/contact/getUserList", {}, cm.access_token);
  ok("联系人列表 12 人", contacts.code === 0 && contacts.data.length === 12, `n=${contacts.data?.length}`);

  const sessions = await post("/api/v1/session/getUserSessionList", {}, cm.access_token);
  ok("单聊会话 7 条", sessions.code === 0 && sessions.data.length === 7, `n=${sessions.data?.length}`);

  const groups = await post("/api/v1/session/getGroupSessionList", {}, cm.access_token);
  const gochat = groups.data?.find((g) => g.group_name === "GoChat 项目组");
  ok("群会话 3 条(含项目组)", groups.code === 0 && groups.data.length === 3 && !!gochat,
    `n=${groups.data?.length}`);

  const joined = await post("/api/v1/contact/loadMyJoinedGroup", {}, cm.access_token);
  // 语义为"加入了的群"(非自己创建):陈默是项目组群主,不计入。
  ok("已加入群 2 个(项目组为陈默所建,不计入)", joined.code === 0 && joined.data.length === 2, `n=${joined.data?.length}`);

  const apply = await post("/api/v1/contact/getNewContactList", {}, cm.access_token);
  ok("待处理好友申请 1 条(何笑寒)", apply.code === 0 && apply.data.length === 1 &&
    apply.data[0].contact_name === "何笑寒", `n=${apply.data?.length}`);
}

// ---------- 3. 消息历史 ----------
{
  const m = await post("/api/v1/message/getMessageList", {
    user_one_id: cm.user_info.uuid, user_two_id: lq.user_info.uuid,
  }, cm.access_token);
  const imgMsg = m.data?.find((x) => x.type === 2 && (x.file_type || "").startsWith("image/"));
  ok("单聊历史 11 条(含图片消息)", m.code === 0 && m.data.length === 11 && !!imgMsg,
    `n=${m.data?.length}`);

  const gm = await post("/api/v1/message/getGroupMessageList", { group_id: "G10000001001" }, cm.access_token);
  ok("项目群历史 14 条", gm.code === 0 && gm.data.length === 14, `n=${gm.data?.length}`);
}

// ---------- 4. WebSocket 双账号实时互发 ----------
{
  // 清掉历史运行残留,保证下面的落库计数确定。
  execSync(`docker exec gochat-mysql mysql -uroot -p123456 gochat -e "DELETE FROM message WHERE content LIKE 'e2e:%'"`, { stdio: "ignore" });
  const hx = await login("15020468137"); // 何笑寒
  const bai = await login("16602851473"); // 白若溪
  const wsUrl = API.replace(/^http/, "ws") + "/wss?token=";
  const inboxHx = [];
  const inboxBai = [];

  const wsHx = new WebSocket(wsUrl + hx.access_token);
  const wsBai = new WebSocket(wsUrl + bai.access_token);
  await Promise.all([waitOpen(wsHx), waitOpen(wsBai)]);
  wsHx.onmessage = (e) => { try { inboxHx.push(JSON.parse(e.data)); } catch { /* 欢迎语等非 JSON 帧 */ } };
  wsBai.onmessage = (e) => { try { inboxBai.push(JSON.parse(e.data)); } catch { /* 欢迎语等非 JSON 帧 */ } };

  const frame = (from, to, text) => ({
    session_id: "S99900000001", type: 0, content: text, url: "",
    send_id: from.user_info.uuid, send_name: from.user_info.nickname,
    send_avatar: from.user_info.avatar, receive_id: to.user_info.uuid,
    file_size: "", file_name: "", file_type: "",
  });
  wsBai.send(JSON.stringify(frame(bai, hx, "e2e: ping")));
  wsHx.send(JSON.stringify(frame(hx, bai, "e2e: pong")));
  await sleep(2500);

  ok("WebSocket 下行·何笑寒收到 ping", inboxHx.some((x) => x.content === "e2e: ping"), `frames=${inboxHx.length}`);
  ok("WebSocket 下行·白若溪收到 pong", inboxBai.some((x) => x.content === "e2e: pong"), `frames=${inboxBai.length}`);
  wsHx.close(); wsBai.close();

  // 先落库兜底校验(推送观察失败但落库成功仍算送达链路半通,两项分开断言)
  const cnt = execSync(
    `docker exec gochat-mysql mysql -uroot -p123456 gochat -N -e "SELECT COUNT(*) FROM message WHERE content LIKE 'e2e:%'"`,
    { encoding: "utf8" }
  ).trim();
  ok("WS 消息先落库(e2e: 前缀)", cnt === "2", "rows=" + cnt);
}

// ---------- 5. 管理端 ----------
{
  const list = await post("/api/v1/admin/getUserInfoList", {}, cm.access_token);
  // 接口语义:列出除操作者自己外的全部用户(13 = 14 - 1)。
  ok("管理端用户列表 13 人(除自己)", list.code === 0 && list.data.length === 13, `n=${list.data?.length}`);

  const forbidden = await post("/api/v1/admin/getUserInfoList", {}, lq.access_token);
  ok("非管理员访问管理端被拒", forbidden.code !== 0, "code=" + forbidden.code);
}

// ---------- 6. 静态资源 ----------
{
  for (const p of ["/static/avatars/seed/u_chenmo.jpg", "/static/files/seed/design_delivery.png", "/static/files/seed/kafka_idempotent_notes.pdf"]) {
    const rsp = await fetch(API + p);
    ok("静态资源 " + p, rsp.status === 200 && Number(rsp.headers.get("content-length")) > 1000,
      rsp.status + " " + rsp.headers.get("content-length") + "B");
  }
}

// ---------- 7. 开发模式短信(仅触达允许的真实号码) ----------
{
  const s = await post("/api/v1/auth/sendSmsCode", { telephone: "15621173723" });
  const code = execSync(`docker exec gochat-redis redis-cli --scan --pattern "*15621173723*"`, { encoding: "utf8" });
  ok("开发模式验证码(写 Redis 不真实发送)", s.code === 0 && code.includes("15621173723"),
    s.code === 0 ? "redis key 存在" : s.message);
}

// ---------- 汇总 ----------
const failed = results.filter((r) => !r.pass);
console.log(`\n== e2e 冒烟:${results.length - failed.length}/${results.length} 通过 ==`);
console.log('清理测试消息: docker exec gochat-mysql mysql -uroot -p123456 gochat -e "DELETE FROM message WHERE content LIKE \'e2e:%\'"');
if (failed.length) {
  console.log("失败项:", failed.map((f) => f.name).join(" | "));
  process.exit(1);
}
