package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteICOStructure 写出来的 .ico 要能被按文件格式读回来：
// 头里的条目数、每档宽高、数据偏移都在文件范围内。
func TestWriteICOStructure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icon.ico")
	if err := writeICO(path, []int{16, 32, 256}); err != nil {
		t.Fatalf("writeICO: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s: %v", path, err)
	}
	if got := binary.LittleEndian.Uint16(data[2:]); got != 1 {
		t.Fatalf("type 应是 1（icon），实际 %d", got)
	}
	count := int(binary.LittleEndian.Uint16(data[4:]))
	if count != 3 {
		t.Fatalf("条目数应是 3，实际 %d", count)
	}

	want := []int{16, 32, 256}
	for index := 0; index < count; index++ {
		base := 6 + index*16
		width := int(data[base])
		if width == 0 {
			width = 256
		}
		if width != want[index] {
			t.Fatalf("第 %d 档应是 %d，实际 %d", index, want[index], width)
		}
		if data[base+1] != data[base] {
			t.Fatalf("第 %d 档宽高不一致：%d/%d", index, data[base], data[base+1])
		}
		if bpp := binary.LittleEndian.Uint16(data[base+6:]); bpp != 32 {
			t.Fatalf("第 %d 档应是 32 位，实际 %d", index, bpp)
		}
		length := binary.LittleEndian.Uint32(data[base+8:])
		offset := binary.LittleEndian.Uint32(data[base+12:])
		if int(offset+length) > len(data) {
			t.Fatalf("第 %d 档数据越界：offset=%d len=%d 文件=%d", index, offset, length, len(data))
		}
		// 一档 DIB = 40 字节头 + 4×size² 像素 + 掩码。
		size := want[index]
		maskRow := ((size + 31) / 32) * 4
		minLength := uint32(40 + size*size*4 + maskRow*size)
		if length < minLength {
			t.Fatalf("第 %d 档数据长度 %d，至少应 %d", index, length, minLength)
		}
	}
}

// TestDIBEntryIsBottomUp 像素按 DIB 规矩自下而上：第 0 行是图像最下面一行。
// 图标上下对称看不出区别，所以这里直接查「左上角透明、中心不透明」两种典型点。
func TestDIBEntryTransparentCorner(t *testing.T) {
	const size = 32
	data := dibEntry(size)
	pixels := data[40:]
	// 文件里第一行 = 图像最下面一行；左下角在圆角外，应透明。
	firstPixel := pixels[0*size*4+3]
	if firstPixel != 0 {
		t.Fatalf("左下角应透明，实际 alpha=%d", firstPixel)
	}
	// 图标是透明底的线条画，中心可能留白：数不透明像素，确认画进来了。
	opaque := 0
	for i := 3; i < size*size*4; i += 4 {
		if pixels[i] != 0 {
			opaque++
		}
	}
	if opaque < size*size/20 {
		t.Fatalf("图标几乎是空的：不透明像素 %d / %d", opaque, size*size)
	}
}
