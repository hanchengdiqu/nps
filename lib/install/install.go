// Package install 提供 npx/npc 的安装、更新以及相关文件复制的工具函数。
// 主要职责：
// 1) 下载 GitHub 最新发行版并解压
// 2) 将二进制与静态资源复制到系统安装目录
// 3) 提供安装与更新入口（InstallNps/InstallNpc/UpdateNps/UpdateNpc）
// 4) 辅助的目录创建、文件复制与权限设置
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"ehang.io/nps/lib/common"
	"github.com/c4milo/unpackit"
)

// Keep it in sync with the template from service_sysv_linux.go file
// Use "ps | grep -v grep | grep $(get_pid)" because "ps PID" may not work on OpenWrt
const SysvScript = `#!/bin/sh
# For RedHat and cousins:
# chkconfig: - 99 01
# description: {{.Description}}
# processname: {{.Path}}
### BEGIN INIT INFO
# Provides:          {{.Path}}
# Required-Start:
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: {{.DisplayName}}
# Description:       {{.Description}}
### END INIT INFO
cmd="{{.Path}}{{range .Arguments}} {{.|cmd}}{{end}}"
name=$(basename $(readlink -f $0))
pid_file="/var/run/$name.pid"
stdout_log="/var/log/$name.log"
stderr_log="/var/log/$name.err"
[ -e /etc/sysconfig/$name ] && . /etc/sysconfig/$name
get_pid() {
    cat "$pid_file"
}
is_running() {
    [ -f "$pid_file" ] && ps | grep -v grep | grep $(get_pid) > /dev/null 2>&1
}
case "$1" in
    start)
        if is_running; then
            echo "Already started"
        else
            echo "Starting $name"
            {{if .WorkingDirectory}}cd '{{.WorkingDirectory}}'{{end}}
            $cmd >> "$stdout_log" 2>> "$stderr_log" &
            echo $! > "$pid_file"
            if ! is_running; then
                echo "Unable to start, see $stdout_log and $stderr_log"
                exit 1
            fi
        fi
    ;;
    stop)
        if is_running; then
            echo -n "Stopping $name.."
            kill $(get_pid)
            for i in $(seq 1 10)
            do
                if ! is_running; then
                    break
                fi
                echo -n "."
                sleep 1
            done
            echo
            if is_running; then
                echo "Not stopped; may still be shutting down or shutdown may have failed"
                exit 1
            else
                echo "Stopped"
                if [ -f "$pid_file" ]; then
                    rm "$pid_file"
                fi
            fi
        else
            echo "Not running"
        fi
    ;;
    restart)
        $0 stop
        if is_running; then
            echo "Unable to stop, will not attempt to start"
            exit 1
        fi
        $0 start
    ;;
    status)
        if is_running; then
            echo "Running"
        else
            echo "Stopped"
            exit 1
        fi
    ;;
    *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
    ;;
esac
exit 0
`

// SystemdScript 为 systemd 服务单元模板，安装到 Linux systemd 环境下时使用。
// 模板变量由生成器在运行时渲染，确保二进制路径、工作目录、用户、日志等参数正确。
const SystemdScript = `[Unit]
Description={{.Description}}
ConditionFileIsExecutable={{.Path|cmdEscape}}
{{range $i, $dep := .Dependencies}} 
{{$dep}} {{end}}
[Service]
LimitNOFILE=65536
StartLimitInterval=5
StartLimitBurst=10
ExecStart={{.Path|cmdEscape}}{{range .Arguments}} {{.|cmd}}{{end}}
{{if .ChRoot}}RootDirectory={{.ChRoot|cmd}}{{end}}
{{if .WorkingDirectory}}WorkingDirectory={{.WorkingDirectory|cmdEscape}}{{end}}
{{if .UserName}}User={{.UserName}}{{end}}
{{if .ReloadSignal}}ExecReload=/bin/kill -{{.ReloadSignal}} "$MAINPID"{{end}}
{{if .PIDFile}}PIDFile={{.PIDFile|cmd}}{{end}}
{{if and .LogOutput .HasOutputFileSupport -}}
StandardOutput=file:/var/log/{{.Name}}.out
StandardError=file:/var/log/{{.Name}}.err
{{- end}}
Restart=always
RestartSec=120
[Install]
WantedBy=multi-user.target
`

