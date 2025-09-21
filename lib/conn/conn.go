package conn

import (
	"bufio"
	"bytes"
	"ehang.io/nps/lib/goroutine"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/astaxie/beego/logs"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/pmux"
	"ehang.io/nps/lib/rate"
	"github.com/xtaci/kcp-go"
)

// Conn 封装了 net.Conn，并提供一组与 NPS 协议配套的读写能力：
// - 支持长度前缀的报文读写（小端序 int32 长度 + 内容）
// - 支持读取 4 字节“标志”位
// - 提供 HTTP 请求探测（读取至 \r\n\r\n）
// - 提供连接存活控制、超时设置与地址包装
// - 提供基于 TLS/Snappy/Rate 的连接装饰器（配合 GetConn 使用）
// Rb 为临时回放缓冲区：当 Read 先前尚未完全消耗的字节会优先从 Rb 读取。
type Conn struct {
	Conn net.Conn
	Rb   []byte
}

// NewConn 创建一个 Conn 包装器，将底层 net.Conn 封装为具备协议化读写能力的连接。
func NewConn(conn net.Conn) *Conn {
	return &Conn{Conn: conn}
}

// readRequest 从连接持续读取数据到 buf，直到检测到 HTTP 报文头结束标志“\r\n\r\n”或缓冲区被填满。
// 返回值 n 为成功读取的字节数；err 在读取失败或缓冲区无法容纳完整请求时返回。
// 注意：buf 由调用者提供，函数不会扩容，n==cap(buf) 且未读到结束符时返回 io.ErrUnexpectedEOF。
func (s *Conn) readRequest(buf []byte) (n int, err error) {
	var rd int
	for {
		rd, err = s.Read(buf[n:])
		if err != nil {
			return
		}
		n += rd
		if n < 4 {
			continue
		}
		if string(buf[n-4:n]) == "\r\n\r\n" {
			return
		}
		// buf 容量已满仍未读到完整请求
		if n == cap(buf) {
			err = io.ErrUnexpectedEOF
			return
		}
	}
}

// GetHost 读取一段 HTTP 请求并解析 Host，返回：
// - method：HTTP 方法（注意：当前实现未显式赋值，建议从返回的 r.Method 获取）
// - address：主机地址（若未带端口，将根据 Host 所属协议推断 80 或 443）
// - rb：原始请求的字节切片（仅含已读取的请求头数据）
// - err：错误信息
// - r：解析后的 *http.Request 对象
// 用途：用于在 TCP 流上探测并解析 HTTP(S) 请求头（例如区分 80/443 或转发 CONNECT）。
func (s *Conn) GetHost() (method, address string, rb []byte, err error, r *http.Request) {
	var b [32 * 1024]byte
	var n int
	if n, err = s.readRequest(b[:]); err != nil {
		return
	}
	rb = b[:n]
	r, err = http.ReadRequest(bufio.NewReader(bytes.NewReader(rb)))
	if err != nil {
		return
	}
	hostPortURL, err := url.Parse(r.Host)
	if err != nil {
		// Host 无法解析为 URL 时，直接返回原始 Host
		address = r.Host
		err = nil
		return
	}
	// 如果 Opaque 为 443，则认为是 HTTPS，默认端口 443，否则默认 80
	if hostPortURL.Opaque == "443" {
		if strings.Index(r.Host, ":") == -1 {
			address = r.Host + ":443"
		} else {
			address = r.Host
		}
	} else {
		if strings.Index(r.Host, ":") == -1 {
			address = r.Host + ":80"
		} else {
			address = r.Host
		}
	}
	return
}

// GetShortLenContent 先读取 4 字节长度，再根据长度读取对应内容。
// 为安全起见，限制最大长度为 32KB；否则返回错误。
// 返回读取到的内容切片及错误信息。
func (s *Conn) GetShortLenContent() (b []byte, err error) {
	var l int
	if l, err = s.GetLen(); err != nil {
		return
	}
	if l < 0 || l > 32<<10 {
		err = errors.New("read length error")
		return
	}
	return s.GetShortContent(l)
}

// GetShortContent 按指定长度 l 从连接读取数据并返回一个新的切片。
// 使用 binary.Read 以小端序从连接读取原始字节到切片。
func (s *Conn) GetShortContent(l int) (b []byte, err error) {
	buf := make([]byte, l)
	return buf, binary.Read(s, binary.LittleEndian, &buf)
}

// ReadLen 读取指定长度的内容到调用方提供的 buf 中。
// 参数：
// - cLen：期望读取的字节数，必须大于 0 且不超过 buf 长度
// - buf：目标缓冲区
// 返回：实际读取的字节数（成功时等于 cLen）和错误。
func (s *Conn) ReadLen(cLen int, buf []byte) (int, error) {
	if cLen > len(buf) || cLen <= 0 {
		return 0, errors.New("长度错误" + strconv.Itoa(cLen))
	}
	if n, err := io.ReadFull(s, buf[:cLen]); err != nil || n != cLen {
		return n, errors.New("Error reading specified length " + err.Error())
	}
	return cLen, nil
}

