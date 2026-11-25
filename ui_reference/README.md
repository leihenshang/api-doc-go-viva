# Wails 版 UI 参考（截图 + 度量）

> 本目录是 `api-doc-go-flutter/ui_reference` 的原样副本，随 `api-doc-go-viva` 一起提交，
> 作为 Viva 版界面复刻的对照物。下文里「Flutter 侧」的说明保留原意，Viva 版只需要
> **截图（`screens/`）与运行时度量（`metrics/`）**；`demo-collection/` 与 `tools/` 是原仓库
> 生成这些素材用的，不参与 Viva 构建。
>
> Viva 版的现状盘点与批次见 [`../doc/执行计划.md`](../doc/执行计划.md)，
> 功能与界面清单见 [`../doc/功能与界面.md`](../doc/功能与界面.md)。

复刻界面时的**对照物**：Wails 客户端各屏的真实截图，以及每屏关键元素的运行时度量（尺寸/字号/行高/圆角/颜色）。

范围：只做桌面（Linux / Windows / macOS），目标是**观感一致**，不做逐像素 diff。

## 目录

```
ui_reference/
├── README.md                # 本文件：索引 + 令牌 + 重新生成截图
├── screens/
│   ├── light/   01..28.png  # 浅色：28 屏
│   └── dark/    29..33.png  # 暗色：5 屏（只覆盖有差异的代表屏）
├── metrics/
│   ├── <屏名>.json          # 每屏：设计令牌 + 关键元素度量
│   ├── _capture-report.json # 采集清单与失败记录（失败会精确到屏）
│   └── ...
├── demo-collection/         # 截图用的示例集合（文本，可重建）
└── tools/
    ├── make_demo.mjs        # 造演示数据（走 App 门面，落盘格式与真实使用一致）
    └── capture.mjs          # 逐屏截图 + 采集度量
```

## 怎么用

1. 先看 `screens/light/01-overview.png` 建立整体印象，再按下面的索引翻具体屏。
2. 写 Flutter 组件时，**数值以 `metrics/*.json` 为准**，不要靠肉眼估间距。
3. 每屏的 JSON 结构：

```jsonc
{
  "theme": "light",
  "viewport": { "w": 1440, "h": 900 },
  "tokens": { "--app-bg": "#f5f5f5", "--app-panel": "#ffffff", ... },   // CSS 设计令牌
  "elements": {
    ".side": {
      "rect": { "x": 1, "y": 45, "w": 260, "h": 826 },
      "fontSize": "13px", "lineHeight": "20.8px", "fontFamily": "Noto Sans SC",
      "color": "rgb(32,33,36)", "background": "rgb(245,245,245)",
      "borderRadius": "0px", "padding": "0px", "gap": "normal"
    }
  }
}
```

> 截图是固定 1440×900、deviceScaleFactor=2（图片实际 2880×1800），素材可直接放大看细节。

## 关键令牌（浅色 / 暗色）

| 令牌 | 浅色 | 暗色 | 用途 |
|---|---|---|---|
| `--app-bg` | `#f5f5f5` | `#17181a` | 窗口/侧栏底色 |
| `--app-panel` | `#ffffff` | `#1e1f22` | 面板、卡片、编辑器 |
| `--app-border` | `#e3e3e3` | `#2b2d31` | 1px 分割线/描边 |
| `--app-text` | `#202124` | `#e6e7e9` | 主文字 |
| `--app-muted` | `#5f6368` | `#9aa0a6` | 次要文字/说明 |
| `--app-accent` | `#18a058` | `#36ad6a` | 品牌绿（装饰） |
| `--app-accent-dark` | `#0e7a3e` | `#7ed6a5` | 可交互主色（按钮/选中） |
| `--app-active` | `#e8f5ee` | `#1e2f26` | 选中行底色 |

**排版与尺度**（实测，来自 `.app` / `.side` / `.row` / `.tab`）：

| 项 | 值 |
|---|---|
| 基准字号 / 行高 | 13px / 20.8px（≈1.6 倍） |
| 页签字号 | 12.5px，行高 16px，高 37px，padding `8px 14px 8px 10px` |
| 表格/按钮字号 | 12px |
| 侧栏宽 | 260px（含 1px 右边框） |
| 树行高 | 30px，padding `0 6px 0 8px` |
| 输入框高 | 28px，圆角 6px |
| 卡片圆角 | 10px（`Card`/弹窗），控件 6px、小控件 4px |
| 请求/响应分栏 | 左 652 / 右 518（≈56% / 44%，可拖动） |
| 标题栏高 | 45px |

## 屏索引

### 浅色（`screens/light/`）

