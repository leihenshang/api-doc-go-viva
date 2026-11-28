# api-doc-go-viva

用 [Viva](https://github.com/vivaui/viva)（Go 优先、原生 GPU 绘制的桌面框架，无 WebView）复刻
api-doc-go 桌面客户端的**可行性验证版**：同一份集合文件、同一个 Go 内核，界面换成 Viva 的原生窗口。

**结论：可行。** 集合格式与 HTTP 执行层原样复用（`api-doc-go-share/collection`），
界面层用 Viva 的声明式 API 写成普通 Go 代码，无头测试能完整驱动「点行 → 点发送 → 断言响应」。

![集合概览：四张数字卡 + 常用动作，固定的「集合」页](doc/viva-overview.png)

![请求页：树里选中请求，右侧地址栏与分段编辑，动作按钮紧跟 URL](doc/viva-request.png)

![文档条目：左栏源码、右栏实时预览](doc/viva-docs.png)

## 跑起来

Go 1.26；Linux 需要可用的 Vulkan 驱动。

```bash
export PATH=$PATH:$HOME/go/bin

bash scripts/check.sh          # 门禁：gofmt + build + vet + test（含无头界面测试）

bin/dev start                  # 构建到 bin/.cache/ 并后台启动，日志 bin/.cache/dev.log
bin/dev status | log | stop | restart
COLLECTION=../api-doc-go-proj ENV_NAME= bin/dev start   # 换集合 / 关环境
```

`GOWORK=off` 只在本机需要：上一级的 `go.work` 只 `use` 三个正式模块，本模块不在其中。
`go.mod` 依赖 `api-doc-go-share` 的正式版本、没有 `replace`，换台机器 `go mod download` 即可构建。

首次启动没给 `-collection` 时，先找上次打开的集合，再回落到示例集合，都没有就打开「尚未打开集合」
首屏（点「打开集合」选目录），不会直接退出——Windows 双击 exe 同理。

## 文档

| 文档 | 内容 |
|---|---|
| [`doc/功能与界面.md`](doc/功能与界面.md) | 做了什么、集合树逐项能力、界面截图、组件与图标、UI 基线 |
| [`doc/上手与构建.md`](doc/上手与构建.md) | 怎么跑、多集合根、Windows 构建、`.deb` / NSIS 打包、目录说明 |
| [`doc/测试与验证.md`](doc/测试与验证.md) | 无头用例怎么证明它真的通了、实测数据、框架层面的发现、明确没做的 |
| [`doc/任务清单.md`](doc/任务清单.md) | 未做项（N 编号）、上游待办、明确不做 |
| [`doc/执行计划.md`](doc/执行计划.md) | 各批次的完成表（T 编号）与验收口径 |
| [`doc/mermaid.md`](doc/mermaid.md) | 文档预览渲染 Mermaid：五个方案的取舍、实现原理（切段 + goja 跑 dagre + canvas 绘制）、给 viva 的 PR 建议 |
| [`doc/环境与崩溃.md`](doc/环境与崩溃.md) | 本机踩过的崩溃、软件 Vulkan 规定、xdotool 截图规则 |
| [`doc/viva-0.6.9.md`](doc/viva-0.6.9.md) | 升级 v0.6.9 的破坏性变更迁移、已替换的自有实现、没换的项与原因、复查清单 |
| [`doc/viva-0.6.10.md`](doc/viva-0.6.10.md) | 升级 v0.6.10 的记录：无破坏性变更、skills 差异、新组件的适配评估 |
| [`doc/ime-linux.md`](doc/ime-linux.md) | Linux 下系统输入法打不出中文的原因与证据 |
| [`doc/third-party.md`](doc/third-party.md) | 第三方依赖与许可：直接 / 间接依赖、嵌入的 dagre.min.js、经 viva 引入的图标字体、分发义务 |
| [`ui_reference/README.md`](ui_reference/README.md) | Wails 版 UI 参考：截图、metrics、令牌 |

## AI 助手

仓库带 Viva 官方的 `building-with-viva` skill（`.claude/skills/` 与 `.agents/skills/` 各一份只读拷贝，
对应 v0.6.9）：写界面、改交互之前先读它。项目约定、硬规则与提交前检查见 [`AGENTS.md`](AGENTS.md)。

## 许可

**MIT + Commons Clause**（全文见 `LICENSE`）。

简单说：**所有人都可以免费使用，但不能把这个软件本身拿去卖钱**。

- **可以**：免费使用（含公司内部生产使用）、修改、再分发、把它嵌进自己的产品一起分发甚至销售——
  只要你的产品不是「实质上就是这个软件」；
- **不可以**：把它本身（或改个名字、改几个函数名）拿来卖，或者只把它的功能包一层就做成收费服务；
- 商业授权（就是想卖它本身）请通过仓库 issue 联系作者。

三点说明：

1. 这不是 OSI 定义的开源许可（Commons Clause 明确不满足开源定义），请叫它 **source-available**；
2. 版权仍归作者，作者本人不受这些限制，也可以随时换成更宽松的许可；
3. 第三方依赖各自独立授权（viva Apache-2.0、goja MIT、dagre MIT …），本项目许可不改变其条款，
   见 `doc/third-party.md`；另外 Commons Clause 要求再分发时保留本许可声明。