// GetLen 从连接读取一个以小端序编码的 int32 长度，并以 int 返回。
func (s *Conn) GetLen() (int, error) {
	var l int32
	err := binary.Read(s, binary.LittleEndian, &l)
	return int(l), err
}

// WriteLenContent 将给定的 buf 按“长度(4B,LE) + 内容”的格式写入到底层连接。
func (s *Conn) WriteLenContent(buf []byte) (err error) {
	var b []byte
	if b, err = GetLenBytes(buf); err != nil {
		return
	}
	return binary.Write(s.Conn, binary.LittleEndian, b)
}

// ReadFlag 读取 4 字节标志位并以字符串形式返回。
// 注意：标志按原样读取，不做编码转换。
func (s *Conn) ReadFlag() (string, error) {
	buf := make([]byte, 4)
	return string(buf), binary.Read(s, binary.LittleEndian, &buf)
}

// SetAlive 将连接的 ReadDeadline 清空（即设置为零时间），用于保持长连接“常活”。
// 根据底层连接类型分别调用 kcp、TCP、pmux 的对应方法。
func (s *Conn) SetAlive(tp string) {
	switch s.Conn.(type) {
	case *kcp.UDPSession:
		s.Conn.(*kcp.UDPSession).SetReadDeadline(time.Time{})
	case *net.TCPConn:
		conn := s.Conn.(*net.TCPConn)
		conn.SetReadDeadline(time.Time{})
		//conn.SetKeepAlive(false)
		//conn.SetKeepAlivePeriod(time.Duration(2 * time.Second))
	case *pmux.PortConn:
		s.Conn.(*pmux.PortConn).SetReadDeadline(time.Time{})
	}
}

// SetReadDeadlineBySecond 设置连接的读超时，单位为秒（time.Duration 的秒数）。
// 等价于：SetReadDeadline(time.Now().Add(t * time.Second))。
func (s *Conn) SetReadDeadlineBySecond(t time.Duration) {
	switch s.Conn.(type) {
	case *kcp.UDPSession:
		s.Conn.(*kcp.UDPSession).SetReadDeadline(time.Now().Add(time.Duration(t) * time.Second))
	case *net.TCPConn:
		s.Conn.(*net.TCPConn).SetReadDeadline(time.Now().Add(time.Duration(t) * time.Second))
	case *pmux.PortConn:
		s.Conn.(*pmux.PortConn).SetReadDeadline(time.Now().Add(time.Duration(t) * time.Second))
	}
}

// GetLinkInfo 从连接读取一段长度前缀的 JSON，并反序列化为 *Link。
// 失败时返回错误。
func (s *Conn) GetLinkInfo() (lk *Link, err error) {
	err = s.getInfo(&lk)
	return
}

// SendHealthInfo 发送健康检查信息。
// 数据格式由 common.BinaryWrite(info, status) 负责拼接（通常以约定分隔符连接）。
// 返回写入字节数及错误。
func (s *Conn) SendHealthInfo(info, status string) (int, error) {
	raw := bytes.NewBuffer([]byte{})
	common.BinaryWrite(raw, info, status)
	return s.Write(raw.Bytes())
}

// GetHealthInfo 从连接读取健康检查信息。
// 过程：先读长度，再读内容，随后按 common.CONN_DATA_SEQ 拆分为 info 和 status。
// 返回：info 字符串、status 布尔值与可能的错误。
func (s *Conn) GetHealthInfo() (info string, status bool, err error) {
	var l int
	buf := common.BufPoolMax.Get().([]byte)
	defer common.PutBufPoolMax(buf)
	if l, err = s.GetLen(); err != nil {
		return
	} else if _, err = s.ReadLen(l, buf); err != nil {
		return
	} else {
		arr := strings.Split(string(buf[:l]), common.CONN_DATA_SEQ)
		if len(arr) >= 2 {
			return arr[0], common.GetBoolByStr(arr[1]), nil
		}
	}
	return "", false, errors.New("receive health info error")
}

// GetHostInfo 读取主机（Host）配置信息并做必要的默认项填充：
// - 自动分配 Id
// - 初始化 Flow
// - 置 NoStore=true（不写入持久化存储）
func (s *Conn) GetHostInfo() (h *file.Host, err error) {
	err = s.getInfo(&h)
	h.Id = int(file.GetDb().JsonDb.GetHostId())
	h.Flow = new(file.Flow)
	h.NoStore = true
	return
}

