/*
Package common 提供 nps/npc 项目中广泛复用的通用工具函数。
本文件 util.go 收纳了与网络、字符串、端口、IO、环境变量等相关的辅助方法。

说明与约定
- 除非特别说明，以下函数均不会修改外部状态，且对输入做最小假设。
- 某些函数历史原因存在“非直观”行为，已在注释中特别标注“注意/陷阱”。
- 这些工具函数被服务端与客户端公用，注意跨平台（Windows/Linux/macOS）差异。

函数总览（按出现顺序）

  - GetHostByName(hostname string) string
    将域名解析为首个 IPv4 文本；若入参并非域名或解析失败，返回空串或原字符串。仅取 IPv4。

  - DomainCheck(domain string) bool
    使用正则判断字符串是否为“看起来像域名”的格式（可带 http/https 前缀，可带或不带结尾/）。不校验 IP。

  - CheckAuth(r *http.Request, user, passwd string) bool
    从 Authorization 或 Proxy-Authorization 头中解析 Basic 凭据，与给定 user/passwd 做等值比对。
    仅支持明文 Basic（base64 的“user:password”）。解析失败或不匹配返回 false。

  - GetBoolByStr(s string) bool / GetStrByBool(b bool) string
    字符串到布尔与布尔到字符串的简单映射："1"/"true" -> true；true -> "1"，false -> "0"。
    注意大小写与值范围有限，"True"、"TRUE" 不会被识别为真。

  - GetIntNoErrByStr(str string) int
    字符串转 int，忽略错误（出错返回 0）。

  - Getverifyval(vkey string) string
    对 vkey 做 MD5（crypt.Md5）。

  - ChangeHostAndHeader(r *http.Request, host, header, addr string, addOrigin bool)
    可修改 r.Host，按“\n”分行、每行“Key:Value”设置 Header；
    若 addOrigin 为真，将 X-Forwarded-For 与 X-Real-IP 设置为 addr（会和已有 X-Forwarded-For 级联）。
    注意：header 行内只以首个冒号分割，未做额外清洗与空格处理；host 仅影响 r.Host 不改 URL。

  - ReadAllFromFile(path string) ([]byte, error)
    读取文件全部内容。

  - FileExists(name string) bool
    判断文件或目录是否存在。注意：若 os.Stat 返回的并非“文件不存在”错误，一律视为存在。

  - TestTcpPort(port int) bool / TestUdpPort(port int) bool
    尝试在 0.0.0.0:port 上监听以判断端口是否可用。失败（被占用/权限不足）返回 false。

  - BinaryWrite(raw *bytes.Buffer, v ...string) / GetWriteStr(v ...string) []byte
    将多个字符串按固定分隔符 CONN_DATA_SEQ 拼接，并以小端 int32 写入“总长度 + 数据体”。
    用于网络粘包场景下的分帧。注意：CONN_DATA_SEQ 定义见 common/const.go。

  - InStrArr / InIntArr
    判断字符串/整型切片是否包含某值。

  - GetPorts(p string) []int
    解析端口列表字符串，如 "80,8080,1000-1005" -> [80 8080 1000 1001 1002 1003 1004 1005]。
    无效片段会被忽略。

  - IsPort(p string) bool
    判断字符串是否为“端口范围内的数字”。注意：历史实现允许到 65536（通常 65535 为上限）。

  - FormatAddress(s string) string
    若 s 不含冒号，视作端口并补齐为 "127.0.0.1:s"；否则原样返回。

  - GetIpByAddr(addr string) string / GetPortByAddr(addr string) int
    从 "ip:port" 文本中取出 ip 或 port。注意：不支持 IPv6（冒号冲突）。

  - CopyBuffer(dst io.Writer, src io.Reader, label ...string) (written int64, err error)
    使用复用缓冲区（CopyBuff）循环拷贝 src->dst，行为类似 io.Copy。
    返回写入字节数与最后一次 Read/Write 错误（EOF 时返回 EOF）。

  - GetLocalUdpAddr() (net.Conn, error)
    通过向 114.114.114.114:53 建立 UDP 连接来获取本地 UDP 绑定地址。
    注意/陷阱：函数返回的连接在返回前已被 Close，error 为 Close 的返回值。
    一般只用于读取 conn.LocalAddr()，而非继续读写。

  - ParseStr(str string) (string, error)
    将传入字符串作为 Go template 解析并执行，模板上下文为环境变量 map（GetEnvMap）。

  - GetEnvMap() map[string]string
    将进程环境变量导出为 map[KEY]VALUE。

  - TrimArr(arr []string) []string
    丢弃切片中的空字符串元素。

  - IsArrContains(arr []string, val string) bool / RemoveArrVal(arr []string, val string) []string
    简单的包含判断与移除首个匹配值。

  - BytesToNum(b []byte) int
    将每个字节的十进制表示依次拼接再转成整数，例如 [1 2 3] -> "123" -> 123。
    注意：并非按 256 进制权重计算，不适用于二进制数值解析。

  - GeSynctMapLen(m sync.Map) int
    通过 Range 统计 sync.Map 的元素数量。

  - GetExtFromPath(path string) string
    注意/陷阱：名称虽为“取扩展名”，但实现返回的是“文件名首段中的首个连续字母数字下划线片段”。
    例如 "abc.def" 返回 "abc"；更像是“文件名主体”的提取，非真正扩展名。

  - GetExternalIp() string / GetIntranetIp() (error, string)
    前者通过 http://myexternalip.com/raw 获取并缓存外网 IP（原样返回，可能含换行）。
    后者返回第一块非回环 IPv4 地址。注意/陷阱：当 net.InterfaceAddrs 出错时也返回 (nil, "")；
    若未找到地址则返回 (error, "")。调用方需留意该非常规错误语义。

  - IsPublicIP(IP net.IP) bool / GetServerIpByClientIp(clientIp net.IP) string
    判断是否公网 IPv4；根据客户端是公网/内网，选择返回外网 IP 或内网 IP。

  - PrintVersion()
    打印版本与核心版本，提示“客户端与服务端核心版本一致方可互通”。
*/
package common

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"ehang.io/nps/lib/version"

	"ehang.io/nps/lib/crypt"
)

