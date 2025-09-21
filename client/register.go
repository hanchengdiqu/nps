package client

// register.go 提供客户端向服务端注册本机公网 IP 的能力。
// 该文件仅负责发起一次性注册请求，并不保持长连接或做重试逻辑。

import (
	"encoding/binary"
	"log"
	"os"

	"ehang.io/nps/lib/common"
)

// RegisterLocalIp 向 NPS 服务端注册当前客户端的公网 IP 信息。
// 注意：该函数为一次性流程，成功后会直接调用 os.Exit(0) 退出当前进程。
// 因此，调用方不应在此函数之后再依赖 defer 资源回收或继续执行业务逻辑。
//
// 参数说明：
//   - server   服务端地址（host:port），例如 "example.com:8024"
//   - vKey     验证密钥（与服务端保持一致，用于鉴权）
//   - tp       传输协议类型，通常为 "tcp" 或 "kcp" 等，由 NewConn 支持的类型决定
//   - proxyUrl 代理地址（可为空）。当需要通过 HTTP/SOCKS 代理连接服务端时传入
//   - hour     注册的有效时长（小时）。将以 int32 LittleEndian 形式写入到连接中
//
// 工作流程：
//  1. 通过 NewConn 与服务端建立一个“注册”用途的临时连接（WORK_REGISTER）。
//  2. 将 hour 以小端序的 int32 写入连接，供服务端读取并设置有效期。
//  3. 记录成功日志后直接退出进程。
func RegisterLocalIp(server string, vKey string, tp string, proxyUrl string, hour int) {
	// 1) 建立到服务端的连接，工作类型为注册（WORK_REGISTER）。
	//    连接建立失败将直接退出程序（log.Fatalln 会打印错误并调用 os.Exit(1)）。
	c, err := NewConn(tp, vKey, server, common.WORK_REGISTER, proxyUrl)
	if err != nil {
		log.Fatalln(err)
	}

	// 2) 将有效期写入到连接：按小端序写入 int32。
	//    服务端会据此确定注册条目的可用时长（单位：小时）。
	if err := binary.Write(c, binary.LittleEndian, int32(hour)); err != nil {
		log.Fatalln(err)
	}

	// 3) 输出成功日志并以 0 码退出进程，表示本次注册流程完成。
	log.Printf("Successful ip registration for local public network, the validity period is %d hours.", hour)
	os.Exit(0)
}
