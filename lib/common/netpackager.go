// 本文件实现了 SOCKS5 UDP 报文的编解码。
//
// 主要能力：
// 1) Addr 地址字段的编码与解码，兼容 IPv4/IPv6/域名三种 ATYP（参见 RFC 1928）。
// 2) UDPHeader 的写入（Rsv、Frag 与目标地址）。
// 3) UDPDatagram 的读取与写入（将 Header 与数据段拼装/拆解）。
//
// 特别说明：
// - 标准 SOCKS5 UDP 数据报中，保留字段 Rsv（2 字节）恒为 0。
//   在本项目中，当通过 TCP 隧道承载 UDP 时，会复用 Rsv 来携带“数据段长度”。
//   这样可以在流式连接（如 TCP）上精确读取恰好长度的数据，避免多读/丢弃。
// - 本文件使用 BufPoolUdp（在其他文件中定义的字节缓冲池）临时复用切片，减少临时内存分配与 GC 压力。
//   该缓冲区的容量需要足以承载一个完整的 UDP 报文（头部 + 数据）。
package common

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"strconv"
)

// NetPackager 抽象了一个通用的“打包/解包”接口。
// 在本文件中我们并未直接使用它，但它描述了 Pack/UnPack 的通用模式，
// 便于调用方针对不同载体（TCP/UDP/文件等）的统一处理。
//
// 参数含义：
// - writer/reader：数据的写入/读取端。
// 返回：错误信息（若有）。
//
// 注意：具体实现需自行定义数据格式与边界，本文件中的 UDPDatagram 便是一个实现示例。
type NetPackager interface {
	Pack(writer io.Writer) (err error)
	UnPack(reader io.Reader) (err error)
}

// ATYP 常量取值，兼容 SOCKS5 中地址类型枚举。
const (
	ipV4       = 1 // IPv4 地址
	domainName = 3 // 域名
	ipV6       = 4 // IPv6 地址
)

// UDPHeader 表示 SOCKS5 UDP 报文的头部。
// 其二进制布局为：
//   +----+----+----+----+....+----+----+
//   |RSV |RSV |FRAG|ATYP|ADDR|PORT|    |
//   +----+----+----+----+....+----+----+
//   0-1: RSV(2字节)   2: FRAG(1字节)  3: ATYP(1字节)
//   接着是 DST.ADDR 与 DST.PORT（大端序）。
//
// 字段说明：
// - Rsv:  标准中应为 0；当 UDP 经由 TCP 传输时，本项目将其复用为“数据段长度”。
// - Frag: 分片标识，标准 SOCKS5 一般为 0，不使用分片。
// - Addr: 目标地址（包含 ATYP、ADDR、PORT）。
type UDPHeader struct {
	Rsv  uint16 // 保留字段；扩展场景下表示数据长度
	Frag uint8  // 分片标志（一般为 0）
	Addr *Addr  // 目的地址（ATYP + DST.ADDR + DST.PORT）
}

// NewUDPHeader 创建一个 UDPHeader。
// rsv: 保留字段或扩展场景下的数据长度；frag: 分片标志；addr: 目标地址。
func NewUDPHeader(rsv uint16, frag uint8, addr *Addr) *UDPHeader {
	return &UDPHeader{
		Rsv:  rsv,
		Frag: frag,
		Addr: addr,
	}
}

// Addr 表示 SOCKS5 地址字段，由三部分组成：ATYP、ADDR 与 PORT。
// - Type: ATYP 取值（1=IPv4, 3=域名, 4=IPv6）。
// - Host: 主机名或 IP 字符串表示。
// - Port: 端口（大端序编码/解码时使用）。
type Addr struct {
	Type uint8
	Host string
	Port uint16
}

// String 以 host:port 形式返回地址字符串。
func (addr *Addr) String() string {
	return net.JoinHostPort(addr.Host, strconv.Itoa(int(addr.Port)))
}

// Decode 从二进制切片中解析出 Addr。
// 输入切片 b 的布局应为：[ATYP | ADDR | PORT]，其中：
// - 若 ATYP=IPv4，ADDR 为 4 字节；
// - 若 ATYP=IPv6，ADDR 为 16 字节；
// - 若 ATYP=域名，ADDR 为 [1字节长度 | 变长字节]；
// PORT 为 2 字节大端序。
// 若解析失败（未知 ATYP），返回错误。
func (addr *Addr) Decode(b []byte) error {
	addr.Type = b[0]
	pos := 1
	switch addr.Type {
	case ipV4:
		addr.Host = net.IP(b[pos : pos+net.IPv4len]).String()
		pos += net.IPv4len
	case ipV6:
		addr.Host = net.IP(b[pos : pos+net.IPv6len]).String()
		pos += net.IPv6len
	case domainName:
		addrlen := int(b[pos])
		pos++
		addr.Host = string(b[pos : pos+addrlen])
		pos += addrlen
	default:
		return errors.New("decode error")
	}

	addr.Port = binary.BigEndian.Uint16(b[pos:])

	return nil
}

// Encode 将 Addr 编码到给定缓冲区 b 中，返回写入的字节数。
// b 的预期内容将是：[ATYP | ADDR | PORT]。
// - 若 ATYP=IPv4/IPv6，会尝试解析 Host 为 IP；若解析失败，则写入全 0 地址。
// - 若 ATYP=域名，会先写入 1 字节长度，再写入实际域名字节。
// - 若 Type 未知，将降级为 IPv4 的 0.0.0.0。
func (addr *Addr) Encode(b []byte) (int, error) {
	b[0] = addr.Type
	pos := 1
	switch addr.Type {
	case ipV4:
		ip4 := net.ParseIP(addr.Host).To4()
		if ip4 == nil {
			ip4 = net.IPv4zero.To4()
		}
		pos += copy(b[pos:], ip4)
	case domainName:
		b[pos] = byte(len(addr.Host))
		pos++
		pos += copy(b[pos:], []byte(addr.Host))
	case ipV6:
		ip16 := net.ParseIP(addr.Host).To16()
		if ip16 == nil {
			ip16 = net.IPv6zero.To16()
		}
		pos += copy(b[pos:], ip16)
	default:
		b[0] = ipV4
		copy(b[pos:pos+4], net.IPv4zero.To4())
		pos += 4
	}
	binary.BigEndian.PutUint16(b[pos:], addr.Port)
	pos += 2

	return pos, nil
}