// Get the corresponding IP address through domain name
func GetHostByName(hostname string) string {
	if !DomainCheck(hostname) {
		return hostname
	}
	ips, _ := net.LookupIP(hostname)
	if ips != nil {
		for _, v := range ips {
			if v.To4() != nil {
				return v.String()
			}
		}
	}
	return ""
}

// Check the legality of domain
func DomainCheck(domain string) bool {
	var match bool
	IsLine := "^((http://)|(https://))?([a-zA-Z0-9]([a-zA-Z0-9\\-]{0,61}[a-zA-Z0-9])?\\.)+[a-zA-Z]{2,6}(/)"
	NotLine := "^((http://)|(https://))?([a-zA-Z0-9]([a-zA-Z0-9\\-]{0,61}[a-zA-Z0-9])?\\.)+[a-zA-Z]{2,6}"
	match, _ = regexp.MatchString(IsLine, domain)
	if !match {
		match, _ = regexp.MatchString(NotLine, domain)
	}
	return match
}

// Check if the Request request is validated
func CheckAuth(r *http.Request, user, passwd string) bool {
	s := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(s) != 2 {
		s = strings.SplitN(r.Header.Get("Proxy-Authorization"), " ", 2)
		if len(s) != 2 {
			return false
		}
	}

	b, err := base64.StdEncoding.DecodeString(s[1])
	if err != nil {
		return false
	}

	pair := strings.SplitN(string(b), ":", 2)
	if len(pair) != 2 {
		return false
	}
	return pair[0] == user && pair[1] == passwd
}

// get bool by str
func GetBoolByStr(s string) bool {
	switch s {
	case "1", "true":
		return true
	}
	return false
}

