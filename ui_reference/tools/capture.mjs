#!/usr/bin/env node
// 逐屏截图 + 采集度量，供 Flutter 复刻时对照。
//
// 为什么不用 Wails 窗口而用 Playwright：桌面包里的前端与 devserver 提供的是**同一份构建产物**，
// 浏览器里驱动的稳定性远高于操作原生窗口；只有「无边框窗口/圆角/系统文件框」这类壳层才需要真机截。
//
// 用法（先起 devserver，见 ../README.md）：
//   APIDOC_BASE=http://127.0.0.1:8199 node tools/capture.mjs
import { mkdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(here, '..')
const PW =
  process.env.PLAYWRIGHT_MODULE ||
  path.resolve(here, '../../../api-doc-go-client/frontend/node_modules/playwright/index.mjs')
const { chromium } = await import(PW)

const BASE = process.env.APIDOC_BASE || 'http://127.0.0.1:8199'
const OUT = path.join(ROOT, 'screens')
const MET = path.join(ROOT, 'metrics')
mkdirSync(path.join(OUT, 'light'), { recursive: true })
mkdirSync(path.join(OUT, 'dark'), { recursive: true })
mkdirSync(MET, { recursive: true })

const MEASURE_SELECTORS = [
  '.app',
  '.side',
  '.row.root',
  '.row',
  '.tabs',
  '.tab',
  '.editor-col',
  '.resp-col',
  '.n-modal',
  '.n-card',
  '.n-button',
  '.n-input',
  '.statusbar',
]

const browser = await chromium.launch()
const page = await browser.newPage({
  viewport: { width: 1440, height: 900 },
  deviceScaleFactor: 2,
  locale: 'zh-CN',
})

const failures = []
const shotIndex = []

/** 读取当前页面的设计令牌与关键元素度量。 */
async function measure() {
  return page.evaluate((selectors) => {
    // 令牌：从样式表里扫出所有 --app-* 变量再逐个取值（同源可读 cssRules）
    const names = new Set()
    for (const sheet of Array.from(document.styleSheets)) {
      let rules = []
      try {
        rules = Array.from(sheet.cssRules || [])
      } catch {
        continue
      }
      for (const rule of rules) {
        const text = rule.cssText || ''
        for (const m of text.matchAll(/--app-[a-z0-9-]+/g)) names.add(m[0])
      }
    }
    const rootStyle = getComputedStyle(document.documentElement)
    const tokens = {}
    for (const n of Array.from(names).sort()) tokens[n] = rootStyle.getPropertyValue(n).trim()

    const pick = (el) => {
      if (!el) return null
      const r = el.getBoundingClientRect()
      const s = getComputedStyle(el)
      return {
        rect: { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) },
        fontSize: s.fontSize,
        lineHeight: s.lineHeight,
        fontFamily: s.fontFamily.split(',')[0].replace(/["']/g, ''),
        color: s.color,
        background: s.backgroundColor,
        borderRadius: s.borderRadius,
        padding: s.padding,
        gap: s.gap,
        border: s.borderTopWidth + ' ' + s.borderTopColor,
      }
    }
    const elements = {}
    for (const sel of selectors) {
      elements[sel] = pick(document.querySelector(sel))
    }
    return { theme: document.documentElement.dataset.theme || 'light', viewport: { w: innerWidth, h: innerHeight }, tokens, elements }
  }, MEASURE_SELECTORS)
}

/** 截一张：action 负责把界面切到目标状态。失败不中断，记进 report 里。 */
async function shot(name, action, opts = {}) {
  const theme = opts.theme || 'light'
  try {
    if (action) await action()
    await page.waitForTimeout(opts.wait ?? 400)
    // 主题守卫：截图用的是哪套配色必须和目录名一致，否则整批图会「明暗颠倒」还看不出来。
    const cur = await page.evaluate(() => document.documentElement.dataset.theme || 'light')
    if (cur !== theme) throw new Error(`主题不符：期望 ${theme}，实际 ${cur}`)
    await page.mouse.move(2, 2) // 悬停态复位，避免上一屏的 hover 留在画面里
    await page.screenshot({ path: path.join(OUT, theme, `${name}.png`) })
    writeFileSync(path.join(MET, `${name}.json`), JSON.stringify(await measure(), null, 2))
    shotIndex.push({ name, theme, status: 'ok', note: opts.note ?? '' })
  } catch (e) {
    failures.push({ name, error: String(e.message || e).split('\n')[0] })
    shotIndex.push({ name, theme, status: 'failed', note: String(e.message || e).split('\n')[0] })
  }
}

const tree = page.getByTestId('tree.row')
const row = (text) => tree.filter({ hasText: text }).first()

/** 收起所有弹窗/菜单，回到工作台。 */
async function resetUi() {
  for (let i = 0; i < 3; i++) {
    await page.keyboard.press('Escape')
    await page.waitForTimeout(120)
  }
  await page.mouse.move(2, 2)
}

/** 主题切到指定值（幂等）：上一轮把暗色写进了设置，下一轮启动就是暗的。 */
async function ensureTheme(mode) {
  const cur = await page.evaluate(() => document.documentElement.dataset.theme || 'light')
  if (cur === mode) return
  await page.getByTestId('titlebar.theme').click()
  await page.waitForFunction((m) => (document.documentElement.dataset.theme || 'light') === m, mode)
}

async function expandAll() {
  // 根行先展开：请求/文档都挂在根下面，根收起时树里只有一行
  const roots = page.getByTestId('tree.root')
  for (let i = 0; i < (await roots.count()); i++) {
    const root = roots.nth(i)
    if ((await root.getAttribute('data-expanded')) === 'true') continue
    await root.getByTestId('tree.root.caret').click()
  }
  for (let pass = 0; pass < 12; pass++) {
    const folded = page.locator('[data-testid="tree.row"][data-kind="folder"][data-collapsed="true"]')
    if ((await folded.count()) === 0) return
    await folded.first().getByTestId('tree.row.caret').click()
  }
}

/** 回到集合概览：概览上的按钮只在「集合」页签存在（打开过请求后就切走了）。 */
async function goOverview() {
  await resetUi()
  await page.getByRole('button', { name: '集合', exact: true }).first().click()
  await page.getByTestId('overview.newRequest').waitFor({ state: 'visible' })
  await page.waitForTimeout(250)
}

async function openReq(name) {
  await row(name).getByTestId('tree.row.name').click()
  await page.getByTestId('tab.item').filter({ hasText: name }).waitFor({ state: 'visible' })
  await page.getByTestId('req.url').waitFor({ state: 'visible' })
  await page.waitForTimeout(250)
}

async function seg(name) {
  await page.getByRole('button', { name, exact: true }).first().click()
  await page.waitForTimeout(250)
}

try {
  await page.goto(BASE, { waitUntil: 'networkidle' })
  await page.getByTestId('tree.root').first().waitFor({ state: 'visible' })
  await ensureTheme('light') // 起点固定为浅色：否则上一轮的暗色会让整批图「明暗颠倒」

  // ---- 集合树 ----
  await shot('01-overview', async () => {
    await expandAll()
  }, { note: '集合概览：目录/统计/快捷操作（真实路径只在这一页展示）' })

  await shot('02-tree-expanded', async () => {
    await page.mouse.move(2, 2)
  }, { note: '集合树展开态：分组缩进、请求方法标记、文档条目' })

  await shot('03-tree-hover', async () => {
    await row('用户列表').hover()
  }, { note: '树行 hover：行底色、右侧操作按钮' })

  await shot('04-tree-context-menu', async () => {
    await page.getByTestId('tree.root').first().click({ button: 'right' })
    await page.waitForTimeout(250)
  }, { note: '根行右键菜单：新建/重命名/在文件管理器中打开' })

  await resetUi()

  // ---- 请求页各分段 ----
  await shot('05-request-params', async () => {
    await openReq('用户列表')
    await seg('请求参数')
  }, { note: '请求页：方法/URL 栏 + 参数表（名称/值/说明 + 启用位）' })

  await shot('06-request-body', async () => {
    await openReq('登录')
    await seg('请求体')
  }, { note: '请求体：JSON 编辑器 + 字段表（5 列）' })

  await shot('07-request-headers', async () => {
    await seg('请求头部')
  }, { note: '请求头：键值行 + 启用位 + 说明列' })

  await shot('08-request-auth', async () => {
    await openReq('申请退款')
    await seg('认证')
  }, { note: '认证：类型下拉 + 令牌输入' })

  await shot('09-request-respdoc', async () => {
    await openReq('用户列表')
    await seg('响应字段')
  }, { note: '响应字段表：字段名/类型/必填/说明/示例' })

  await shot('10-request-docs', async () => {
    await seg('文档')
  }, { note: '接口级文档（Markdown 编辑器）' })

  // ---- 响应区 ----
  await shot('11-response-sent', async () => {
    await openReq('用户列表')
    await seg('请求参数') // 上一屏停在「文档」分段，这里切回来才能同时看到参数与响应
    await page.getByTestId('req.send').click()
    await page.getByTestId('resp.status').waitFor({ state: 'visible', timeout: 15000 })
    await page.waitForTimeout(500)
  }, { note: '响应区：状态码/耗时/大小 + 美化后的 JSON 树' })

  await shot('12-response-headers', async () => {
    await page.locator('[data-testid="resp.tab"][data-seg="headers"]').click()
    await page.waitForTimeout(250)
  }, { note: '响应头表格' })

  await shot('13-tabs', async () => {
    await openReq('登录')
    await openReq('创建订单')
    await openReq('快速开始')
    await page.waitForTimeout(300)
  }, { note: '多页签：图标/名称/未保存圆点/关闭按钮' })

  // ---- 命令面板 ----
  await shot('14-command-palette', async () => {
    await page.keyboard.press('Control+k')
    await page.waitForTimeout(350)
  }, { note: '命令面板：请求搜索 + 环境 + 命令' })

  await resetUi()

  // ---- 设置 ----
  await shot('15-settings-appearance', async () => {
    await page.getByTestId('statusbar.settings').click()
    await page.getByTestId('settings.theme').waitFor()
  }, { note: '设置·外观：主题/缩放/响应区位置/自动保存/关闭行为' })

  await shot('16-settings-network', async () => {
    await page.getByTestId('settings.nav.network').click()
    await page.waitForTimeout(250)
  }, { note: '设置·网络与安全：代理/证书/超时/重定向' })

  await shot('17-settings-local', async () => {
    await page.getByTestId('settings.nav.local').click()
    await page.waitForTimeout(250)
  }, { note: '设置·本地数据：工作目录/恢复上限/缓存' })

  await shot('18-settings-mcp', async () => {
    await page.keyboard.press('Escape')
    await page.getByTestId('statusbar.mcp').click()
    await page.getByTestId('settings.pane.mcp').waitFor()
    await page.waitForTimeout(300)
  }, { note: '设置·MCP：开关/端口/令牌/工具组/可访问工作区（只显示名称）' })

  await resetUi()

  // ---- 弹窗家族 ----
  await shot('19-dialog-import', async () => {
    await goOverview()
    await page.getByTestId('overview.import').click()
    await page.waitForTimeout(350)
  }, { note: '导入集合：格式选择 + 文本/文件' })

  await resetUi()
  await shot('20-dialog-export-scope', async () => {
    await goOverview()
    await page.getByTestId('overview.exportMd').click()
    await page.getByTestId('export.dialog').waitFor()
    await page.getByTestId('export.modePick').click()
    await page.waitForTimeout(250)
  }, { note: '导出范围：全部 / 仅所选（目录/请求/文档复选树）' })

  await resetUi()
  await shot('21-dialog-history', async () => {
    await page.getByTestId('statusbar.history').click()
    await page.waitForTimeout(400)
  }, { note: '请求历史：按时间/状态/耗时' })

  await resetUi()
  await shot('22-dialog-trash', async () => {
    await page.getByTestId('statusbar.trash').click()
    await page.waitForTimeout(400)
  }, { note: '回收站：删除条目与恢复' })

  await resetUi()
  await shot('23-dialog-cookies', async () => {
    await page.getByTestId('statusbar.cookies').click()
    await page.waitForTimeout(400)
  }, { note: 'Cookie 管理' })

  await resetUi()
  await shot('24-dialog-env', async () => {
    await page.getByTestId('env.select').click()
    await page.locator('.n-dropdown-option').filter({ hasText: '环境设置' }).first().click()
    await page.waitForTimeout(350)
  }, { note: '环境管理：环境列表 + 变量表（敏感值打码）' })

  await resetUi()
  await shot('25-dialog-run', async () => {
    await goOverview()
    await page.getByTestId('overview.run').click()
    await page.waitForTimeout(350)
  }, { note: '运行集合：环境/过滤/失败即停/超时' })

  await resetUi()
  await shot('26-dialog-mock', async () => {
    await goOverview()
    await page.getByTestId('overview.mock').click()
    await page.waitForTimeout(350)
  }, { note: '本地 Mock：端口 + 启停' })

  await resetUi()

  // ---- 文档 ----
  await shot('27-doc-preview', async () => {
    await row('快速开始').getByTestId('tree.row.name').click()
    await page.getByTestId('doc.toEdit').waitFor({ timeout: 10000 })
    await page.waitForTimeout(400)
  }, { note: '文档页：只读预览（标题层级/表格/代码块/引用）' })

  await shot('28-doc-edit', async () => {
    await page.getByTestId('doc.toEdit').click()
    await page.waitForTimeout(700)
  }, { note: '文档页：编辑态（左源码右实时预览）' })

  // ---- 暗色：只覆盖有差异的代表屏 ----
  await shot('29-dark-overview', async () => {
    await goOverview() // 上一屏停在文档编辑页，先回到「集合」页签
    await ensureTheme('dark')
  }, { theme: 'dark', note: '暗色·概览' })

  await shot('30-dark-request', async () => {
    await openReq('用户列表')
    await seg('请求参数')
  }, { theme: 'dark', note: '暗色·请求页' })

  await shot('31-dark-response', async () => {
    await page.getByTestId('req.send').click()
    await page.getByTestId('resp.status').waitFor({ state: 'visible', timeout: 15000 })
    await page.waitForTimeout(400)
  }, { theme: 'dark', note: '暗色·响应区' })

  await shot('32-dark-settings-mcp', async () => {
    await page.getByTestId('statusbar.mcp').click()
    await page.getByTestId('settings.pane.mcp').waitFor()
    await page.waitForTimeout(300)
  }, { theme: 'dark', note: '暗色·MCP 设置' })

  await shot('33-dark-tree', async () => {
    await goOverview()
    await expandAll()
  }, { theme: 'dark', note: '暗色·集合树' })
} finally {
  writeFileSync(
    path.join(ROOT, 'metrics', '_capture-report.json'),
    JSON.stringify({ base: BASE, shots: shotIndex, failures }, null, 2),
  )
  await browser.close()
  console.log(JSON.stringify({ ok: shotIndex.filter((s) => s.status === 'ok').length, failed: failures }, null, 2))
  if (failures.length) process.exitCode = 2
}
