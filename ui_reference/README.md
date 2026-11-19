# Wails 版 UI 参考（截图 + 度量）

> 本目录是 `api-doc-go-flutter/ui_reference` 的原样副本，随 `api-doc-go-viva` 一起提交，
> 作为 Viva 版界面复刻的对照物。下文里「Flutter 侧」的说明保留原意，Viva 版只需要
> **截图（`screens/`）与运行时度量（`metrics/`）**；`demo-collection/` 与 `tools/` 是原仓库
> 生成这些素材用的，不参与 Viva 构建。
>
> Viva 版的现状盘点、差距与执行批次见 [`执行计划.md`](执行计划.md)，主 README 的
> 「UI 参考」一节说明了尺寸基线与组件映射。

复刻 Flutter 界面时的**对照物**：Wails 客户端各屏的真实截图，以及每屏关键元素的运行时度量（尺寸/字号/行高/圆角/颜色）。

范围：只做桌面（Linux / Windows / macOS），目标是**观感一致**，不做逐像素 diff。

> 要把这件事交给别的 AI 执行，直接让它读本文末尾的 **《实施建议（执行 AI 手册）》**（含硬约束、文件映射、批次与任务模板）。

## 目录

```
ui_reference/
├── README.md                # 本文件：索引 + 令牌 + Flutter 对照建议
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

## Flutter 对照建议

| Wails / naive-ui | Flutter 建议 |
|---|---|
| `n-tree`（集合树） | 自绘：`ListView.builder` + 行内缩进，不要用 `ExpansionTile`（高度/缩进/拖拽都不对） |
| `n-tabs` | 自绘页签条（高 37、字号 12.5），`TabBar` 高度与内边距都要覆盖 |
| `n-data-table` | 自绘表格行（行高 28–32、表头 12px muted）或 `DataTable` + 缩紧 |
| `n-input` | `TextField` + `isDense: true`、`contentPadding` 对齐 28px 高、`OutlineInputBorder(radius: 6)` |
| `n-button` | `FilledButton.tonal`/`OutlinedButton` 统一样式：高 28–30、圆角 6、字号 12 |
| `n-modal` | `showDialog` + `Dialog(shape: RoundedRectangleBorder(10))`，遮罩色对齐 `--app-mask` |
| `n-tooltip` / `n-popover` | `Tooltip` / `showMenu`（圆角 8 + 轻投影） |
| `md-editor-v3` | `flutter_code_editor` 或 `re_editor` + `flutter_markdown` 预览；能力对不齐时以「可编辑 + 可预览」为底线 |
| 字体 | 打包 Noto Sans SC；显式 `TextStyle(fontSize: 13, height: 1.6)`，否则密度会差一档 |

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

# 实施建议（执行 AI 手册）

> 这份文件的读者是**接手改造的 AI**，不是人。目标：按截图把 Flutter 端补齐/校准，且不推翻已有工程约定。

## 0. 先读这 5 处，不要从零开始

| 文件 | 为什么必须先读 |
|---|---|
| `../AGENTS.md` | 工程约定、门禁命令、什么不许提交 |
| `../doc/约定与门禁.md` | 测试怎么写、依赖/体积规则（踩坑点都在这） |
| `../doc/Wails界面复刻清单.md` | **当前进度表**：✅ 已做（别重做）/ 🟡 缺元素 / ⬜ 未做 / ⛔ 范围外 |
| `../lib/theme/palette.dart` | 颜色令牌唯一来源（浅/暗成对） |
| `../lib/theme/dims.dart` | 尺寸基准与缩放（比截图更重要，见 §1） |

结论先行：**本工程不是空壳**，`lib/ui/` 下已有 20+ 页面/对话框、`lib/ui/widgets/` 有 14 个可复用组件。本目录是「新的对照物」，用于修观感、补缺元素，**不是重写 UI 的起点**。

## 1. 三条硬约束（违反会返工）

**① 范围：桌面三平台；同步 / MCP / 服务端绑定是项目明示的「范围外」。**
所以 `screens/light/18-settings-mcp.png`、`screens/dark/32-dark-settings-mcp.png` 只作**参考**，不要实现 MCP 分区；遇到清单里标 ⛔ 的能力，一律登记而不是实现。

**② 数值口径：截图是 Wails 基准，Flutter 侧已整体放大一档，别照抄。**

| 项 | Wails（截图/metrics） | Flutter（`Dims`/`palette` 现值） |
|---|---|---|
| 基准字号 | 13px | 14 |
| 侧栏宽 | 260 | 320 |
| 标题栏高 | 45 | 52 |
| 状态栏高 | ~28 | 34 |
| 树行 / 列表行高 | 30 | 34 |
| 输入框 / 按钮高 | 28 | 32 |
| 圆角 | 6（卡片 10） | 6（窗口 10） |

**铁律**：尺寸写 `Dims.of(context)`（`px()/rowH/controlH/pad/...`），颜色写 `AppPalette`（`paletteOf(context)`）。
禁止：`SizedBox(height: 30)`、`Color(0xFFE3E3E3)` 这类字面量；也禁止为了对齐截图去改 `Dims` 的基准值。
确实需要调整某档尺寸时：**只改 `dims.dart` 一处**，然后浅/暗 + 三个主要屏回归，并在清单里注明。

**③ 不做空壳。** 功能没实现就不加按钮（概览页已有此约定）。截图上有、本端没有的入口，要么实现内核，要么删掉入口，不允许「点了没反应」。

## 2. 单屏工作流（每屏都走这 6 步）

1. **看材料**：打开 `screens/{light|dark}/<屏>.png` 与 `metrics/<屏>.json`。只从截图取「有没有 / 在哪 / 什么状态」，从 metrics 取「行高、间距、字号、颜色」，从 `Dims` 取「最终数值」。
2. **定位实现**：按 §3 的映射表打开对应 `.dart`，先读现有代码再改。
3. **分类差异**：缺失元素 / 顺序不对 / 间距不对 / 颜色不对 / 状态缺失（hover、空态、禁用、超长）。一次只处理一类，避免大改。
4. **改**：优先复用 `widgets/`（`KVTable` `FieldsTable` `MethodBadge` `StatusBadge` `GhostButton` `EmptyHint` `KbdKey` `SyncedTextField` `CompactDropdown` `CompactCheck` `SectionTitle` `JsonTree` `MarkdownView`）；新增组件放对应目录，不新建平行体系。
5. **门禁**：`bash scripts/check.sh`（format → analyze → test → build linux → xcheck）。**全绿才算改完**；`sample-collection/` 必须零改动。
6. **收尾**：更新 `doc/Wails界面复刻清单.md` 的状态与「依据 / 缺口」列；本目录的截图和 metrics 不要改（它们是基准）。

## 3. 截图 → Flutter 文件映射

| 截图 | 主要落点 |
|---|---|
| `01-overview` | `lib/ui/overview_page.dart` |
| `02-tree-expanded` `03-tree-hover` `04-tree-context-menu` `33-dark-tree` | `lib/ui/side_bar.dart`（树行、右键菜单） |
| `05-request-params` `06-request-body` `07-request-headers` `08-request-auth` `09-request-respdoc` `10-request-docs` | `lib/ui/request_page.dart` + `lib/ui/request_segments.dart` + `lib/ui/widgets/tables.dart` |
| `11-response-sent` `12-response-headers` `31-dark-response` | `lib/ui/response_panel.dart` + `lib/ui/widgets/json_tree.dart` |
| `13-tabs` | `lib/ui/tab_bar.dart` |
| `14-command-palette` | `lib/ui/dialogs/command_palette.dart` |
| `15..17-settings-*` | `lib/ui/dialogs/settings_dialog.dart` |
| `18-settings-mcp` `32-dark-settings-mcp` | ⛔ 范围外，仅参考 |
| `19-dialog-import` | `lib/ui/dialogs/curl_import.dart` / `openapi_import_dialog.dart` |
| `20-dialog-export-scope` | **本端没有**：新增导出范围选择（可参考 Wails 的复选框树 + 计数） |
| `21-dialog-history` `22-dialog-trash` `23-dialog-cookies` `24-dialog-env` | `lib/ui/dialogs/` 对应文件 |
| `25-dialog-run` `26-dialog-mock` | ⬜ 内核未实现（清单 B17/B18），按 §1③ 处理 |
| `27-doc-preview` `28-doc-edit` | `lib/ui/doc_page.dart` + `lib/ui/widgets/markdown_view.dart` |
| `29-dark-overview` `30-dark-request` | 换色验证：`palette.dart`（不是新页面） |

## 4. 建议批次（按投入产出比）

**P0 · 观感校准（只改样式，不动逻辑）**
- 树行 / 页签 / 表格 / 输入框的**密度**（行高、内边距、字号档位）与截图并排比对；
- 暗色令牌核对：底色、分隔线、选中态、代码块；**开关滑块**在暗色下用 `#C9CED8`（Wails 已修纯白问题）；
- 页签高度/最小宽、集合页签固定在左。