// get str by bool
func GetStrByBool(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// int
func GetIntNoErrByStr(str string) int {
	i, _ := strconv.Atoi(strings.TrimSpace(str))
	return i
}

// Get verify value
func Getverifyval(vkey string) string {
	return crypt.Md5(vkey)
}

// Change headers and host of request
func ChangeHostAndHeader(r *http.Request, host string, header string, addr string, addOrigin bool) {
	if host != "" {
		r.Host = host
	}
	if header != "" {
		h := strings.Split(header, "\n")
		for _, v := range h {
			hd := strings.Split(v, ":")
			if len(hd) == 2 {
				r.Header.Set(hd[0], hd[1])
			}
		}
	}
	addr = strings.Split(addr, ":")[0]
	if prior, ok := r.Header["X-Forwarded-For"]; ok {
		addr = strings.Join(prior, ", ") + ", " + addr
	}
	if addOrigin {
		r.Header.Set("X-Forwarded-For", addr)
		r.Header.Set("X-Real-IP", addr)
	}
}

// Read file content by file path
func ReadAllFromFile(filePath string) ([]byte, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ioutil.ReadAll(f)
}

// FileExists reports whether the named file or directory exists.
func FileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// Judge whether the TCP port can open normally
func TestTcpPort(port int) bool {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{net.ParseIP("0.0.0.0"), port, ""})
	defer func() {
		if l != nil {
			l.Close()
		}
	}()
	if err != nil {
		return false
	}
	return true
}

// Judge whether the UDP port can open normally
func TestUdpPort(port int) bool {
	l, err := net.ListenUDP("udp", &net.UDPAddr{net.ParseIP("0.0.0.0"), port, ""})
	defer func() {
		if l != nil {
			l.Close()
		}
	}()
	if err != nil {
		return false
	}
	return true
}

// Write length and individual byte data
// Length prevents sticking
// # Characters are used to separate data
func BinaryWrite(raw *bytes.Buffer, v ...string) {
	b := GetWriteStr(v...)
	binary.Write(raw, binary.LittleEndian, int32(len(b)))
	binary.Write(raw, binary.LittleEndian, b)
}

// get seq str
func GetWriteStr(v ...string) []byte {
	buffer := new(bytes.Buffer)
	var l int32
	for _, v := range v {
		l += int32(len([]byte(v))) + int32(len([]byte(CONN_DATA_SEQ)))
		binary.Write(buffer, binary.LittleEndian, []byte(v))
		binary.Write(buffer, binary.LittleEndian, []byte(CONN_DATA_SEQ))
	}
	return buffer.Bytes()
}

// inArray str interface
func InStrArr(arr []string, val string) bool {
	for _, v := range arr {
		if v == val {
			return true
		}
	}
	return false
}

// inArray int interface
func InIntArr(arr []int, val int) bool {
	for _, v := range arr {
		if v == val {
			return true
		}
	}
	return false
}

// format ports str to a int array
func GetPorts(p string) []int {
	var ps []int
	arr := strings.Split(p, ",")
	for _, v := range arr {
		fw := strings.Split(v, "-")
		if len(fw) == 2 {
			if IsPort(fw[0]) && IsPort(fw[1]) {
				start, _ := strconv.Atoi(fw[0])
				end, _ := strconv.Atoi(fw[1])
				for i := start; i <= end; i++ {
					ps = append(ps, i)
				}
			} else {
				continue
			}
		} else if IsPort(v) {
			p, _ := strconv.Atoi(v)
			ps = append(ps, p)
		}
	}
	return ps
}

// is the string a port
func IsPort(p string) bool {
	pi, err := strconv.Atoi(p)
	if err != nil {
		return false
	}
	if pi > 65536 || pi < 1 {
		return false
	}
	return true
}

// if the s is just a port,return 127.0.0.1:s
func FormatAddress(s string) string {
	if strings.Contains(s, ":") {
		return s
	}
	return "127.0.0.1:" + s
}

// get address from the complete address
func GetIpByAddr(addr string) string {
	arr := strings.Split(addr, ":")
	return arr[0]
}

