# AGENTS.md

面向 AI 编码助手的项目约定。**改界面前先读 skill**：`.claude/skills/building-with-viva/SKILL.md`
（Viva 官方约定，`.agents/skills/` 有一份相同的只读拷贝），再回来看本文件的硬规则。

## 项目概览

`api-doc-go-viva` 是 api-doc-go 桌面客户端的 **Viva 原生 GPU 绘制验证版**：复用
`api-doc-go-share/collection` 的集合文件与发送逻辑，界面用 `github.com/vivaui/viva@v0.6.8` 重写。

- 入口：`main.go`（`-collection A,B` 一次可开多个集合根，`-env` 选环境）
- 界面：`internal/view/`（`model` 状态 / `tree` 集合树 / `page` 布局 / `panes` 分段 /
  `dialogs` 弹窗 / `varhover` 变量悬停 / `docode` 文档源码区）
- 读写：`internal/explorer/`；发送：`internal/send/`；界面基线：`ui_reference/`
- 无集合启动是正常路径：`view.New(nil, …)` 造占位集合走 `welcomePage`，**不要**把它当错误退出，
  否则 Windows 双击 exe 只会闪退
- 文档索引见 `README.md` 的「文档」表

## 命令

```bash
export PATH=$PATH:$HOME/go/bin
GOWORK=off go build ./...        # 父目录 go.work 不含本模块，必须关掉
GOWORK=off go test ./...
GOWORK=off go test -race ./...
bash scripts/check.sh            # 门禁：gofmt → build → vet → test

bin/dev start|status|log|stop|restart    # 后台起窗口，日志 bin/.cache/dev.log
COLLECTION=../api-doc-go-flutter/sample-collection ENV_NAME=dev bin/dev start
```

## 用 Viva 时

1. **先读 skill**：`.claude/skills/building-with-viva/SKILL.md` + `recipes.md`。它规定绘制路径禁反射与属性包、
   空闲不画、声明期不分配、命中标识交给 `gfx.Tree`、后台工作走 `ui.Effect`、不给未测量就宣称「零分配」。
2. **先 `go doc` 再猜 API**：`go doc github.com/vivaui/viva/ui` 列出全部元素，最近的例子在模块的
   `examples/guides` 与 `internal/componentdemo`。`go doc` 里没有的符号就是不存在，不要发明再包一层绕过去。
3. **优先无头验证**：`gofmt` / `go vet` → `app.Driver` 用例（`Click` / `Move` / `Type` / `Snapshot`）→
   无头 `layout` / `tree` / `expect` → 最后才开真窗口做观感确认。
   装了 `viva` 命令的中间再加 `viva vet`（本机未安装；`go run` 会被 `tools/patchgen` 缺的 go.sum 条目挡住）。
4. **如实说明验到哪一步**：单测 / 无头布局 / 真窗口 / 输入法是四种不同的结论，不要混着说。

skill 是上游文件的原样拷贝（权限 0444，别就地改）。升级 Viva 后按版本重新覆盖，随代码一起提交：

```bash
V=$(go list -m -f '{{.Version}}' github.com/vivaui/viva)
SRC="$(go env GOMODCACHE)/github.com/vivaui/viva@$V/docs/skills/building-with-viva"
for d in .claude .agents; do install -m 444 "$SRC/SKILL.md" "$SRC/recipes.md" "$d/skills/building-with-viva/"; done
```

## 硬规则（违反会 panic 或返工）

- **树键必须带集合根**：`dirKey(rootDir, dir)` / `entryKey(rootDir, file)` / `"root:"+col.Dir`，
  禁止手写 `"dir:"+dir` / `"entry:"+file`。两个集合根有同名相对路径又同时展开时会
  `panic: ui: two siblings have the key …`。回归用例 `TestTwoRootsWithSamePathsShareNoTreeKeys`；
  现象、根因与 `remapKey` 的改法见 [`doc/环境与崩溃.md`](doc/环境与崩溃.md)。
- **GUI 验证必须用软件 Vulkan**：`VK_ICD_FILENAMES=/usr/share/vulkan/icd.d/lvp_icd.x86_64.json bin/dev start`。
  硬件 Vulkan 会和 IDE 的 Electron GPU 进程抢同一块 AMD 卡，把 IDE 一起带崩（SIGTRAP）。
  取证步骤同见该文档。
- **xdotool 坐标是物理像素**（本机 ×1.25，窗口逻辑 1180×760 → 物理 1475×950）：每次重启重新取窗口 ID 与位置，
  `mousedown` 与 `mouseup` 必须写在同一条命令里。

## 集合树改动清单（易错点）

- 键：只用 `dirKey` / `entryKey` / `"root:"+dir`，保证一次绘制内唯一。
- 展开状态 `m.expanded` 的键与行键一致，跨集合不能共用。
- 选中：`selectEntryRow` / `selectFolderDir` 会把键写到 `m.selKey` 与 `m.pendingKey`；
  `applySelect` / `applyPendingSelect` 下一帧才能选中（Tree 的行数要等画完一帧）。
- 拖放：`canDrop` 只做判定，`dropRow` 才落盘；跨集合移动重扫两个根，源根的标签由 `refreshTabs` 自动关闭。
- 移动 / 克隆 / 删除分组后，路径前缀变化要同步 `tabs[].file`、`m.req.file`、`m.entryFile`、
  `m.selKey`、`m.pendingSelect`（参考 `moveFolder` / `moveEntry` 的 remap）。
- 分组移动遇到目标同名目录时按客户端语义**报错**，不要偷偷改目录名。

## 提交前检查

```bash
gofmt -l internal
bash scripts/check.sh
GOWORK=off go test -race ./...
```

新增交互必须有 `internal/view/view_test.go` 这类无头驱动用例（点 / 悬停 → 断言模型与画面文字）；
涉及磁盘格式（seq / uid / 回收站 / 克隆）的，同时在 `internal/explorer/*_test.go` 断言文件内容。
不引入每帧分配：派生数据在状态变化时算一次。