// Write 将 UDPHeader 写入到 io.Writer。
// 内部会从 BufPoolUdp 申请一个临时缓冲区：
//   [RSV(2) | FRAG(1) | ATYP(1) | ADDR | PORT]
// 若 Addr 为空，则写入一个空地址（默认零值）。
func (h *UDPHeader) Write(w io.Writer) error {
	b := BufPoolUdp.Get().([]byte)
	defer BufPoolUdp.Put(b)

	binary.BigEndian.PutUint16(b[:2], h.Rsv)
	b[2] = h.Frag

	addr := h.Addr
	if addr == nil {
		addr = &Addr{}
	}
	length, _ := addr.Encode(b[3:])

	_, err := w.Write(b[:3+length])
	return err
}

// UDPDatagram 表示一个完整的 UDP 数据报（头部 + 数据段）。
// Header 即 SOCKS5 头部；Data 为实际负载数据。
type UDPDatagram struct {
	Header *UDPHeader
	Data   []byte
}

// ReadUDPDatagram 从一个 io.Reader 中读取并解析出 UDPDatagram。
//
// 读取流程：
// 1) 先读取前 5 字节 [RSV(2) | FRAG(1) | ATYP(1) | LEN/ADDR首字节]，以确定 ATYP 与头部总长 hlen。
// 2) 若 header.Rsv == 0（标准 SOCKS5）：读取 reader 中剩余全部数据，认为没有冗余；数据长度 dlen = 总长 - hlen。
// 3) 若 header.Rsv != 0（扩展：UDP over TCP）：按 dlen=Rsv 精确再读，确保不多不少，便于在流式连接上切分报文。
// 4) 解码地址字段，并复制数据段到独立切片中返回。
//
// 返回：成功时返回数据报指针，失败时返回错误。
func ReadUDPDatagram(r io.Reader) (*UDPDatagram, error) {
	b := BufPoolUdp.Get().([]byte)
	defer BufPoolUdp.Put(b)

	// 当 r 是流式载体（如 TCP）时，Read 可能会多读，导致边界不明。
	// 因此使用 io.ReadFull（而非 ReadAtLeast）确保不额外丢弃数据。
	n, err := io.ReadFull(r, b[:5])
	if err != nil {
		return nil, err
	}

	header := &UDPHeader{
		Rsv:  binary.BigEndian.Uint16(b[:2]),
		Frag: b[2],
	}

	atype := b[3]
	hlen := 0
	switch atype {
	case ipV4:
		hlen = 10 // 2(RSV)+1(FRAG)+1(ATYP)+4(ADDR)+2(PORT)
	case ipV6:
		hlen = 22 // 2+1+1+16+2
	case domainName:
		hlen = 7 + int(b[4]) // 2+1+1+1(len)+len+2
	default:
		return nil, errors.New("addr not support")
	}
	dlen := int(header.Rsv)
	if dlen == 0 { // 标准 SOCKS5 UDP 数据报
		extra, err := ioutil.ReadAll(r) // 假设无冗余数据，全部视为当前报文的一部分
		if err != nil {
			return nil, err
		}
		copy(b[n:], extra)
		n += len(extra) // 总长度
		dlen = n - hlen // 数据段长度
	} else { // 扩展：在 UDP over TCP 时，用 Rsv 指示数据段长度，便于定长读取
		if _, err := io.ReadFull(r, b[n:hlen+dlen]); err != nil {
			return nil, err
		}
		n = hlen + dlen
	}
	header.Addr = new(Addr)
	if err := header.Addr.Decode(b[3:hlen]); err != nil {
		return nil, err
	}
	data := make([]byte, dlen)
	copy(data, b[hlen:n])
	d := &UDPDatagram{
		Header: header,
		Data:   data,
	}
	return d, nil
}

// NewUDPDatagram 构造一个 UDPDatagram。
func NewUDPDatagram(header *UDPHeader, data []byte) *UDPDatagram {
	return &UDPDatagram{
		Header: header,
		Data:   data,
	}
}

// Write 将 UDPDatagram 写入到 io.Writer。
// 过程：先写入头部（UDPHeader.Write），随后写入数据段，再整体写出到目标。
func (d *UDPDatagram) Write(w io.Writer) error {
	h := d.Header
	if h == nil {
		h = &UDPHeader{}
	}
	buf := bytes.Buffer{}
	if err := h.Write(&buf); err != nil {
		return err
	}
	if _, err := buf.Write(d.Data); err != nil {
		return err
	}

	_, err := buf.WriteTo(w)
	return err
}

// ToSocksAddr 将 net.Addr（如 UDPConn 的远端地址）转换为 SOCKS5 Addr 表示。
// 若传入为 nil，则返回 0.0.0.0:0；类型固定设置为 IPv4（ipV4）。
func ToSocksAddr(addr net.Addr) *Addr {
	host := "0.0.0.0"
	port := 0
	if addr != nil {
		h, p, _ := net.SplitHostPort(addr.String())
		host = h
		port, _ = strconv.Atoi(p)
	}
	return &Addr{
		Type: ipV4,
		Host: host,
		Port: uint16(port),
	}
}
