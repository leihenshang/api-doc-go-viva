# Linux 下系统输入法打不出中文：原因与证据

> 结论先说：**这不是文本框的 bug，也不是字体/渲染的问题**，而是 Viva 的 Linux 后端只实现了
> IBus 的 D-Bus 协议，而本机用的是 fcitx5——它的 IBus 兼容层不完整，且 IBus 地址文件是空的。

## Viva 的输入法路径

| 位置 | 事实 |
|---|---|
| `internal/platform/linux/ime_linux.go` | 默认（XCB）后端用 `linuxIME`，经 `internal/ibus` 连 IBus 守护进程；`newIME()` 在拿不到 bus 时直接返回 `nil` |
| `internal/ibus/ibus.go` `Address()` | 只认两种来源：`$IBUS_ADDRESS`，或 `~/.config/ibus/bus/<machine-id>-<host>-<display>` 文件里的 `IBUS_ADDRESS=` 行 |
| `internal/platform/linux` | 没有任何 `XOpenIM` / `XCreateIC`：**不支持 XIM**，所以 `XMODIFIERS=@im=fcitx` 对 Viva 无效 |
| `gtk_ime_linux.go`（`-tags vivagtk`） | 另一条路是 GtkIMContext；需要显式用 GTK 后端编译 |

## 本机的实测

| 检查 | 结果 |
|---|---|
| `XMODIFIERS` / `GTK_IM_MODULE` | `@im=fcitx` / `fcitx`（fcitx5 + 搜狗输入法） |
| `~/.config/ibus/bus/27844fa…-unix-0` | 存在，但内容是 `IBUS_ADDRESS=`（**空**）与 `IBUS_DAEMON_PID=` |
| `ibus.Dial()`（默认启动） | `ibus: no daemon address` → `newIME` 返回 nil → 窗口根本没有输入上下文 |
| 启动时打字 | `nihao` 原样进文本框，无候选窗、无 preedit |
| 强制 `IBUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus` | `Dial` / `CreateInputContext` / `FocusIn` 都成功，但每个 `ProcessKeyEvent` **都没有应答**（探针 6 个键全部超时） |
| fcitx5 的 IBus 服务 | 只暴露 `org.freedesktop.IBus.CreateInputContext`，后续上下文协议没人应答 |
| `-tags vivagtk` 构建并运行 | 能开窗，但打 `nihao` 仍是拉丁字母；启动时伴随 GLib `g_value_set_boxed` 断言告警 |
| `xdotool type 中文` | 能插入 → 文本管线、字体、排版都正常 |
| 应用内 Ctrl+C / Ctrl+V | 能复制粘贴 → **粘贴是当前可用的中文输入兜底** |

探针做法：把 Viva 的 `internal/ibus` 与 `internal/dbus` 源码原样复制成独立模块，调
`Dial → NewContext → FocusIn → ProcessKey`，观察 `Commit` / `Preedit` 回调；结论是
「连得上、建得了上下文，但按键没有回音」。

## 可行解（按代价排序）

1. **换输入法框架**：用 `ibus-daemon -drx` 并把桌面输入法切到 IBus（`GTK_IM_MODULE=ibus`），
   Viva 的默认路径即可工作，代码不用改。
2. **等/推上游**：Viva 的 Linux 后端补 XIM，或补全与 fcitx5 的 IBus 交互；GTK 路径也需要修。
3. **本应用兜底**（已实现）：设置面板里如实显示输入法状态（`internal/ime`，与 Viva 同一条
   发现规则），并提示「可从别处复制中文再粘贴」。

> 注意：`internal/ime` 只做**探测与提示**，不尝试自己实现输入法。探测结果为「可发现 IBus 地址」
> 也不保证引擎一定应答（本机强制指到会话总线就是这种情况），所以文案写的是地址来源，不是「可用」。