**P1 · 补缺元素（清单标 🟡 的）**
- 请求页缺的「脚本 / 断言 / 变量」分段（gRPC 同）；
- 请求体字段表 5 列是否与 `06-request-body` 一致；
- 响应区：`原始/美化` 切换、查找、示例下拉、冲突条状态。

**P2 · 新界面**
- `20-dialog-export-scope`：导出前选范围（全部 / 按目录·请求·文档勾选，底部计数）。Wails 侧数据模型：`all + groups[] + requests[] + docs[]`，选中目录 = 整棵子树，只选叶子时保留祖先目录。
- `19-dialog-import` 的 Postman / Bruno 导入（若做，先补内核再放入口）。

**P3 · 清理**
- 概览页 6 个空壳按钮：按约定要么补内核、要么移除，并在清单登记。

## 5. 每屏 Definition of Done

- [ ] 截图里的元素齐全（或已登记 ⛔ 范围外 + 原因）
- [ ] 所有尺寸来自 `Dims`，所有颜色来自 `AppPalette`，无字面量
- [ ] 浅色 + 暗色都看过（暗色不是反色，逐项对 `31/32/33`）
- [ ] 字号档位一致：正文 / 次要 / 表格 三档不混用
- [ ] 空态、超长文本、滚动条、禁用态不破版
- [ ] `bash scripts/check.sh` 全绿；`sample-collection/` 零改动
- [ ] `doc/Wails界面复刻清单.md` 状态与依据已更新

