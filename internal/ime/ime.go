// Package ime 判断「本机输入法能不能被 Viva 用上」。
//
// Viva 的 Linux 后端只走 IBus 的 D-Bus 协议（没有 XIM，也不读 XMODIFIERS）：
// 它按 $IBUS_ADDRESS，或 ~/.config/ibus/bus/<machine-id>-<host>-<display> 里的
// IBUS_ADDRESS= 行去连输入法；两者都拿不到地址时，窗口干脆不创建输入上下文，
// 键直接进文本框——表现就是「系统输入法打不出中文，英文正常」。
//
// 这里把这条判定复述一遍，只为了在设置里如实告诉用户，不做任何输入法实现。
package ime

import (
	"os"
	"path/filepath"
	"strings"
)

// Status 是一次探测的结果。
type Status struct {
	// Available 表示 Viva 能发现一个 IBus 地址（不代表引擎一定应答）。
	Available bool
	// Detail 是给界面看的说明。
	Detail string
}

// Detect 按 Viva 的发现顺序检查输入法地址。
func Detect() Status {
	if addr := strings.TrimSpace(os.Getenv("IBUS_ADDRESS")); addr != "" {
		return Status{Available: true, Detail: "IBus：" + addr + "（来自 IBUS_ADDRESS）"}
	}
	if path, line := addressFile(); line != "" {
		return Status{Available: true, Detail: "IBus：" + line + "（来自 " + path + "）"}
	}
	if fcitx := os.Getenv("XMODIFIERS"); strings.Contains(fcitx, "fcitx") {
		return Status{
			Available: false,
			Detail: "不可用：本机是 fcitx5（XMODIFIERS=" + fcitx + "），Viva 不读 XIM；" +
				"改用 IBus 输入法框架，或从别处复制中文再粘贴",
		}
	}
	return Status{Available: false, Detail: "不可用：没有发现 IBus 地址，中文只能靠粘贴"}
}

// addressFile 找 Viva 会读的那个地址文件并取出其中的 IBUS_ADDRESS 行。
func addressFile() (path, address string) {
	machine := machineID()
	if machine == "" {
		return "", ""
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		config = filepath.Join(home, ".config")
	}
	host, number := displayParts(os.Getenv("DISPLAY"))
	path = filepath.Join(config, "ibus", "bus", machine+"-"+host+"-"+number)
	data, err := os.ReadFile(path)
	if err != nil {
		return path, ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "IBUS_ADDRESS="); ok {
			return path, strings.TrimSpace(value)
		}
	}
	return path, ""
}

func machineID() string {
	for _, name := range []string{"/var/lib/dbus/machine-id", "/etc/machine-id"} {
		if data, err := os.ReadFile(name); err == nil {
			if value := strings.TrimSpace(string(data)); value != "" {
				return value
			}
		}
	}
	return ""
}

// displayParts 与 Viva 同口径：":0.0" -> ("unix", "0")。
func displayParts(display string) (host, number string) {
	if display == "" {
		display = ":0.0"
	}
	host, rest, found := strings.Cut(display, ":")
	if !found {
		return "unix", "0"
	}
	if host == "" {
		host = "unix"
	}
	number, _, _ = strings.Cut(rest, ".")
	if number == "" {
		number = "0"
	}
	return host, number
}
