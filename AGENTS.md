# AGENTS.md

面向 AI 编码助手的项目约定，重点是**本机（Deepin / X11 / AMD RX 6750 GRE）跑 Viva 验证程序时已经踩过的崩溃**。
动手前先读本文件；改集合树、跑 GUI 验证、用 xdotool 截图时尤其要遵守。

## 项目概览

`api-doc-go-viva` 是 api-doc-go 桌面客户端的 **Viva 原生 GPU 绘制验证版**：
复用 `api-doc-go-share/collection` 的集合文件与发送逻辑，界面用 `github.com/vivaui/viva@v0.6.5` 重写。

- 入口：`main.go`（`-collection A,B` 一次可开多个集合根，`-env` 选环境）
- 无集合启动是正常路径：`view.New(nil, …)` 会造一个空占位集合，首屏走 `welcomePage`
  （「打开集合」）；打开成功后 `adoptCollection` 替换占位根。**不要**把"没有集合"当错误直接退出，
  否则 Windows 双击 exe 只会闪退。
- 界面：`internal/view/`（`model.go` 状态、`tree.go` 集合树、`page.go` 布局、`panes.go` 请求/响应分段、`dialogs.go` 弹窗）
- 读写：`internal/explorer/`（集合扫描、新建/重命名/移动/克隆/回收站）
- 发送：`internal/send/`
- 界面规范与参考图：`ui_reference/`
- 门禁：`scripts/check.sh`（gofmt → build → vet → test）

常用命令（必须带 `GOWORK=off`，避免父目录 `go.work` 干扰）：

```bash
export PATH=$PATH:$HOME/go/bin
GOWORK=off go build ./...
GOWORK=off go test ./...
GOWORK=off go test -race ./...
bash scripts/check.sh

bin/dev start|stop|restart|status   # 后台启动/停止，日志 bin/.cache/dev.log
COLLECTION=../api-doc-go-flutter/sample-collection bin/dev start

# Windows 版（产物在 dist/，默认 windowsgui；调试加 --console / -Console）
bash scripts/build-windows.sh
powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1
```

## 崩溃一：Viva Tree 树键重复（程序 panic，已修复，禁止回退）

### 现象

程序启动或拖拽展开另一个集合根时直接退出，日志开头是：

```
panic: ui: two siblings have the key 768806502 in one paint; key rows by something unique
```

`bin/.cache/dev.log` 里能看到 `ui.(*Page).name` → `ui.Tree` → `view.sidebar` 的调用栈。
触发条件：**两个集合根里有相同的相对路径**（例如两份 sample-collection 都有 `用户/健康检查.yml`），
且两边同时展开；跨集合拖拽时"悬停自动展开目标根"最容易触发。

### 根因

`ui.Tree` 要求同一次绘制内每一行的键唯一。原实现用
`"dir:"+相对目录` / `"entry:"+相对文件` 作键，两个集合完全重复。
另外 `expanded` 展开状态也用了同一套键，同名分组会互相串。

### 已采用的修复（改树相关代码时必须沿用）

- 行键必须带集合根：
  - `dirKey(rootDir, dir)   = "dir:" + rootDir + "|" + dir`
  - `entryKey(rootDir, file) = "entry:" + rootDir + "|" + file`
  - 集合根自身仍是 `"root:" + col.Dir`
- 新代码**禁止**再手写 `"dir:"+dir` / `"entry:"+file`；选中、待选中、展开、
  `remapKey` 都走上面的函数（`remapKey` 只改 `|` 后面的相对路径，根前缀不动）。
- `treeHash` 仍可继续用，但输入必须是带根的键。
- 回归用例：`TestTwoRootsWithSamePathsShareNoTreeKeys`（两个集合同名条目同时展开，断言键唯一）。
  改集合树后至少跑 `GOWORK=off go test ./internal/view/`。

## 崩溃二：Electron GPU 进程崩溃 → CodeBuddy 主进程 SIGTRAP（IDE 退出、远程断开）

### 现象

- IDE 窗口突然消失，重开后提示"上次未正常退出"；Remote-SSH / 集成终端全断。
- 系统没重启、没 OOM：`uptime` 仍是几天，内存充足，`free -h` 正常。
- `coredumpctl list` 有 `buddycn` 的 `SIGTRAP`（约 16–19 MB core）。
- 旧会话 `main.log` 里连续出现：

```
[CrashCollector] child-process-gone (GPU) reason=abnormal-exit exitCode=512
```

约每 15 秒一次；持续约 2 分钟后主进程 abort，core 里能搜到：

```
FATAL:content/browser/gpu/gpu_data_manager_impl_private.cc:415]
GPU process isn't usable. Goodbye.
```

即：GPU 进程反复崩溃 → Chromium 判定 GPU 不可用 → `ImmediateCrash()`（SIGTRAP）。
主进程一死，扩展宿主、集成终端、Remote-SSH 隧道、`bin/dev` 起的验证程序全部被回收，
所以"远程被退出"是**IDE 进程树被连坐**，不是网络或机器故障。

### 本机诱因

