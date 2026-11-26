# Markdown 里的 Mermaid：方案探索（未实施）

> 结论先写：viva 的 Markdown 组件没有「自定义代码块」钩子，mermaid 只能先渲染成图片、再走已有的图片通路。
> 差别只在「那张图片由谁生成」。本文记录 2026-11-26 的分析，暂不实施。

## 一、现状与约束

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
- 落地细节：位图用 `image.RGBA` 手画（圆角矩形、线段、箭头、文字），文字需 `golang.org/x/image/font/opentype`。

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
2. **后台渲染**：按 viva 的约定走 `ui.Effect`，先返回占位图（固定高度 + 「渲染中」），完成后 `Invalidate`，
   不能在窗口线程里同步渲染。
3. **导出**：`openExport("md"/"html")` 目前会原样带走代码块。要么导出时一并生成图片并改写引用，
   要么在导出面板里明确写「导出不含图」。

## 四、建议的落地顺序

1. **先做 A 的骨架**：扫描 + 替换成图片引用 + 缓存 + 没有渲染器时降级成代码块。
   这一步不引外部依赖，能立刻把「mermaid 不显示」变成「结构正确、可读」；
2. 再补 **B 的前两种图**（`pie`、`flowchart`），在 `Image` 回调里优先自绘；
3. 最后做 **C 的检测**（有 `mmdc` 就交给它，覆盖 class / gantt 这类难图）。

## 五、待定问题

- 自绘的字体与 viva 画布不同源，同一篇文档里图内文字与正文会有细微差异，可接受否？
- 图要不要支持「点击放大 / 复制源码」？这会牵扯预览区的命中区域。
- 编辑器侧是否给 mermaid 块做语法高亮（`Highlight` 能做，代价是给 `mermaid` 语言写一份规则）。
