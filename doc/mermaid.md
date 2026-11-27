# Markdown 里的 Mermaid：方案探索（未实施）

> 结论先写：viva 的 Markdown 组件没有「自定义代码块」钩子，mermaid 只能先渲染成图片、再走已有的图片通路。
> 差别只在「那张图片由谁生成」。本文记录 2026-11-26 的分析，暂不实施。

## 一、现状与约束

### 渲染管线的五个可干预层

2026-11-27 补：上一版只看了 `MarkdownStyle`，其实解析器 `github.com/vivaui/viva/markdown` 是**公开包**，
加上 `MarkdownView.Codes()`，一共五个层次可以下手。按「改动越小越靠上」排：

| 层 | 接口 | 能做的事 | 硬限制 |
|---|---|---|---|
| L1 源码 | 自己预处理字符串 | 把 ` ```mermaid ` 块改写成 `![mermaid](mermaid:<hash>)` | 预览文本与源码不再一一对应（行号偏移） |
| L2 解析 | `markdown.Document{Blocks []Block}`、`Block.Kind/Info/Text/Start/End`、`Document.Spans(b)` | 读出每个 mermaid 块的源码、语言、字节范围 | `Block` 带未导出字段（inline spans 按块下标存在 Document 里），**不能就地替换 Blocks**，只能改文本重新 Parse |
| L3 布局 | `MarkdownView.Layout(canvas, style, doc, width)`、`Codes() []MarkdownCode{Bounds, Text}` | 拿到每个代码块在版面里的矩形与内容 | 块高由**代码行数**决定，图撑不开；不能改布局 |
| L4 样式 | `MarkdownStyle.Image func(dest) (gfx.Bitmap, bool)` | **唯一**能让「一块内容」以位图身份参与排版的出口 | 只对图片语法 / `Span.Image` 生效，对代码块无效 |
| L5 绘制 | `MarkdownPaint`、`MarkdownStyle.CodeInset` | 在代码块矩形上覆盖画小部件（角标、按钮） | 与 L3 同理：画得下才算，画不下就被裁 |

`MarkdownPaint` 里没有块级回调，`MarkdownLook` 只有颜色与圆角 —— 所以 L5 只能「覆盖」，不能「替换」。

### 组件只给两个扩展点

`components.MarkdownStyle` 里能挂的只有：

```go
Highlight func(language, line string, dst []MarkdownCodeRun) []MarkdownCodeRun
Image     func(destination string) (gfx.Bitmap, bool)
```

- `Highlight` 只能把一行代码染成几段彩色文字，**画不了图**；
- `Image` 是唯一的块级出口：`![alt](dest)` 会回问要一张 `gfx.Bitmap`；
- `MarkdownLook` 只有颜色与圆角，没有 block 级重建回调；
- 因此 **不能**让组件在代码块的位置直接画一张图。

### 已有的一半能力

`internal/view/docimage.go` 已经按路径解码图片并缓存，粘贴图片还能写进集合的 `assets/`。
所以「把 mermaid 块换成图片引用」这条路的后半段是现成的。

### 两个硬约束

- **没有公开的离屏 canvas**：viva 只暴露平台内部的 offscreen。要位图就得用 `image.RGBA` 自己栅格化，
  或者引第三方画图库（`fogleman/gg` 之类），再用 `x/image/font` 加载系统字体——与 viva 自己的字体引擎不同源，观感会有差异。
- **离线优先、纯文本存储**是项目定位：联网渲染（kroki/mermaid.ink）和打包 Chromium 都不合适。

## 二、四个方案

### A. 预处理成图片 + 外部渲染器

抽出 ` ```mermaid ` 块渲染成 PNG，写进 `assets/`（或内存缓存），源码里替换成
`![mermaid](assets/mermaid-<hash>.png)`，交给已有图片通路。

渲染器来源：

| 来源 | 体积 / 依赖 | 质量 | 离线 |
|---|---|---|---|
| mermaid-cli（`mmdc`） | Node + Chromium，约 400MB | 最好 | 装得上才可用 |
| kroki.io / mermaid.ink | 零依赖 | 好 | 否，且文档内容外发 |
| 用户自带 `mmdc` | 只做检测 | 最好 | 取决于用户环境 |

- 优点：改动最小，覆盖率 100%；
- 缺点：要么安装包暴涨，要么默认不可用；HTTP 方案违反离线定位。

### B. 纯 Go 自绘（解析子集 + 自己画）

自己解析 mermaid 语法，布局后画到位图，`Image` 回调里按 hash 返回。