| 文件 | 内容 | 复刻要点 |
|---|---|---|
| `01-overview.png` | 集合概览 | 4 个统计卡（目录/请求/环境/存储格式）+ 三组操作按钮；**真实路径只在这一页出现** |
| `02-tree-expanded.png` | 集合树展开 | 根行 30px；分组缩进；请求前置方法文字（GET/POST 着色）；文档前置 MD 图标 |
| `03-tree-hover.png` | 树行 hover | 悬停整行浅底 + 右侧「···」操作入口 |
| `04-tree-context-menu.png` | 根行右键 | 菜单圆角 8px、轻投影；项高 30px |
| `05-request-params.png` | 请求页·参数 | 方法下拉 + URL 输入 + 发送按钮；参数表四列（勾选/名称/值/说明）+ 删除列 |
| `06-request-body.png` | 请求体 | 左 JSON 编辑器（等宽字体）+ 右字段表 5 列 |
| `07-request-headers.png` | 请求头 | 同参数表；说明列可空 |
| `08-request-auth.png` | 认证 | 类型下拉 + 令牌输入（Bearer） |
| `09-request-respdoc.png` | 响应字段 | 字段名/类型/必填/说明/示例，必填行有强调 |
| `10-request-docs.png` | 接口文档 | Markdown 编辑器（工具栏 + 预览） |
| `11-response-sent.png` | 响应成功 | 状态码 200 着色、耗时/大小/HTTP 版本；JSON 树缩进 + 语法色 |
| `12-response-headers.png` | 响应头 | 两列表格 |
| `13-tabs.png` | 多页签 | 集合页签固定在最左；页签最小宽、悬停关闭按钮 |
| `14-command-palette.png` | 命令面板 | 居中浮层、分组（请求/环境/命令）、快捷键右对齐 |
| `15-settings-appearance.png` | 设置·外观 | 左侧分区导航（160px）+ 右侧表单；`label` 90px 右对齐 |
| `16-settings-network.png` | 设置·网络与安全 | 开关行 + 数字输入 |
| `17-settings-local.png` | 设置·本地数据 | 同上 |
| `18-settings-mcp.png` | 设置·MCP | 开关/端口/工具组复选/可访问工作区（只显示名称）/来源白名单 |
| `19-dialog-import.png` | 导入 | 格式单选 + 文本域 |
| `20-dialog-export-scope.png` | 导出范围 | 两个模式按钮 + 复选树 + 底部计数 |
| `21-dialog-history.png` | 历史 | 列表行高 32px，状态/耗时右对齐 |
| `22-dialog-trash.png` | 回收站 | 批量勾选 + 恢复/彻底删除 |
| `23-dialog-cookies.png` | Cookie | 域分组 + 键值表 |
| `24-dialog-env.png` | 环境管理 | 左环境列表 + 右变量表；敏感值打码 |
| `25-dialog-run.png` | 运行集合 | 表单 + 进度列表 |
| `26-dialog-mock.png` | 本地 Mock | 端口输入 + 启停按钮 |
| `27-doc-preview.png` | 文档预览 | 标题层级/表格/代码块（深底）/引用/列表 |
| `28-doc-edit.png` | 文档编辑 | 左源码右预览，工具栏 28px 高 |

### 暗色（`screens/dark/`）

`29-dark-overview` · `30-dark-request` · `31-dark-response` · `32-dark-settings-mcp` · `33-dark-tree`

暗色只需按令牌换色，结构完全相同；注意代码块在浅色下也是深底（不要改成浅底）。

## 重新生成

```bash
# 0) 目录重置（截图要可重复，旧数据会混进树里）
cd api-doc-go-flutter/ui_reference && rm -rf demo-collection screens metrics && mkdir demo-collection

# 1) 构建前端与两个本地服务
cd ../../api-doc-go/api-doc-go-client
(cd frontend && npm run build)
go build -o /tmp/apidoc-devserver ./cmd/devserver
go build -o /tmp/apidoc-fixtures ./cmd/testfixtures

# 2) 起服务：夹具(:8188) + devserver(:8199)
/tmp/apidoc-fixtures &
# 独立配置目录：否则会读到你自己机器的主题/设置，截图明暗就不可控了
XDG_CONFIG_HOME=/tmp/apidoc-uiref-config \
  /tmp/apidoc-devserver -dir <repo>/api-doc-go-flutter/ui_reference/demo-collection \
  -addr 127.0.0.1:8199 -web <repo>/api-doc-go/api-doc-go-client/frontend/dist &

# 3) 造数据 + 截图
cd <repo>/api-doc-go-flutter/ui_reference
APIDOC_BASE=http://127.0.0.1:8199 APIDOC_FIXTURES=http://127.0.0.1:8188 node tools/make_demo.mjs
APIDOC_BASE=http://127.0.0.1:8199 node tools/capture.mjs
```

`capture.mjs` 的约束（写死就是为了防止出现「明暗颠倒/内容为空」的废图）：

- 起点必须是浅色，暗色屏由脚本自己切；
- 每屏截图后校验 `data-theme` 与目录一致，不一致直接记失败；
- 任何一屏失败不会中断整批，最后汇总在 `metrics/_capture-report.json`，退出码非 0。

## 注意

- **不要只照截图写死数值**：截图给观感与状态，`metrics/*.json` 给数值。
- 平台差异（窗口边框、圆角、字体渲染）不追求一致；Flutter 侧保持同一套间距/字号/层级即可。
- `demo-collection/` 是数据不是代码，改动它会改变后续截图；需要新场景就在 `make_demo.mjs` 里加，别手改文件。

---

