# viva 0.6.9 升级与适配记录

2026-11-26 把 `github.com/vivaui/viva` 从 v0.6.8 升到 **v0.6.9**（Go 1.26，系统依赖不变），
skill（`.claude/skills/building-with-viva`、`.agents/skills/building-with-viva`）同步覆盖，
本节记录迁移、已适配的项、以及「viva 有了但这次没换」的自有实现与原因。

## 1. 破坏性变更（已迁移）

| 0.6.8 | 0.6.9 | 本项目改动 |
|---|---|---|
| `ui.ModalWith(c, &open, title, spec, body, footer)` | `ui.Modal(...)`（同名，spec 在 body 前） | 19 处直接改名 |
| `ui.Modal(c, &open, title, body, footer)` | `ui.Modal(c, &open, title, ui.ModalSpec{}, body, footer)` | 9 处补零值 spec（默认尺寸不变） |
| `ui.TableSpec.Headers` / `Sortable` | 每列 `ui.TableColumn{Title, Sortable}` | `catalog.go` 的 `table()` 封装没有调用点，直接删除（它引用的正是被移除的字段） |
| `ui.TextFieldID` 等 | `ui.TextField(..., ui.WithID(id))` | 1 处（`hovertarget_test.go`） |
| `ui.PopoverWith` | `ui.Popover`（spec 在 body 前） | 本项目未使用 |

迁移后 `bash scripts/check.sh` 与 `go test -race ./...` 全绿，无界面行为变化（零值 `ModalSpec`
与旧的 plain 形态等价）。

## 2. 已替换的自有实现（这次做了）

| 自有实现 | 换成 | 位置 | 收益 |
|---|---|---|---|
| 发送按钮「发送中…＋Disable」分支 | `Box.Loading(m.sending)` | `view/panes.go` | 忙碌期不派发按下（不会重复发）、宽度不跳、外观不变，代码少一个分支 |
| 空状态（`components.Empty` 的 canvas 钩子 + 固定高度） | `ui/empty`（页面级 + actions 插槽） | `view/catalog.go` | 跟随主题与密度；「没有匹配的请求」处顺手加了「清空搜索」按钮 |
| 选目录不带初值 | `OpenDirectoryOptions.InitialDirectory` | `view/actions.go` | 打开集合 / 导入集合都从当前集合的上一级开始找 |
| 请求名无长度约束 | `ui.MaxLength(60)` + `ui.ShowCount()` | `view/dialogs.go` | 文件名取请求名，撞上长度上限之前就能看见 |
| 「清空 Cookie / 历史 / 审计」一点就执行 | `ui/popconfirm`（按钮旁问一句） | `view/dialogs.go` | 不可撤销的动作先问一句；三处按钮都从 footer / 行尾挪到行首（原因见下），触发的确认气泡才不越出弹窗 |
| 请求名只有底部一行通用错误 | `ui/FormField` + `ui/validate` | `view/dialogs.go` / `view/actions.go` | 错误画在字段下方、标题与边框一起变红；规则是 `Required` + `MaxLength(60)`，输入变化与提交时各校验一次 |
| 卡片底用 `components.Card` 的 canvas 钩子 + 手写内边距（概览统计卡、文档预览栏） | `ui/card`（页眉 / 页脚 / Small / Flat） | `view/overview.go` / `view/panes.go` | 统计卡的标签成了卡片标题（`Size: card.Small` 紧凑尺寸），文档预览卡加了带分隔线的页脚「共 N 行 · M 字」（行数口径与状态栏一致）；顺带删掉没有调用点的 `codeCard` 与 `cardDraw` |
| 设置「本地数据」四行 `ui.Wrap`、gRPC 连接信息两行文本 | `ui/descriptions`（`Bordered` / `LabelWidth`） | `view/dialogs.go` / `view/panes.go` | 标签列对齐、带边框；值用 `Item.Content` + `ui.Wrap` 放长路径（纯文本 `Value` 会被裁掉），顺手去掉值里重复的「集合：」「环境：」前缀 |
| 「请求失败：长原因」单行红字 | `ui/typography`（`Rows: 3` + 展开 + 复制） | `view/panes.go` | TLS/DNS/超时这类长原因截断到 3 行，可展开、可一键复制；`Spec` 没有危险色，所以警示色留在「请求失败」标题上，按钮文案换成中文 |
| 8 处 `app.SetClipboardText` | `ui.SetClipboardText` | `view/actions.go` / `view/panes.go` | 同签名，入口统一到 `ui`（读剪贴板仍用 `app.ClipboardText`，`ui` 没有对应函数） |

