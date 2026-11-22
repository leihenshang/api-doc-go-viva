// makeicon 把 internal/brand 的图标画法落成安装包要的文件：
// build/windows/icon.ico（多尺寸）与 icon.png（文档与预览用）。
//
// 为什么要生成而不是放一张 png：安装程序要的 .ico 是 Windows 专用容器
// （每档尺寸一份 DIB），手搓一份容易过时；这里每次打包前重新生成，
// 图标的唯一来源始终是代码。
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/leihenshang/api-doc-go-viva/internal/brand"
)

// icoSizes 是 .ico 里放的尺寸：16/24/32 给任务栏与小图标，
// 48/64 给资源管理器，128/256 给高 DPI 与「大图标」视图。
var icoSizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	out := flag.String("out", "build/windows", "输出目录（会写入 icon.ico 与 icon.png）")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("建目录 %s: %v", *out, err)
	}
	icoPath := filepath.Join(*out, "icon.ico")
	if err := writeICO(icoPath, icoSizes); err != nil {
		log.Fatalf("写 %s: %v", icoPath, err)
	}

	pngPath := filepath.Join(*out, "icon.png")
	file, err := os.Create(pngPath)
	if err != nil {
		log.Fatalf("建 %s: %v", pngPath, err)
	}
	if err := png.Encode(file, brand.Image(256)); err != nil {
		_ = file.Close()
		log.Fatalf("写 %s: %v", pngPath, err)
	}
	if err := file.Close(); err != nil {
		log.Fatalf("关 %s: %v", pngPath, err)
	}

	info, _ := os.Stat(icoPath)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	fmt.Printf("图标已生成：%s（%d 档，%d 字节）、%s\n", icoPath, len(icoSizes), size, pngPath)
}

// writeICO 写一份 32 位 BMP 条目的 .ico。
//
// 用未压缩 DIB 而不是 PNG 条目：PNG-in-ICO 只有 Vista 之后认，
// 而 DIB 从 Win95 起就是标准，NSIS 与各预览器都稳。
func writeICO(path string, sizes []int) error {
	var body bytes.Buffer
	type entry struct {
		size   int
		length int32
	}
	entries := make([]entry, 0, len(sizes))

	// 先占位 6 字节头 + 每个条目 16 字节，图数据追加在后面。
	headerSize := 6 + 16*len(sizes)
	if err := binary.Write(&body, binary.LittleEndian, make([]byte, headerSize)); err != nil {
		return err
	}
	for _, size := range sizes {
		data := dibEntry(size)
		entries = append(entries, entry{size: size, length: int32(len(data))})
		if _, err := body.Write(data); err != nil {
			return err
		}
	}

	// 回填头与条目表。
	out := body.Bytes()
	binary.LittleEndian.PutUint16(out[2:], 1)                  // type = icon
	binary.LittleEndian.PutUint16(out[4:], uint16(len(sizes))) // count
	offset := int32(headerSize)
	for index, item := range entries {
		base := 6 + index*16
		dim := byte(item.size)
		if item.size >= 256 {
			dim = 0 // 0 表示 256
		}
		out[base] = dim
		out[base+1] = dim
		out[base+2] = 0                                 // 调色板颜色数
		out[base+3] = 0                                 // 保留
		binary.LittleEndian.PutUint16(out[base+4:], 1)  // planes
		binary.LittleEndian.PutUint16(out[base+6:], 32) // bpp
		binary.LittleEndian.PutUint32(out[base+8:], uint32(item.length))
		binary.LittleEndian.PutUint32(out[base+12:], uint32(offset))
		offset += item.length
	}
	return os.WriteFile(path, out, 0o644)
}

// dibEntry 是 .ico 里的一档：BITMAPINFOHEADER + 自下而上的 BGRA + AND 掩码。
func dibEntry(size int) []byte {
	pixels := brand.RGBA(size)
	maskRow := ((size + 31) / 32) * 4
	maskSize := maskRow * size
	var buf bytes.Buffer

	header := make([]byte, 40)
	binary.LittleEndian.PutUint32(header[0:], 40)                            // biSize
	binary.LittleEndian.PutUint32(header[4:], uint32(size))                  // biWidth
	binary.LittleEndian.PutUint32(header[8:], uint32(size*2))                // biHeight = XOR + AND
	binary.LittleEndian.PutUint16(header[12:], 1)                            // biPlanes
	binary.LittleEndian.PutUint16(header[14:], 32)                           // biBitCount
	binary.LittleEndian.PutUint32(header[20:], uint32(size*size*4+maskSize)) // biSizeImage
	_, _ = buf.Write(header)

	// XOR：DIB 是自下而上、BGRA。
	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			i := (y*size + x) * 4
			buf.Write([]byte{pixels[i+2], pixels[i+1], pixels[i], pixels[i+3]})
		}
	}
	// AND 掩码全 0：透明由 alpha 通道表达（32 位图标里掩码只为兼容老读法）。
	_, _ = buf.Write(make([]byte, maskSize))
	return buf.Bytes()
}