// UpdateNps 更新 nps（服务端）到 GitHub 最新发行版：
// 1) 获取最新版本号并下载压缩包
// 2) 解压后复制二进制及静态资源到系统安装目录
// 3) 提示用户重启生效
func UpdateNps() {
	destPath := downloadLatest("server")
	//复制文件到对应目录
	bin := programBaseName()
	copyStaticFile(destPath, bin, true)
	fmt.Println("Update completed, please restart")
}

// UpdateNpc 更新 npc（客户端）到 GitHub 最新发行版：
// 1) 获取最新版本号并下载压缩包
// 2) 解压后复制二进制到系统安装目录
// 3) 提示用户重启生效
func UpdateNpc() {
	destPath := downloadLatest("client")
	//复制文件到对应目录
	bin := programBaseName()
	copyStaticFile(destPath, bin, false)
	fmt.Println("Update completed, please restart")
}

// release 用于解析 GitHub Releases 接口返回的版本信息。
type release struct {
	TagName string `json:"tag_name"`
}

// downloadLatest 下载并解压指定类型（server/client）的最新发行版，并返回解压后的根目录。
// - bin 为 "server" 或 "client"，用于拼接包名与后续路径处理
// - 自动根据当前系统架构选择 tar.gz 包
// - 对 server 会去掉 web、views 目录后缀；对 client 会去掉 conf 目录后缀
func downloadLatest(bin string) string {
	// get version
	data, err := http.Get("https://api.github.com/repos/ehang-io/nps/releases/latest")
	if err != nil {
		log.Fatal(err.Error())
	}
	b, err := ioutil.ReadAll(data.Body)
	if err != nil {
		log.Fatal(err)
	}
	rl := new(release)
	json.Unmarshal(b, &rl)
	version := rl.TagName
	fmt.Println("the latest version is", version)
	filename := runtime.GOOS + "_" + runtime.GOARCH + "_" + bin + ".tar.gz"
	// download latest package
	downloadUrl := fmt.Sprintf("https://ehang.io/nps/releases/download/%s/%s", version, filename)
	fmt.Println("download package from ", downloadUrl)
	resp, err := http.Get(downloadUrl)
	if err != nil {
		log.Fatal(err.Error())
	}
	destPath, err := unpackit.Unpack(resp.Body, "")
	if err != nil {
		log.Fatal(err)
	}
	if bin == "server" {
		destPath = strings.Replace(destPath, "/web", "", -1)
		destPath = strings.Replace(destPath, `\web`, "", -1)
		destPath = strings.Replace(destPath, "/views", "", -1)
		destPath = strings.Replace(destPath, `\views`, "", -1)
	} else {
		destPath = strings.Replace(destPath, `\conf`, "", -1)
		destPath = strings.Replace(destPath, "/conf", "", -1)
	}
	return destPath
}

// copyStaticFile 将解压目录中的二进制与静态资源复制到系统安装目录，并返回最终可执行文件路径。
// 参数：
// - srcPath: 解压后的源目录（包含可执行文件及 web 资源等）
// - bin:     目标可执行文件名（不带扩展名），通常为当前程序名
// - isServer: 是否为服务端安装。为 true 时将强制复制 web 静态资源，缺失则失败
// 行为说明：
// - 服务端（isServer=true）：会复制 web/views 与 web/static；若缺失任一目录则失败中止
// - 非 Windows 平台：优先复制到 /usr/bin，若失败则退回 /usr/local/bin；并在对应目录生成 "*-update"
// - Windows 平台：复制 exe 到应用目录，同时生成 "*-update.exe"
// - 返回最终可执行文件路径
func copyStaticFile(srcPath, bin string, isServer bool) string {
	path := common.GetInstallPath()

	// 服务端：严格要求 web 资源存在并复制
	if isServer {
		viewsSrc := filepath.Join(srcPath, "web", "views")
		staticSrc := filepath.Join(srcPath, "web", "static")
		if !common.FileExists(viewsSrc) || !common.FileExists(staticSrc) {
			log.Fatalln("web resources not found, install/update aborted")
		}
		if err := CopyDir(viewsSrc, filepath.Join(path, "web", "views")); err != nil {
			log.Fatalln(err)
		}
		chMod(filepath.Join(path, "web", "views"), 0766)
		if err := CopyDir(staticSrc, filepath.Join(path, "web", "static")); err != nil {
			log.Fatalln(err)
		}
		chMod(filepath.Join(path, "web", "static"), 0766)
	}

	binPath, _ := filepath.Abs(os.Args[0])

	if !common.IsWindows() {
		if _, err := copyFile(filepath.Join(srcPath, bin), "/usr/bin/"+bin); err != nil {
			if _, err := copyFile(filepath.Join(srcPath, bin), "/usr/local/bin/"+bin); err != nil {
				log.Fatalln(err)
			} else {
				copyFile(filepath.Join(srcPath, bin), "/usr/local/bin/"+bin+"-update")
				chMod("/usr/local/bin/"+bin+"-update", 0755)
				binPath = "/usr/local/bin/" + bin
			}
		} else {
			copyFile(filepath.Join(srcPath, bin), "/usr/bin/"+bin+"-update")
			chMod("/usr/bin/"+bin+"-update", 0755)
			binPath = "/usr/bin/" + bin
		}
	} else {
		copyFile(filepath.Join(srcPath, bin+".exe"), filepath.Join(common.GetAppPath(), bin+"-update.exe"))
		copyFile(filepath.Join(srcPath, bin+".exe"), filepath.Join(common.GetAppPath(), bin+".exe"))
	}

	chMod(binPath, 0755)
	return binPath
}

