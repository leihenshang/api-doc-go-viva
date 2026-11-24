// Package brand 是应用图标的唯一来源：与 Wails 版同一张图（build/appicon.png），
// 编译期嵌进二进制。
//
// 三处复用：运行时窗口 / 托盘图标、打包时的 .ico、文档里的 PNG——改一张图，
// 程序里、安装包里、任务栏上一起变。
package brand

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"sync"
)

//go:embed appicon.png
var iconPNG []byte

var (
	iconOnce sync.Once
	iconSrc  image.Image
)

// RGBA 返回 size 见方的直通（非预乘）RGBA，供 viva 的 StatusIcon / WindowIcon 用。
//
// 源图是 1024 见方的 PNG，按最近邻降采样：系统最终只画 16~32px，
// 双线性在这里只会把边缘糊成一圈灰。
func RGBA(size int) []byte {
	if size <= 0 {
		return nil
	}
	iconOnce.Do(func() {
		if img, err := png.Decode(bytes.NewReader(iconPNG)); err == nil {
			iconSrc = img
		}
	})
	out := make([]byte, size*size*4)
	if iconSrc == nil {
		return out
	}
	b := iconSrc.Bounds()
	for y := 0; y < size; y++ {
		sy := b.Min.Y + y*b.Dy()/size
		for x := 0; x < size; x++ {
			sx := b.Min.X + x*b.Dx()/size
			c := color.NRGBAModel.Convert(iconSrc.At(sx, sy)).(color.NRGBA)
			i := (y*size + x) * 4
			out[i+0], out[i+1], out[i+2], out[i+3] = c.R, c.G, c.B, c.A
		}
	}
	return out
}

// PNG 返回原始图标字节。
func PNG() []byte { return iconPNG }

// Image 返回 size 见方的图（打包时写 PNG 用）。
func Image(size int) image.Image {
	return &image.NRGBA{Pix: RGBA(size), Stride: size * 4, Rect: image.Rect(0, 0, size, size)}
}
