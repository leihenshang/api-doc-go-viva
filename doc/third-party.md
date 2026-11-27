# 第三方依赖与许可

2026-11-27 建立。梳理三部分：`go.mod` 的直接依赖、**随二进制分发的非 Go 资源**、
以及经 viva 间接引入的第三方资产。

> 仓库自身还没有 `LICENSE`（未声明开源许可），所以这里只记录第三方部分。
> 如果将来要开源，需要先补一份自己的许可再谈分发。

## 一、直接依赖（`go.mod` 的 require 块）

| 模块 | 版本 | 许可 | 用途 |
|---|---|---|---|
| `github.com/vivaui/viva` | v0.6.9 | Apache-2.0 | GUI 框架：布局、绘制、文本、输入法 |
| `github.com/dop251/goja` | v0.0.0-20260926152631-39ec2650adc9 | MIT | 请求前后置脚本与断言；mermaid 布局在它里面跑 dagre |
| `github.com/modelcontextprotocol/go-sdk` | v1.8.0 | MIT | MCP 服务端（工具注册、stdio / streamable HTTP） |
| `github.com/bufbuild/protocompile` | v0.14.1 | Apache-2.0 | `.proto` 编译（gRPC 请求构造） |
| `google.golang.org/grpc` | v1.85.0 | Apache-2.0 | gRPC 客户端 |
| `google.golang.org/protobuf` | v1.36.12 | BSD-3-Clause | protobuf 运行时 |
| `gopkg.in/yaml.v3` | v3.0.1 | MIT | 集合 / 环境 / 请求的 YAML 读写 |
| `github.com/leihenshang/api-doc-go-share` | v0.15.3 | MIT | 与 Wails 版共享的数据结构（本组织自有模块） |

## 二、关键间接依赖（`go.mod` 的 indirect 块）

都是被上面几个链进来的，构建产物里会真正参与链接，所以一并登记。

| 模块 | 版本 | 许可 | 谁链进来的 |
|---|---|---|---|
| `github.com/dlclark/regexp2/v2` | v2.5.2 | MIT | goja（正则） |
| `github.com/go-sourcemap/sourcemap` | v2.1.3+incompatible | BSD-3-Clause | goja（source map） |
| `github.com/google/pprof` | v0.0.0-20230207041349 | Apache-2.0 | goja（profile） |
| `github.com/ebitengine/purego` | v0.11.1 | Apache-2.0 | viva（系统调用） |
| `github.com/segmentio/asm` | v1.1.3 | MIT | segmentio/encoding |
| `github.com/segmentio/encoding` | v0.5.4 | MIT | grpc / protobuf（JSON 编解码） |
| `github.com/google/jsonschema-go` | v0.4.3 | MIT | MCP SDK（schema） |
| `github.com/yosida95/uritemplate/v3` | v3.0.2 | BSD-3-Clause | MCP SDK（URI 模板） |
| `google.golang.org/genproto/googleapis/rpc` | v0.0.0-20260817212433 | Apache-2.0 | grpc |
| `golang.org/x/net` | v0.60.0 | BSD-3-Clause | grpc |
| `golang.org/x/oauth2` | v0.36.0 | BSD-3-Clause | grpc |
| `golang.org/x/sync` | v0.25.0 | BSD-3-Clause | grpc |
| `golang.org/x/sys` | v0.50.0 | BSD-3-Clause | viva / grpc |
| `golang.org/x/text` | v0.44.0 | BSD-3-Clause | viva / grpc |
| `golang.org/x/time` | v0.15.0 | BSD-3-Clause | grpc |

完整清单（含只在某个平台或测试里出现的）以 `go list -m all` 为准。

## 三、随二进制分发的非 Go 资源

| 文件 | 体积 | 来源 | 许可 | 校验 |
|---|---|---|---|---|
| `internal/view/assets/dagre.min.js` | 96,038 B | [`@dagrejs/dagre@1.1.4`](https://github.com/dagrejs/dagre)（npm 包 `dist/dagre.min.js`） | MIT | `sha256 2cde82baf0b9232c00aa13932945ff92a17fb08c6885a635592330a4c2c567c5` |

说明：

- 这是 mermaid 预览的布局引擎，用 `go:embed` 打进二进制，由 goja 在进程内执行（见 `doc/mermaid.md`）。
- **min 版没有内联的许可头**（文件以 UMD 包装开头），所以来源、版本与校验值以本表为准；
  升级 dagre 时同步更新这一行（版本、体积、sha256）。
- 校验命令：`sha256sum internal/view/assets/dagre.min.js`。
- 除这个文件外，仓库里没有别的第三方二进制资源；`doc/*.png` 都是本项目自己的截图。

## 四、经 viva 间接引入的资产

viva 本身是 Apache-2.0，它的 `NOTICE` 声明了内部包含的第三方：

| 组件 | 许可 | 本项目是否用到 |
|---|---|---|
| shadcn-svelte | MIT | 否（只是它的设计来源） |
| Lucide | ISC（部分派生自 Feather，MIT） | **是**：所有 `Icon*` 图标 |
| Unicode Character Database | Unicode License V3 | 间接（文本宽度与断行） |
| Ghostty | MIT | 间接（终端渲染思路 / 部分表数据） |
| `github.com/ebitengine/purego` | Apache-2.0 | 是（已在第二节） |
| `github.com/pelletier/go-toml/v2` | MIT | 否 |
| `golang.org/x/text` | BSD-3-Clause | 是（已在第二节） |

完整文本在 viva 模块的 `THIRD_PARTY_NOTICES.md`，仓库路径
`$(go env GOMODCACHE)/github.com/vivaui/viva@v0.6.9/THIRD_PARTY_NOTICES.md`。

## 五、分发时的义务

| 许可 | 义务 | 涉及 |
|---|---|---|
| Apache-2.0 | 附带 `LICENSE` 与 **`NOTICE`**；修改过的文件要标注 | viva、grpc、protocompile、purego、pprof、genproto |
| MIT | 保留版权声明与许可文本 | goja、yaml.v3、MCP SDK、dagre、regexp2、segmentio、jsonschema-go、share |
| BSD-3-Clause | 保留版权声明、许可文本，且不得用作者名背书 | protobuf、x/*、sourcemap、uritemplate |
| ISC | 保留版权声明与许可文本 | Lucide（经 viva） |

**最容易漏的是 Apache-2.0 的 NOTICE**：viva 的 `NOTICE` 与 `THIRD_PARTY_NOTICES.md` 必须随发布物一起给出去，
只在二进制里链接是不够的。

仓库目前没有 `licenses/` 目录，建议发布前导出一份：

```bash
go run github.com/google/go-licenses@latest save ./... --save_path=./licenses --force
```

## 六、维护约定

1. **改 `go.mod` 就改第一节 / 第二节**（新增、升级、移除都要动表）。
2. **新增嵌入资源必须在第三节登记**：文件名、体积、来源（含版本）、许可、sha256。
3. 每次发版前跑一遍：`go list -m all | wc -l`（数量变化说明依赖动过）、
   `sha256sum internal/view/assets/*`（资源是否被替换）。