// InstallNpc 创建安装目录（如不存在），并将当前应用目录中的 npc 二进制复制到系统安装目录。
// 注意：不会覆盖或生成配置文件，仅负责二进制落地。
func InstallNpc() {
	path := common.GetInstallPath()
	if !common.FileExists(path) {
		err := os.Mkdir(path, 0755)
		if err != nil {
			log.Fatal(err)
		}
	}
	bin := programBaseName()
	copyStaticFile(common.GetAppPath(), bin, false)
}

// InstallNps 安装 nps（服务端）：
// - 创建必要目录（conf、web/static、web/views），首次安装时复制默认配置
// - 复制二进制与静态资源到系统安装目录
// - 打印使用提示，并设置日志目录权限
// 返回最终二进制路径以便后续使用
func InstallNps() string {
	path := common.GetInstallPath()
	if common.FileExists(path) {
		MkidrDirAll(path, "web/static", "web/views")
	} else {
		MkidrDirAll(path, "conf", "web/static", "web/views")
		// not copy config if the config file is exist
		if err := CopyDir(filepath.Join(common.GetAppPath(), "conf"), filepath.Join(path, "conf")); err != nil {
			log.Fatalln(err)
		}
		if common.FileExists(filepath.Join(path, "conf")) {
			chMod(filepath.Join(path, "conf"), 0766)
		} else {
			log.Fatalln("没有找到配置文件，故此没有设置配置文件权限")
		}

	}
	bin := programBaseName()
	binPath := copyStaticFile(common.GetAppPath(), bin, true)
	log.Println("install ok!")
	log.Println("Static files and configuration files in the current directory will be useless")
	log.Println("The new configuration file is located in", path, "you can edit them")
	if !common.IsWindows() {
		log.Println("You can start with:")
		log.Println(bin, "start|stop|restart|uninstall|update or "+bin+"-update update")
		log.Println("anywhere!")
	} else {
		log.Println("You can copy executable files to any directory and start working with:")
		log.Println(bin+".exe", "start|stop|restart|uninstall|update or "+bin+"-update.exe update")
		log.Println("now!")
	}
	chMod(common.GetLogPath(), 0777)
	return binPath
}

// InstallNps 安装 nps（服务端）：
// - 创建必要目录（conf、web/static、web/views），首次安装时复制默认配置
// - 复制二进制与静态资源到系统安装目录
// - 打印使用提示，并设置日志目录权限
// 返回最终二进制路径以便后续使用
func ReInstallNps() string {
	path := common.GetInstallPath()
	MkidrDirAll(path, "conf", "web/static", "web/views")
	// not copy config if the config file is exist
	if err := CopyDir(filepath.Join(common.GetAppPath(), "conf"), filepath.Join(path, "conf")); err != nil {
		log.Fatalln(err)
	}
	if common.FileExists(filepath.Join(path, "conf")) {
		chMod(filepath.Join(path, "conf"), 0766)
	} else {
		log.Fatalln("没有找到配置文件，故此没有设置配置文件权限")
	}
	bin := programBaseName()
	binPath := copyStaticFile(common.GetAppPath(), bin, true)
	log.Println("install ok!")
	log.Println("Static files and configuration files in the current directory will be useless")
	log.Println("The new configuration file is located in", path, "you can edit them")
	if !common.IsWindows() {
		log.Println("You can start with:")
		log.Println(bin, "start|stop|restart|uninstall|update|install --force or "+bin+"-update update")
		log.Println("anywhere!")
	} else {
		log.Println("You can copy executable files to any directory and start working with:")
		log.Println(bin+".exe", "start|stop|restart|uninstall|update|install --force or "+bin+"-update.exe update")
		log.Println("now!")
	}
	chMod(common.GetLogPath(), 0777)
	return binPath
}