// GetConfigInfo 读取客户端（Client）配置并填充默认值：
// - NoStore=true, Status=true
// - 若 Flow 为空则初始化
// - NoDisplay=false（允许在界面展示）
func (s *Conn) GetConfigInfo() (c *file.Client, err error) {
	err = s.getInfo(&c)
	c.NoStore = true
	c.Status = true
	if c.Flow == nil {
		c.Flow = new(file.Flow)
	}
	c.NoDisplay = false
	return
}

// GetTaskInfo 读取隧道（Tunnel）配置并填充默认值：
// - 自动分配 Id
// - NoStore=true
// - 初始化 Flow
func (s *Conn) GetTaskInfo() (t *file.Tunnel, err error) {
	err = s.getInfo(&t)
	t.Id = int(file.GetDb().JsonDb.GetTaskId())
	t.NoStore = true
	t.Flow = new(file.Flow)
	return
}

// SendInfo 发送结构化信息。
// 线协议：可选 4 字节 flag（若 flag 非空） + 4 字节长度(LE) + JSON 内容。
// 参数 t 会被序列化为 JSON 后发送。
func (s *Conn) SendInfo(t interface{}, flag string) (int, error) {
	/*
		The task info is formed as follows:
		+----+-----+---------+
		|type| len | content |
		+----+---------------+
		| 4  |  4  |   ...   |
		+----+---------------+
	*/
	raw := bytes.NewBuffer([]byte{})
	if flag != "" {
		binary.Write(raw, binary.LittleEndian, []byte(flag))
	}
	b, err := json.Marshal(t)
	if err != nil {
		return 0, err
	}
	lenBytes, err := GetLenBytes(b)
	if err != nil {
		return 0, err
	}
	binary.Write(raw, binary.LittleEndian, lenBytes)
	return s.Write(raw.Bytes())
}

// getInfo 是读取长度前缀后的 JSON 并反序列化到 t 的内部工具函数。
// 注意：t 应该传入指向目标结构体的指针（例如 &obj）。
func (s *Conn) getInfo(t interface{}) (err error) {
	var l int
	buf := common.BufPoolMax.Get().([]byte)
	defer common.PutBufPoolMax(buf)
	if l, err = s.GetLen(); err != nil {
		return
	} else if _, err = s.ReadLen(l, buf); err != nil {
		return
	} else {
		json.Unmarshal(buf[:l], &t)
	}
	return
}

// Close 关闭底层连接。
func (s *Conn) Close() error {
	return s.Conn.Close()
}

// Write 将字节切片写入到底层连接，直接转发给 net.Conn.Write。
func (s *Conn) Write(b []byte) (int, error) {
	return s.Conn.Write(b)
}

// Read 从连接读取数据。
// 若内部回放缓冲 Rb 非空，则优先从 Rb 拷贝到 b；当 Rb 耗尽后，再从底层连接读取。
func (s *Conn) Read(b []byte) (n int, err error) {
	if s.Rb != nil {
		// Rb 非空，优先消费 Rb
		if len(s.Rb) > 0 {
			n = copy(b, s.Rb)
			s.Rb = s.Rb[n:]
			return
		}
		s.Rb = nil
	}
	return s.Conn.Read(b)
}

// WriteClose 向对端写入“关闭”标志（common.RES_CLOSE）。
func (s *Conn) WriteClose() (int, error) {
	return s.Write([]byte(common.RES_CLOSE))
}

// WriteMain 向对端写入“主连接”标志（common.WORK_MAIN）。
func (s *Conn) WriteMain() (int, error) {
	return s.Write([]byte(common.WORK_MAIN))
}

// WriteConfig 向对端写入“配置连接”标志（common.WORK_CONFIG）。
func (s *Conn) WriteConfig() (int, error) {
	return s.Write([]byte(common.WORK_CONFIG))
}

// WriteChan 向对端写入“通道连接”标志（common.WORK_CHAN）。
func (s *Conn) WriteChan() (int, error) {
	return s.Write([]byte(common.WORK_CHAN))
}

// GetAddStatus 读取一个布尔值，表示“添加任务/主机”的结果状态。
func (s *Conn) GetAddStatus() (b bool) {
	binary.Read(s.Conn, binary.LittleEndian, &b)
	return
}

// WriteAddOk 写入“添加成功”的布尔状态。
func (s *Conn) WriteAddOk() error {
	return binary.Write(s.Conn, binary.LittleEndian, true)
}

// WriteAddFail 写入“添加失败”的布尔状态，并在返回前关闭连接。
func (s *Conn) WriteAddFail() error {
	defer s.Close()
	return binary.Write(s.Conn, binary.LittleEndian, false)
}

// LocalAddr 返回本地地址（包装 net.Conn.LocalAddr）。
func (s *Conn) LocalAddr() net.Addr {
	return s.Conn.LocalAddr()
}

// RemoteAddr 返回远端地址（包装 net.Conn.RemoteAddr）。
func (s *Conn) RemoteAddr() net.Addr {
	return s.Conn.RemoteAddr()
}

