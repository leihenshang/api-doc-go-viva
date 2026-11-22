// Package brand 是应用图标的唯一来源：圆角方块 + 三条横线（像一列请求）。
//
// 只有一份画法，三处复用：运行时窗口 / 托盘图标、打包时的 .ico、文档里的 PNG。
// 这样改一次颜色或比例，程序里、安装包里、任务栏上都不会各长一副样子。
package brand

import (
	"image"
	"math"
)

// Accent 是图标底色（与界面强调色一致）。
var Accent = [3]uint8{59, 130, 246}

// RGBA 返回 size 见方的直通（非预乘）RGBA，按 4 倍尺寸画再降采样，
// 因为系统会把它缩到 16~22px，直接画小图边缘会糊。
func RGBA(size int) []byte {
	const scale = 4
	big := size * scale
	pixels := make([]byte, big*big*4)
	radius := float64(big) * 0.24
	for y := 0; y < big; y++ {
		for x := 0; x < big; x++ {
			i := (y*big + x) * 4
			if !insideRoundRect(float64(x)+0.5, float64(y)+0.5, float64(big), radius) {
				continue
			}
			r, g, b := Accent[0], Accent[1], Accent[2]
			// 三条白色横线：像列表里的一列条目。
			for _, center := range []float64{0.28, 0.5, 0.72} {
				cy := center * float64(big)
				if float64(y)+0.5 > cy-float64(big)*0.05 && float64(y)+0.5 < cy+float64(big)*0.05 &&
					float64(x)+0.5 > float64(big)*0.28 && float64(x)+0.5 < float64(big)*0.72 {
					r, g, b = 255, 255, 255
				}
			}
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = r, g, b, 255
		}
	}
	return downsample(pixels, big, size)
}

// Image 是 RGBA 的 image 版本（写 PNG / ICO 时用）。
func Image(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	copy(img.Pix, RGBA(size))
	return img
}

// insideRoundRect 判断点是否落在圆角矩形内：把点夹到「内矩形」上，
// 到夹点的距离不超过圆角半径就在里面（标准圆角矩形的距离写法）。
func insideRoundRect(x, y, side, radius float64) bool {
	cx := math.Min(math.Max(x, radius), side-radius)
	cy := math.Min(math.Max(y, radius), side-radius)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}

// downsample 把 big 见方的 RGBA 盒式降采样成 size 见方（取平均，边缘不出锯齿）。
func downsample(pixels []byte, big, size int) []byte {
	factor := big / size
	out := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a, n int
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					i := ((y*factor+dy)*big + (x*factor + dx)) * 4
					r += int(pixels[i])
					g += int(pixels[i+1])
					b += int(pixels[i+2])
					a += int(pixels[i+3])
					n++
				}
			}
			o := (y*size + x) * 4
			if n == 0 {
				continue
			}
			out[o], out[o+1], out[o+2], out[o+3] = uint8(r/n), uint8(g/n), uint8(b/n), uint8(a/n)
		}
	}
	return out
}