// MkidrDirAll 在 path 下递归创建多个子目录（如果不存在），用于初始化安装目录结构。
func MkidrDirAll(path string, v ...string) {
	for _, item := range v {
		if err := os.MkdirAll(filepath.Join(path, item), 0755); err != nil {
			log.Fatalf("Failed to create directory %s error:%s", path, err.Error())
		}
	}
}

// CopyDir 复制 srcPath 目录下的所有文件到 destPath（保持相对路径结构）：
// - 要求 srcPath 与 destPath 都必须存在且为目录
// - 非 Windows 系统下为复制出的文件设置 0766 权限
// - 仅复制文件，子目录会在需要时自动创建
func CopyDir(srcPath string, destPath string) error {
	//检测目录正确性
	if srcInfo, err := os.Stat(srcPath); err != nil {
		fmt.Println(err.Error())
		return err
	} else {
		if !srcInfo.IsDir() {
			e := errors.New("SrcPath is not the right directory!")
			return e
		}
	}
	if destInfo, err := os.Stat(destPath); err != nil {
		return err
	} else {
		if !destInfo.IsDir() {
			e := errors.New("DestInfo is not the right directory!")
			return e
		}
	}
	err := filepath.Walk(srcPath, func(path string, f os.FileInfo, err error) error {
		if f == nil {
			return err
		}
		if !f.IsDir() {
			destNewPath := strings.Replace(path, srcPath, destPath, -1)
			log.Println("copy file ::" + path + " to " + destNewPath)
			copyFile(path, destNewPath)
			if !common.IsWindows() {
				chMod(destNewPath, 0766)
			}
		}
		return nil
	})
	return err
}

// 生成目录并拷贝文件
// copyFile 复制单个文件到目标路径：
// - 会在复制前按需创建目标路径中的父级目录
// - 返回写入的字节数与可能的错误
// 注意：权限设置由调用方或上层逻辑负责
func copyFile(src, dest string) (w int64, err error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return
	}
	defer srcFile.Close()
	//分割path目录
	destSplitPathDirs := strings.Split(dest, string(filepath.Separator))

	//检测时候存在目录
	destSplitPath := ""
	for index, dir := range destSplitPathDirs {
		if index < len(destSplitPathDirs)-1 {
			destSplitPath = destSplitPath + dir + string(filepath.Separator)
			b, _ := pathExists(destSplitPath)
			if b == false {
				log.Println("mkdir:" + destSplitPath)
				//创建目录
				err := os.Mkdir(destSplitPath, os.ModePerm)
				if err != nil {
					log.Fatalln(err)
				}
			}
		}
	}
	dstFile, err := os.Create(dest)
	if err != nil {
		return
	}
	defer dstFile.Close()

	return io.Copy(dstFile, srcFile)
}

// 检测文件夹路径时候存在
// pathExists 判断给定路径是否存在。
// 返回：存在与否的布尔值以及可能的错误（非不存在导致的错误）。
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// chMod 在非 Windows 平台设置文件/目录权限，Windows 下跳过以避免无效调用。
func chMod(name string, mode os.FileMode) {
	if !common.IsWindows() {
		os.Chmod(name, mode)
	}
}

// programBaseName 返回当前运行程序的基名（去除 .exe 扩展名）
func programBaseName() string {
	exe, err := os.Executable()
	if err != nil {
		// 退回到 Args[0]
		base := filepath.Base(os.Args[0])
		if strings.HasSuffix(strings.ToLower(base), ".exe") {
			return strings.TrimSuffix(base, ".exe")
		}
		return base
	}
	base := filepath.Base(exe)
	if strings.HasSuffix(strings.ToLower(base), ".exe") {
		return strings.TrimSuffix(base, ".exe")
	}
	return base
}