// get port from the complete address
func GetPortByAddr(addr string) int {
	arr := strings.Split(addr, ":")
	if len(arr) < 2 {
		return 0
	}
	p, err := strconv.Atoi(arr[1])
	if err != nil {
		return 0
	}
	return p
}

func CopyBuffer(dst io.Writer, src io.Reader, label ...string) (written int64, err error) {
	buf := CopyBuff.Get()
	defer CopyBuff.Put(buf)
	for {
		nr, er := src.Read(buf)
		//if len(pr)>0 && pr[0] && nr > 50 {
		//	logs.Warn(string(buf[:50]))
		//}
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			err = er
			break
		}
	}
	return written, err
}

// send this ip forget to get a local udp port
func GetLocalUdpAddr() (net.Conn, error) {
	tmpConn, err := net.Dial("udp", "114.114.114.114:53")
	if err != nil {
		return nil, err
	}
	return tmpConn, tmpConn.Close()
}

// parse template
func ParseStr(str string) (string, error) {
	tmp := template.New("npc")
	var err error
	w := new(bytes.Buffer)
	if tmp, err = tmp.Parse(str); err != nil {
		return "", err
	}
	if err = tmp.Execute(w, GetEnvMap()); err != nil {
		return "", err
	}
	return w.String(), nil
}

// get env
func GetEnvMap() map[string]string {
	m := make(map[string]string)
	environ := os.Environ()
	for i := range environ {
		tmp := strings.Split(environ[i], "=")
		if len(tmp) == 2 {
			m[tmp[0]] = tmp[1]
		}
	}
	return m
}

// throw the empty element of the string array
func TrimArr(arr []string) []string {
	newArr := make([]string, 0)
	for _, v := range arr {
		if v != "" {
			newArr = append(newArr, v)
		}
	}
	return newArr
}

func IsArrContains(arr []string, val string) bool {
	if arr == nil {
		return false
	}
	for _, v := range arr {
		if v == val {
			return true
		}
	}
	return false
}

// remove value from string array
func RemoveArrVal(arr []string, val string) []string {
	for k, v := range arr {
		if v == val {
			arr = append(arr[:k], arr[k+1:]...)
			return arr
		}
	}
	return arr
}

// convert bytes to num
func BytesToNum(b []byte) int {
	var str string
	for i := 0; i < len(b); i++ {
		str += strconv.Itoa(int(b[i]))
	}
	x, _ := strconv.Atoi(str)
	return int(x)
}

// get the length of the sync map
func GeSynctMapLen(m sync.Map) int {
	var c int
	m.Range(func(key, value interface{}) bool {
		c++
		return true
	})
	return c
}

func GetExtFromPath(path string) string {
	s := strings.Split(path, ".")
	re, err := regexp.Compile(`(\w+)`)
	if err != nil {
		return ""
	}
	return string(re.Find([]byte(s[0])))
}

var externalIp string

func GetExternalIp() string {
	if externalIp != "" {
		return externalIp
	}
	resp, err := http.Get("http://myexternalip.com/raw")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	content, _ := ioutil.ReadAll(resp.Body)
	externalIp = string(content)
	return externalIp
}

func GetIntranetIp() (error, string) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, ""
	}
	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return nil, ipnet.IP.To4().String()
			}
		}
	}
	return errors.New("get intranet ip error"), ""
}

func IsPublicIP(IP net.IP) bool {
	if IP.IsLoopback() || IP.IsLinkLocalMulticast() || IP.IsLinkLocalUnicast() {
		return false
	}
	if ip4 := IP.To4(); ip4 != nil {
		switch true {
		case ip4[0] == 10:
			return false
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return false
		case ip4[0] == 192 && ip4[1] == 168:
			return false
		default:
			return true
		}
	}
	return false
}

func GetServerIpByClientIp(clientIp net.IP) string {
	if IsPublicIP(clientIp) {
		return GetExternalIp()
	}
	_, ip := GetIntranetIp()
	return ip
}

func PrintVersion() {
	fmt.Printf("Version: %s\nCore version: %s\nSame core version of client and server can connect each other\n", version.VERSION, version.GetVersion())
}
