# 升级 viva 0.6.9 → 0.6.10（2026-11-28）

## 结论：无破坏性变更，直接换版本即可

```bash
go get github.com/vivaui/viva@v0.6.10   # 只动了 go.mod / go.sum
go build ./...                           # 无报错
bash scripts/check.sh                    # 全绿
```

和 0.6.9 那次不同（那次有 `components.Card` → `ui/card`、`MarkdownStyle` 字段变化等，
见 `doc/viva-0.6.9.md`），0.6.10 是纯增量：**没有改签名、没有删 API**。

## skills 同步

`building-with-viva` 从模块的 `docs/skills/` 重新拷到 `.claude/skills/` 与 `.agents/skills/`。

两个版本之间的差异只有 SKILL.md 的 5 行、recipes.md 完全没变：

1. 组件清单补了 5 个新组件：`ui/overflowlist`、`ui/statistic`、`ui/inputtag`、
   `ui/autocomplete`、`ui/indicator`
2. 新增一条关于 `ui.TextSelectionArea` 的说明（只读文字可选中复制）
3. 「系统文件」那段把新组件并列进「整块区域」那一类

## 新能力评估：哪些能在本项目用上

按「收益 / 风险」排序，逐项对照现状。

| 能力 | 现状 | 评估 |
|---|---|---|
| `ui.TextSelectionArea` | `internal/view/jsonselect.go`（470 行自绘：锚点、拖选、Ctrl+C、折叠箭头） | **不换**。它只让区域内的 `Text`/`Paragraph`/`Wrap` 参与选中，而响应正文是自绘的 JSON 折叠树（行号、语法高亮、上千行虚拟化），包不进去。适合的是**小段只读文本**：MCP 提示、连接信息、导入结果、错误详情 —— 这些目前是普通 `ui.Text`，确实选不中 |
| `ui/statistic` | 概览页 `statCard`（`card.View` + `ui.Text`） | 可换，收益小（现有卡片已对齐设计稿）。它的 `Format(value, precision, sep)` 对概览页的数字没意义（都是整数或路径字符串） |
| `ui/overflowlist` | 标签栏自绘「⋯」菜单（`tabOverflow`）+ 横向滚动 | 可换，但要重做交互（现在是「滚动 + 溢出菜单」双通道）。风险中等，收益是少维护一段自绘 |
| `ui/autocomplete` | URL 输入框（纯 `ui.Field`） | **新功能**：环境变量（`{{host}}`）与历史 URL 补全。收益中，需要先想清楚候选来源 |
| `ui/inputtag` | 参数/请求头是表格编辑器（`fields.go`） | 不建议换：表格要「名/值/说明/启用」四列，tag 输入承载不了 |
| `ui/indicator` | 脏标记靠标签上的圆点（自绘） | 可换，收益低 |

## 建议的下一步

1. **`TextSelectionArea` 用在「详情小面板」**（低风险、立刻能感觉到）：
   连接信息、MCP 审计详情、导入摘要、响应头/请求头这类几行到几十行的只读文本。
2. **`autocomplete` 给 URL 栏做补全**（收益最高，但要先定候选：环境名、集合内其他 URL、历史）。
3. `overflowlist` 可以留到标签栏下次改动时顺带换掉。

响应正文那套自绘（`jsonselect.go`）**不换**：它要的是折叠树 + 语法高亮 + 大响应性能，
`TextSelectionArea` 的模型是「一行行 `ui.Text` 的可选区域」，两者目标不同。