## 3. viva 有了、但这次没换的自有实现（含原因）

| viva 能力 | 本项目现状 | 不换的原因 |
|---|---|---|
| `ui/alert`（状态图标 + 标题 + 说明 + 动作 + 关闭） | 一次性错误文本（危险色 `ui.Text`） | `alert` 需要 model 持有 `alert.New()` 实例（它记住「用户关掉了」）；本项目的提示跟着状态消失，换成 Alert 反而多一份状态 |
| `ui/editabletabs`（按标题宽度、关闭 / 新增、超出滚动） | 自绘标签条（拖拽换位、右键菜单、横向滚动、溢出「⋯」列表） | 换过去会**丢功能**：拖拽排序与右键菜单 editabletabs 没有 |
| `ui.Table`（列标题 + 排序 + 筛选 + 勾选） | 参数 / 请求头 / 字段表是「行内带输入框的可编辑表格」；运行器结果表带行尾「规则」按钮 | `Table` 的 Cell 只能是字符串，放不下输入框与按钮 |
| `ui.AlertDialog`（规范确认框：焦点范围、Escape、危险色动作） | 删除分组等确认用自建 `ui.Modal` + 两个按钮 | **试过又退回了**：AlertDialog 由 components 直接画在 widget 层，无头 `Snapshot` 里既没有文字也没有命中区（实测 `Texts`/`Hits` 都没有「删除分组」），「每个交互都要有无头用例」这条约定守不住。自建 Modal 的按钮是普通 `ui.Button`，标签可点可断言 |
| `ui/anchor`（页内目录）、`ui/comment`、`ui/timeline`、`ui/steps`、`ui/transfer` 等 | 项目里没有对应场景 | 新增能力，不是替换点 |
| `ui.TreeNode.Loading` | 树没有异步加载 | 没有可替换的「加载中」自绘 |
| `MenuItem.Toggled` | 排序菜单用 `Checked` 并在点击后关闭 | 排序菜单点完就该关，保持打开反而不合预期 |

## 3.1 两条实测限制（写代码前先知道）

- **弹窗内的 `ui.Popover`（含 popconfirm）在无头驱动里点不到**：确认气泡画在 Modal 内部时，
  `driver.Click` 落在气泡按钮上不会触发 `Clicked()`（真窗口正常，已用软件 Vulkan 实拍确认：
  气泡在「清空」下方完整展开、点「确认清空」生效）。因此 popconfirm 的交互只能验到
  「点按钮 → 气泡出现」，确认那一下由真窗口保证；两个既有用例里改成直接调用清空方法。
- **气泡宽度 280px，贴着弹窗右边界会越界**：越界那部分点击会打到弹窗的 scrim 上（实测会把
  弹窗一起关掉）。所以三处「清空」都从 footer / 行尾挪到了内容行首，气泡向右有空间。

## 4. 升级后自动获得的修正（不需要改代码）

- Linux 剪贴板：刚获得焦点的所有者会被等待；X11 选区等待被中断时重试；PRIMARY 与 AT-SPI 只复制选中文本。
- X11 文件拖放期间保留重绘；GTK Wayland 在内容指针进入时保留预编辑（输入法）。
- 文本字段二次按下打开系统剪切 / 复制 / 粘贴菜单（Linux 由页面自绘同一份词表，与 `PageConfig.EditMenu` 共用）。
- `textview` 在当前帧绘制请求的滚动行；文本域的自定义绘制保留选中文本。
- Windows 剪贴板 PNG 读取失败时回退 DIB；macOS 非主线程启动立即失败而不是挂起。

## 5. 复查清单（下次升级照做）

1. `go get github.com/vivaui/viva@<版本>` → `go mod tidy`。
2. 按 AGENTS.md 的命令用 `install -m 444` 覆盖两份 skill（只读，别就地改）。
3. 读 `docs/releases/<版本>.md` 的 **Caveats** 与 `docs/upgrading.md`，逐条对 breaking changes。
4. `gofmt` / `bash scripts/check.sh` / `go test -race ./...`，再开真窗口（软件 Vulkan）看观感。
5. 把本文件的第 2、3 节按新版本重审一遍——表里「没换」的项在新版本可能变得值得换。
