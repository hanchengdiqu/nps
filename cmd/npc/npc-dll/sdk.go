package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"sync"
	"unsafe"

	"ehang.io/nps/client"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/version"
)

var (
	clMu sync.Mutex
	cl   *client.TRPClient
)

//export StartClientByVerifyKey
func StartClientByVerifyKey(serverAddr, verifyKey, connType, proxyUrl *C.char) C.int {
	clMu.Lock()
	defer clMu.Unlock()

	// 关闭旧实例（若有）
	if cl != nil {
		cl.Close()
		cl = nil
	}

	srv := C.GoString(serverAddr)
	key := C.GoString(verifyKey)
	ct := C.GoString(connType)
	proxy := C.GoString(proxyUrl)

	cl = client.NewRPClient(srv, key, ct, proxy, nil, 60)

	// 很多 nps 的 Start() 会阻塞，放到 goroutine 里跑，避免卡死调用方
	go cl.Start()

	return 1
}

//export GetClientStatus
func GetClientStatus() C.int {
	return C.int(client.NowStatus)
}

//export CloseClient
func CloseClient() {
	clMu.Lock()
	defer clMu.Unlock()
	if cl != nil {
		cl.Close()
		cl = nil
	}
}

//export Version
func Version() *C.char {
	return C.CString(version.VERSION)
}

//export Logs
func Logs() *C.char {
	return C.CString(common.GetLogMsg())
}

// 提供给 C#/C 侧释放用，避免 CString 泄漏
//
//export NpcFreeCString
func NpcFreeCString(p *C.char) {
	if p != nil {
		C.free(unsafe.Pointer(p))
	}
}

func main() {
	// 留空即可，使其能编译为 c-shared
}
