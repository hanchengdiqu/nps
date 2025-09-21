// Package common 提供与运行环境相关的通用工具函数。
//
// 本文件（run.go）聚焦于在不同操作系统下的路径选择与判定，包括：
// 1) 获取“安装目录”和“程序目录”，并据此确定实际运行（配置）目录；
// 2) 判断是否为 Windows 系统；
// 3) 统一提供日志、配置、临时文件等路径的获取方法。
//
// 注意：
// - 非 Windows 系统下，/etc、/var/log、/tmp 等目录通常需要管理员权限才能写入；
// - Windows 下通常使用程序所在目录以规避权限问题；
// - 这里的 npx（服务端）与 npc（客户端）为项目中组件的约定命名。
package common

import (
	"os"
	"path/filepath"
	"runtime"
)

// GetRunPath 返回当前程序选用的“运行（配置）目录”。
//
// 选择逻辑：
// - 若“安装目录”（GetInstallPath）存在，则优先使用安装目录；
// - 否则回退到“程序目录”（GetAppPath，即可执行文件所在目录）。
//
// 典型用途：作为查找 conf 目录或默认资源文件的根路径。
func GetRunPath() string {
	var path string
	// 先尝试使用预设的安装目录，仅当该目录“实际存在”时才采用。
	if path = GetInstallPath(); !FileExists(path) {
		// 安装目录不存在时，回退到可执行文件所在目录。
		return GetAppPath()
	}
	return path
}

// GetInstallPath 返回不同系统的“预期安装目录”。
// 注意：该函数仅返回路径字符串，并不保证目录真实存在。
// - Windows: C:\\Program Files\\npx
// - 非 Windows: /etc/npx
func GetInstallPath() string {
	var path string
	if IsWindows() {
		path = `C:\\Program Files\\npx`
	} else {
		path = "/etc/npx"
	}
	return path
}

// GetAppPath 返回“可执行文件所在目录”的绝对路径。
//
// 实现说明：
// - 首选使用 filepath.Abs(filepath.Dir(os.Args[0])) 进行绝对化；
// - 若转换失败（极少数情况下），则退回 os.Args[0] 原值（可能为相对路径）。
//
// 注意：若通过符号链接或 PATH 调用程序，os.Args[0] 的解析结果可能受运行环境影响。
func GetAppPath() string {
	if path, err := filepath.Abs(filepath.Dir(os.Args[0])); err == nil {
		return path
	}
	return os.Args[0]
}

// IsWindows 判断当前程序是否运行在 Windows 系统上。
func IsWindows() bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return false
}

// GetLogPath 返回服务端日志文件 npx.log 的完整路径。
// - Windows: 放在程序目录（避免权限问题）。
// - 非 Windows: /var/log/npx.log（写入通常需要管理员权限）。
func GetLogPath() string {
	var path string
	if IsWindows() {
		path = filepath.Join(GetAppPath(), "npx.log")
	} else {
		path = "/var/log/npx.log"
	}
	return path
}

// GetNpcLogPath 返回客户端日志文件 npc.log 的完整路径。
// - Windows: 放在程序目录。
// - 非 Windows: /var/log/npc.log。
func GetNpcLogPath() string {
	var path string
	if IsWindows() {
		path = filepath.Join(GetAppPath(), "npc.log")
	} else {
		path = "/var/log/npc.log"
	}
	return path
}

// GetTmpPath 返回运行期临时文件（如 pid 文件）所在目录。
// - Windows: 使用程序目录以减少权限限制带来的问题。
// - 非 Windows: 使用 /tmp。
func GetTmpPath() string {
	var path string
	if IsWindows() {
		path = GetAppPath()
	} else {
		path = "/tmp"
	}
	return path
}

// GetConfigPath 返回默认配置文件路径。
// - Windows: 绝对路径（程序目录下的 conf/npc.conf）。
// - 非 Windows: 返回相对路径 "conf/npc.conf"。
//
// 建议在非 Windows 下与 GetRunPath 结合以得到更稳妥的绝对路径，例如：
//   filepath.Join(GetRunPath(), GetConfigPath())
func GetConfigPath() string {
	var path string
	if IsWindows() {
		path = filepath.Join(GetAppPath(), "conf/npc.conf")
	} else {
		path = "conf/npc.conf"
	}
	return path
}
