#!/usr/bin/env node
// 造一份「演示范例集合」，供 UI 参考截图使用。
//
// 为什么要造数据而不是截空集合：空态看不出密度、行高、字段表这些真正要复刻的东西。
// 数据走 App 门面（devserver 的 POST /api/App/<Method>），保证落盘格式与实际使用完全一致。
//
// 用法：
//   APIDOC_BASE=http://127.0.0.1:8181 node tools/make_demo.mjs
import { existsSync, mkdirSync, readdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'

const BASE = process.env.APIDOC_BASE || 'http://127.0.0.1:8181'
// 夹具服务（cmd/testfixtures）：/json/* 与 /echo 能返回结构化 JSON，截图里的响应区才有内容可看。
const FIXTURES = process.env.APIDOC_FIXTURES || 'http://127.0.0.1:8188'
const OUT = path.resolve(new URL('.', import.meta.url).pathname, '../demo-collection')

async function call(method, ...args) {
  const res = await fetch(`${BASE}/api/App/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(args),
  })
  const text = await res.text()
  if (!res.ok || !text.trim().startsWith('{')) throw new Error(`${method} HTTP ${res.status}: ${text.slice(0, 300)}`)
  const payload = JSON.parse(text)
  if (!payload.ok) throw new Error(`${method}: ${payload.error}`)
  return payload.data
}

/**
 * 只允许「刚初始化过」的目录：devserver 启动时会建 manifest 与 environments/。
 * 已有业务内容说明是上一轮跑出来的 —— 直接叠加会出现两份同名目录，先清空再跑。
 */
const INIT_ARTIFACTS = new Set(['.gitignore', 'opencollection.yml', 'environments'])
function assertClean(dir) {
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true })
    return
  }
  for (const entry of readdirSync(dir)) {
    if (INIT_ARTIFACTS.has(entry)) continue
    throw new Error(`目录已有内容（${dir}/${entry}）：请先清空 demo-collection 再重跑`)
  }
}

const KV = (name, value, description = '', enabled = true) => ({ name, value, description, enabled })

async function mkRequest(folder, name, method, patch) {
  const created = await call('CreateRequest', folder, name, method)
  const cur = await call('ReadRequest', created.uid)
  const next = { ...cur, ...patch }
  await call('SaveRequest', next)
  return next
}

const respDoc = (type, params, sample) => ({
  type,
  params,
  sample: JSON.stringify(sample, null, 2),
  contentType: 'json',
  status: 200,
  headers: [KV('Content-Type', 'application/json')],
})

const demo = async () => {
  assertClean(OUT)
  await call('InitCollection', OUT)
  await call('RenameCollection', '示例项目') // 目录名叫 demo-collection，展示名用中文更接近真实项目
  // 显示名在打开时进了缓存：ReloadCollection 只刷树不刷名字，必须「关掉再打开」才看得到
  const roots = await call('ListCollections')
  for (const r of roots) await call('CloseCollection', r.root)
  await call('OpenCollection', OUT)

  // 环境变量：请求里的 {{host}} / {{token}} 靠它解析，缺了发送会直接失败
  await call('SaveEnv', {
    name: 'dev',
    vars: [
      { name: 'host', value: FIXTURES, enabled: true, secret: false },
      { name: 'token', value: 'demo-token-9f3c', enabled: true, secret: true },
    ],
  })

  // ---- 目录结构：两层 + 根请求，覆盖「分组嵌套」的缩进与连接线 ----
  await call('CreateFolder', '', '用户中心')
  await call('CreateFolder', '用户中心', '用户管理')
  await call('CreateFolder', '', '订单')
  await call('CreateFolder', '订单', '退款')

  // ---- 请求：参数 / 头部 / JSON 体 / 表单体 / 认证 / 响应字段 / 断言 / 文档 ----
  await mkRequest('用户中心', '登录', 'POST', {
    url: '{{host}}/echo',
    summary: '账号密码登录，返回访问令牌',
    headers: [KV('Content-Type', 'application/json'), KV('X-Client', 'desktop')],
    body: {
      type: 'json',
      raw: '{\n  "username": "alice",\n  "password": "{{token}}"\n}',
      form: [],
      fields: [
        { name: 'username', type: 'string', required: true, example: 'alice', description: '登录名' },
        { name: 'password', type: 'string', required: true, example: 's3cret', description: '密码' },
      ],
    },
    docs: '## 说明\n\n- 账号密码换令牌，有效期 2 小时\n- 失败返回 `401`，不要重试\n\n> 密码请只放在环境变量里，不要提交到仓库。',
    respDoc: respDoc('object', [
      { fieldName: 'token', type: 'string', required: true, description: '访问令牌', example: 'eyJhbGciOi…' },
      { fieldName: 'expiresIn', type: 'integer', required: true, description: '有效期（秒）', example: '7200' },
      { fieldName: 'user.id', type: 'integer', required: false, description: '用户 ID', example: '1001' },
      { fieldName: 'user.name', type: 'string', required: false, description: '显示名', example: 'Alice' },
    ], { token: 'eyJhbGciOi…', expiresIn: 7200, user: { id: 1001, name: 'Alice' } }),
    asserts: [{ name: '状态码 200', expr: 'response.status === 200' }, { name: '有令牌', expr: 'response.json.token != ""' }],
  })

  await mkRequest('用户中心/用户管理', '用户列表', 'GET', {
    url: '{{host}}/json/withArray',
    summary: '分页查询用户',
    params: [KV('page', '1', '页码', true), KV('size', '20', '每页条数', true), KV('keyword', '', '可选：按名称模糊匹配', false)],
    headers: [KV('Authorization', 'Bearer {{token}}'), KV('Accept', 'application/json')],
    docs: '## 查询参数\n\n| 参数 | 说明 |\n| --- | --- |\n| page | 从 1 开始 |\n| size | 最大 100 |',
    respDoc: respDoc('object', [
      { fieldName: 'total', type: 'integer', required: true, description: '总条数', example: '128' },
      { fieldName: 'items[].id', type: 'integer', required: true, description: '用户 ID', example: '1001' },
      { fieldName: 'items[].name', type: 'string', required: true, description: '显示名', example: 'Alice' },
      { fieldName: 'items[].email', type: 'string', required: false, description: '邮箱', example: 'alice@example.com' },
    ], { total: 128, items: [{ id: 1001, name: 'Alice', email: 'alice@example.com' }] }),
  })

  await mkRequest('用户中心/用户管理', '用户详情', 'GET', {
    url: '{{host}}/json/nested',
    summary: '按 ID 查询单个用户',
    headers: [KV('Authorization', 'Bearer {{token}}')],
    docs: '路径参数 `id`，不存在返回 404。',
  })

  await mkRequest('用户中心', '健康检查', 'GET', {
    url: '{{host}}/json/flat',
    summary: '存活探针（不需要认证）',
  })

  await mkRequest('订单', '创建订单', 'POST', {
    url: '{{host}}/echo',
    summary: '创建一笔订单',
    headers: [KV('Content-Type', 'application/json'), KV('Authorization', 'Bearer {{token}}')],
    body: {
      type: 'json',
      raw: '{\n  "sku": "A-1001",\n  "count": 2,\n  "note": "工作日送达"\n}',
      form: [],
      fields: [
        { name: 'sku', type: 'string', required: true, example: 'A-1001', description: '商品编码' },
        { name: 'count', type: 'integer', required: true, example: '2', description: '数量' },
        { name: 'note', type: 'string', required: false, example: '工作日送达', description: '备注' },
      ],
    },
  })

  await mkRequest('订单/退款', '申请退款', 'POST', {
    url: '{{host}}/echo',
    summary: '按订单号申请退款',
    auth: { type: 'bearer', token: '{{token}}' },
    body: {
      type: 'form',
      raw: '',
      form: [KV('orderId', '202611060001', '订单号'), KV('reason', '七天无理由', '退款原因')],
    },
    docs: '退款走表单提交（`application/x-www-form-urlencoded`）。',
  })

  // ---- 文档：表格 + 代码块 + 列表 + 引用，覆盖 Markdown 渲染的每一类节点 ----
  const guide = await call('CreateDoc', '快速开始', '')
  await call('SaveDoc', {
    uid: guide.uid,
    name: '快速开始',
    path: guide.path,
    content: [
      '# 快速开始',
      '',
      '这份集合演示了客户端的各项能力：目录分组、请求参数、响应字段表与项目文档。',
      '',
      '## 第一步：选择环境',
      '',
      '顶部「环境」下拉切到 `dev`，`{{host}}` 与 `{{token}}` 会自动替换。',
      '',
      '| 变量 | 说明 |',
      '| --- | --- |',
      '| host | 测试环境地址 |',
      '| token | 访问令牌 |',
      '',
      '## 第二步：发送请求',
      '',
      '```bash',
      'curl -X POST "$HOST/echo/login" \\',
      '  -H "Content-Type: application/json" \\',
      '  -d \'{"username":"alice","password":"s3cret"}\'',
      '```',
      '',
      '- 响应会自动美化，可切「原始」看原文',
      '- 字段表可以从响应一键生成',
      '',
      '> 提示：Ctrl/⌘ + K 打开命令面板。',
    ].join('\n'),
  })

  const auth = await call('CreateDoc', '鉴权说明', '用户中心')
  await call('SaveDoc', {
    uid: auth.uid,
    name: '鉴权说明',
    path: auth.path,
    content: '# 鉴权说明\n\n所有业务接口都要求 `Authorization: Bearer {{token}}`。\n\n令牌过期后返回 401，请重新登录换取。',
  })

  writeFileSync(path.join(OUT, '.gitignore'), '', 'utf8')
  const info = await call('ActiveCollection')
  console.log(JSON.stringify({ dir: OUT, name: info?.name, requests: 6, docs: 2 }, null, 2))
}

demo().catch((e) => {
  console.error('make_demo 失败:', e.message)
  process.exit(1)
})