// SetDeadline 设置读/写截止时间（包装 net.Conn.SetDeadline）。
func (s *Conn) SetDeadline(t time.Time) error {
	return s.Conn.SetDeadline(t)
}

// SetWriteDeadline 设置写截止时间（包装 net.Conn.SetWriteDeadline）。
func (s *Conn) SetWriteDeadline(t time.Time) error {
	return s.Conn.SetWriteDeadline(t)
}

// SetReadDeadline 设置读截止时间（包装 net.Conn.SetReadDeadline）。
func (s *Conn) SetReadDeadline(t time.Time) error {
	return s.Conn.SetReadDeadline(t)
}

// GetLenBytes 将任意内容 buf 封装为“长度(4B,LE) + 内容”的字节序列并返回。
// 常用于与对端通信时的长度前缀编码写入。
func GetLenBytes(buf []byte) (b []byte, err error) {
	raw := bytes.NewBuffer([]byte{})
	if err = binary.Write(raw, binary.LittleEndian, int32(len(buf))); err != nil {
		return
	}
	if err = binary.Write(raw, binary.LittleEndian, buf); err != nil {
		return
	}
	b = raw.Bytes()
	return
}

// SetUdpSession 对 KCP 会话进行参数配置，以获得更优的流式传输体验：
// - 启用流模式
// - 调整窗口大小、读写缓冲
// - 设置 NoDelay/Mtu/ACK 等参数
func SetUdpSession(sess *kcp.UDPSession) {
	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(64 * 1024)
	sess.SetWriteBuffer(64 * 1024)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetMtu(1600)
	sess.SetACKNoDelay(true)
	sess.SetWriteDelay(false)
}

// CopyWaitGroup 建立 conn1 与 conn2 之间的双向转发（中间可叠加加密/压缩/限速）。
// 行为：
// - 使用 GetConn 对 conn1 进行包装（按 crypt/snappy/rate/isServer）
// - 若 rb 不为空，先将 rb 写入对端（通常作为协议预先数据）
// - 使用 goroutine.CopyConnsPool 进行高效数据转发，并等待转发完成
// - 将统计流量累加到 flow（由 goroutine.NewConns 内部处理）
// 异常：若 Invoke 返回错误，会记录日志。
func CopyWaitGroup(conn1, conn2 net.Conn, crypt bool, snappy bool, rate *rate.Rate, flow *file.Flow, isServer bool, rb []byte) {
	//var in, out int64
	//var wg sync.WaitGroup
	connHandle := GetConn(conn1, crypt, snappy, rate, isServer)
	if rb != nil {
		connHandle.Write(rb)
	}
	//go func(in *int64) {
	//	wg.Add(1)
	//	*in, _ = common.CopyBuffer(connHandle, conn2)
	//	connHandle.Close()
	//	conn2.Close()
	//	wg.Done()
	//}(&in)
	//out, _ = common.CopyBuffer(conn2, connHandle)
	//connHandle.Close()
	//conn2.Close()
	//wg.Wait()
	//if flow != nil {
	//	flow.Add(in, out)
	//}
	wg := new(sync.WaitGroup)
	wg.Add(1)
	err := goroutine.CopyConnsPool.Invoke(goroutine.NewConns(connHandle, conn2, flow, wg))
	wg.Wait()
	if err != nil {
		logs.Error(err)
	}
}

// GetConn 根据标志返回一个可读写的连接包装：
// - cpt=true：启用 TLS 加密（区分服务端/客户端）
// - snappy=true：启用 Snappy 压缩
// - 始终叠加限速包装 rate.Rate（若提供）
// 返回的连接实现 io.ReadWriteCloser 接口。
func GetConn(conn net.Conn, cpt, snappy bool, rt *rate.Rate, isServer bool) io.ReadWriteCloser {
	if cpt {
		if isServer {
			return rate.NewRateConn(crypt.NewTlsServerConn(conn), rt)
		}
		return rate.NewRateConn(crypt.NewTlsClientConn(conn), rt)
	} else if snappy {
		return rate.NewRateConn(NewSnappyConn(conn), rt)
	}
	return rate.NewRateConn(conn, rt)
}

// LenConn 是对 io.Writer 的简单包装，用于统计写出的总字节数。
// 每次 Write 成功后，会将写入字节数累加到 Len 字段。
type LenConn struct {
	conn io.Writer
	Len  int
}

// NewLenConn 创建一个带字节数统计的 Writer 包装器。
func NewLenConn(conn io.Writer) *LenConn {
	return &LenConn{conn: conn}
}

// Write 实现 io.Writer 接口，同时将写入的字节数累加到 Len。
func (c *LenConn) Write(p []byte) (n int, err error) {
	n, err = c.conn.Write(p)
	c.Len += n
	return
}