- AMD RX 6750 GRE（`radeonsi` / Mesa 24.3.0）+ Electron 37 的 ANGLE OpenGL。
- 验证程序默认走 **Vulkan**（不是 OpenGL，`LIBGL_ALWAYS_SOFTWARE` 对它无效）。
  在 IDE 运行时反复启动/拖拽 GUI 验证程序，会与 IDE 的 GPU 进程争同一块 AMD GPU；
  两次崩溃时验证程序都在运行，第二次在启动后 1–2 分钟内就出现了 GPU 崩溃循环。
- 应用自身的 panic（崩溃一）是另一回事，不会直接杀 IDE，但排查时不要混淆。

### 规定：GUI 验证必须用软件 Vulkan

启动验证程序时固定加：

```bash
VK_ICD_FILENAMES=/usr/share/vulkan/icd.d/lvp_icd.x86_64.json \
COLLECTION=/tmp/a,/tmp/b bin/dev start
```

确认真的走了软件渲染（只应看到 `libvulkan_lvp.so`，不应出现 `libvulkan_radeon.so`）：

```bash
PID=$(cat bin/.cache/dev.pid)
grep -o '/[^ ]*libvulkan_[a-z]*\.so' /proc/$PID/maps | sort -u
```

仍不稳定时，让用户给 CodeBuddy 关硬件加速（`argv.json` 里
`"disable-hardware-acceleration": true`，或启动加 `--disable-gpu`），这是根治 IDE 侧 SIGTRAP 的办法。
不要为了"验证快"而反复用硬件 Vulkan 启停 GUI。

### 崩溃后如何取证（不要先怀疑 OOM）

```bash
coredumpctl list --since "今天 00:00"
ls -lt "$HOME/.config/CodeBuddy CN/logs" | head
grep "child-process-gone (GPU)" "$HOME/.config/CodeBuddy CN/logs/<旧会话>/main.log" | tail
journalctl --since "..." --no-pager | grep -iE "oom|killed process|gpu|drm"
```

只有 `dmesg`/journal 明确出现 OOM 时才按内存问题处理；本机两次都是 GPU 崩溃链。

## GUI 验证与 xdotool 规则

1. **先无头测试，后 GUI**：逻辑改动先过 `go test`，GUI 只做最终观感确认，减少窗口启停次数。
2. **注意 DPI 缩放**：窗口逻辑尺寸 1180×760，本机物理窗口是 1475×950（×1.25）。
   `xdotool` 用物理像素；坐标要么从截图上的实际像素量，要么按 `窗口宽 / 1180` 换算，
   不要直接拿界面逻辑坐标去点。
3. **每次重启都重新取窗口 ID 与位置**，不要复用上一次的 `/tmp/wid` 或坐标：

   ```bash
   WID=$(xdotool search --name "API-DOC · Viva" | while read id; do
     [ "$(xdotool getwindowpid $id)" = "$(cat bin/.cache/dev.pid)" ] && echo $id
   done | head -1)
   eval $(xdotool getwindowgeometry --shell $WID)   # 得到 X / Y
   ```

   窗口位置会变（例如从 `53,78` 变成 `542,240`），坐标必须加 `X`/`Y` 偏移。
4. **按下与松开必须在同一条命令里**：`mousedown` 后若命令/终端被中断，`mouseup` 不会执行，
   会留下"鼠标一直按着"的状态。拖拽脚本要把 `xdotool mouseup 1` 放在同一条命令末尾。
5. 拖拽前先 `import -window $WID` 截图确认行位置；拖拽后先查磁盘（`ls` / `grep seq:`）
   再截图，避免把"没点中"误判成功能失败。
6. 截完图马上 `bin/dev stop`，并清理 `/tmp` 下的临时集合与截图；临时副本只放 `/tmp`，
   不要直接拖仓库里的 `sample-collection`。

## 集合树改动清单（易错点）

- 键：只用 `dirKey` / `entryKey` / `"root:"+dir`，保证一次绘制内唯一。
- 展开状态 `m.expanded` 的键与行键一致，跨集合不能共用。
- 选中：`selectEntryRow` / `selectFolderDir` 会把键写到 `m.selKey` 与 `m.pendingKey`；
  `applySelect` / `applyPendingSelect` 下一帧才能选中（Tree 的行数要等画完一帧）。
- 拖放：`canDrop` 只做判定，`dropRow` 才落盘；跨集合移动重扫两个根，
  源根的标签由 `refreshTabs` 自动关闭。
- 移动 / 克隆 / 删除分组后，路径前缀变化要同步 `tabs[].file`、`m.req.file`、`m.entryFile`、
  `m.selKey`、`m.pendingSelect`（参考 `moveFolder` / `moveEntry` 的 remap）。
- 分组移动遇到目标同名目录时按客户端语义**报错**，不要偷偷改目录名。

## 提交前检查

```bash
gofmt -l internal
bash scripts/check.sh
GOWORK=off go test -race ./...
```

新增交互必须有 `internal/view/view_test.go` 的无头驱动用例；
涉及磁盘格式（seq / uid / 回收站 / 克隆）的，同时在 `internal/explorer/*_test.go` 断言文件内容。