- 优点：零外部依赖、离线、配色跟主题走、尺寸可控、可缓存；
- 缺点：工作量最大，每种图都要一个布局器，且只能覆盖子集；
- 落地细节：`gfx.Bitmap{Pix []byte, Width, Height, Stride}` 可以直接构造（premultiplied BGRA），
  **不需要**第三方画图库；难的是字：viva 的字体引擎不对外暴露「把字画进我的像素缓冲」，
  图内文字要么用 `golang.org/x/image/font` 自己排（与正文观感有差），要么只画图形 + 图例走正文。

覆盖难度递增：`pie` → `flowchart TD/LR` → `sequenceDiagram` → `classDiagram` / `stateDiagram` → `gantt` / `timeline`。

### C. 混合：自绘子集 + 外部渲染器兜底（倾向方案）

`pie` / `flowchart` / `sequence` 自绘（覆盖绝大多数 README 场景）；其他类型在检测到 `mmdc` 时交给它，
否则显示代码块 + 一行「暂不支持渲染」。

默认离线可用，装了外部工具还能提升质量。

### D. 外部进程按需调用（不预处理）

预览时发现 mermaid 块就起进程（`mmdc -i - -o out.png`），结果缓存到 `~/.cache/`。

- 优点：不往集合里写文件（不污染纯文本存储）、质量最好；
- 缺点：进程管理与超时、默认可用性差、导出时要另走一遍。

## 三、三个必须一起考虑的点

1. **缓存键**：源码 hash + 主题（明/暗）+ 渲染宽度。宽度变化会影响换行与布局，不能只按源码缓存。
2. **后台渲染**：`MarkdownStyle.Image` 是**在 Layout 时**被问的（一张图还可能问多次——表格先量单元格），
   所以渲染器不能同步跑。官方姿势是：首次返回 `false`（文档按 alt 文本排版），
   渲染完成后 `Invalidate` 重排一次；位图给稳定的 `gfx.Bitmap.ID`，换图时递增 `Version`，
   否则每帧都会把像素重新上传一次。走 `ui.Effect` 起后台任务。
3. **导出**：`openExport("md"/"html")` 目前会原样带走代码块。要么导出时一并生成图片并改写引用，
   要么在导出面板里明确写「导出不含图」。
4. **行号对齐（L1 特有）**：代码块换成图片语法后行数会变，将来若做「预览滚动同步源码」，
   需要维护一张原文 ↔ 预览的偏移表，或者替换时补足空行。

## 四、推荐方案的骨架（L1 + L4）

```go
// 1) L2 用来定位：不要用正则扫源码，缩进 / 引用里的 fence 会漏。
var doc markdown.Document
doc.Parse(src)
for i := range doc.Blocks {
    b := &doc.Blocks[i]
    if b.Kind != markdown.Code {
        continue
    }
    lang := strings.ToLower(strings.Fields(b.Info + " ")[0]) // Info 可能带额外参数
    if lang != "mermaid" {
        continue
    }
    // b.Start / b.End 是含 fence 的字节范围 → 在这一段上做替换，其余原样拷贝
}

// 2) L1 替换：![mermaid](mermaid:<sha1>)；alt 文本留在正文里，搜索与复制仍能命中
func rewriteMermaid(src string) string

// 3) L4 接住：只在 Image 回调里分派，文档其余图片仍走 docImage
Image: func(destination string) (gfx.Bitmap, bool) {
    if hash, ok := strings.CutPrefix(destination, "mermaid:"); ok {
        return m.mermaidBitmap(hash) // 命中缓存返回位图；没渲染好就 false（按 alt 排版）
    }
    return m.docImage(docBase(m.editor.file), destination)
}

// 4) 渲染器接口：外部命令与纯 Go 子集共用，谁先实现都不影响上层
type DiagramRenderer interface {
    // px 是目标像素宽（按 DPR 放大），返回的位图直接当布局尺寸用。
    Render(src string, px int, dark bool) (gfx.Bitmap, error)
}
```

替换时要保持「块的字节范围之外一个字符都不动」，这样同一份 `Document` 里其他块的 `Start/End` 仍然可对回原文。

## 五、建议的落地顺序

1. **先做 A 的骨架**：扫描 + 替换成图片引用 + 缓存 + 没有渲染器时降级成代码块。
   这一步不引外部依赖，能立刻把「mermaid 不显示」变成「结构正确、可读」；
2. 再补 **B 的前两种图**（`pie`、`flowchart`），在 `Image` 回调里优先自绘；
3. 最后做 **C 的检测**（有 `mmdc` 就交给它，覆盖 class / gantt 这类难图）。

## 六、待定问题

- 自绘的字体与 viva 画布不同源，同一篇文档里图内文字与正文会有细微差异，可接受否？
- 图要不要支持「点击放大 / 复制源码」？这会牵扯预览区的命中区域。
- 编辑器侧是否给 mermaid 块做语法高亮（`Highlight` 能做，代价是给 `mermaid` 语言写一份规则）。