## 6. 一句话口诀

**截图定「有没有、在哪、什么状态」；metrics 定「多高多宽什么色」；`Dims`/`AppPalette` 定「最终写多少」。**

## 7. 可直接粘贴给执行 AI 的任务模板

```text
仓库：api-doc-go-flutter（Flutter 桌面端）。
参考物：ui_reference/（Wails 版截图 + metrics 度量 + 本 README）。
先读：AGENTS.md、doc/约定与门禁.md、doc/Wails界面复刻清单.md、
      lib/theme/{palette,dims}.dart、lib/ui/widgets/ 下的现有组件。

任务：把 <屏名，例如 05-request-params> 的观感对齐参考截图。

约束（必须遵守）：
1) 只做桌面端；同步 / MCP / 服务端绑定是范围外，遇到标 ⛔ 的一律不实现。
2) 数值口径：截图是 Wails 基准，Flutter 已整体放大一档。
   尺寸一律用 Dims.of(context)，颜色一律用 AppPalette，禁止字面量；
   不要为了对齐截图改 dims.dart 的基准值。
3) 先读现有实现再改，优先复用 lib/ui/widgets/ 里的组件；不做空壳入口。
4) 改完必须 `bash scripts/check.sh` 全绿，且 `sample-collection/` 零改动。

产出：
- 改动文件清单 + 每个文件改了什么（3 行以内）；
- 与截图的差异说明：还差哪些元素、为什么（范围外 / 内核未实现 / 未做）；
- 更新 doc/Wails界面复刻清单.md 的对应行（状态 + 依据）。
```

## 8. 本机限制（执行者注意）

- 当前开发机 `PATH` 里**没有 `flutter`**（`command -v flutter` 为空），只能做静态检查与代码评审；视觉验证需要在装了 Flutter SDK 的环境跑 `flutter run -d linux`。
- 采图用的两个本地服务（`devserver` / `testfixtures`）与脚本都在 `tools/`，截图后可停掉；不要把它们提交进 Flutter 仓库。
