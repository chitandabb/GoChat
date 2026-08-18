// 群推送链路实测：为两个真实成员各签一个 Access Token，开两条 WS,
// A 在群里发消息，验证双方是否都收到服务端推送（含回显）。
// 用法：node test/group_push_check.mjs <group_id> <uuidA> <uuidB>
import crypto from 'crypto'

const [groupId, uuidA, uuidB] = process.argv.slice(2)
if (!groupId || !uuidA || !uuidB) {
  console.error('用法: node group_push_check.mjs <group_id> <uuidA> <uuidB>')
  process.exit(1)
}

const SECRET = 'gochat-dev-secret-change-me'

function sign(uuid) {
  const header = { alg: 'HS256', typ: 'JWT' }
  const now = Math.floor(Date.now() / 1000)
  const payload = {
    uuid,
    is_admin: 0,
    iat: now,
    exp: now + 3600,
    jti: 'grouppushtest',
  }
  const b64 = (o) =>
    Buffer.from(JSON.stringify(o)).toString('base64url')
  const data = `${b64(header)}.${b64(payload)}`
  const sig = crypto.createHmac('sha256', SECRET).update(data).digest('base64url')
  return `${data}.${sig}`
}

function connect(name, uuid) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:8000/wss?token=${sign(uuid)}`)
    const frames = []
    ws.onopen = () => resolve({ name, ws, frames })
    ws.onerror = (e) => reject(new Error(`${name} 连接失败: ${e.message || e}`))
    ws.onmessage = (m) => {
      frames.push(m.data)
      console.log(`[${name}] 收到帧: ${String(m.data).slice(0, 160)}`)
    }
    ws.onclose = () => console.log(`[${name}] 连接关闭`)
  })
}

const a = await connect('A(' + uuidA + ')', uuidA)
const b = await connect('B(' + uuidB + ')', uuidB)
console.log('两个连接均已建立')

await new Promise((r) => setTimeout(r, 500))
const msg = {
  session_id: 'test',
  type: 0,
  content: '群推送实测 ping',
  url: '',
  send_id: uuidA,
  send_name: 'push-test-A',
  send_avatar: '',
  receive_id: groupId,
  file_size: '',
  file_name: '',
  file_type: '',
}
a.ws.send(JSON.stringify(msg))
console.log('[A] 已发送群消息,等待 3 秒观察推送...')
await new Promise((r) => setTimeout(r, 3000))

const aGot = a.frames.filter((f) => f.includes('群推送实测')).length
const bGot = b.frames.filter((f) => f.includes('群推送实测')).length
console.log(`\n结果: A 收到 ${aGot} 次, B 收到 ${bGot} 次`)
console.log(aGot >= 1 && bGot >= 1
  ? '✅ 群推送链路正常（若某端收到 2 次，即重复成员导致的重复推送）'
  : '❌ 群推送缺失，问题在服务端')
a.ws.close()
b.ws.close()
process.exit(0)
