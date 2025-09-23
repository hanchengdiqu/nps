package main

/*
#include <stdlib.h>
static void my_free(void* p){ free(p); }  // ← 补上这个封装，避免与运行时符号冲突
*/
import "C"

import (
	"unsafe"

	"ehang.io/nps/client"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/version"
	"github.com/astaxie/beego/logs"
)

var cl *client.TRPClient

//export StartClientByVerifyKey
func StartClientByVerifyKey(serverAddr, verifyKey, connType, proxyUrl *C.char) C.int {
	_ = logs.SetLogger("store")
	if cl != nil {
		cl.Close()
	}
	cl = client.NewRPClient(
		C.GoString(serverAddr),
		C.GoString(verifyKey),
		C.GoString(connType),
		C.GoString(proxyUrl),
		nil,
		60,
	)
	cl.Start()
	return C.int(1)
}

//export GetClientStatus
func GetClientStatus() C.int {
	return C.int(client.NowStatus)
}

//export CloseClient
func CloseClient() {
	if cl != nil {
		cl.Close()
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

//export NpcFreeCString
func NpcFreeCString(p *C.char) {
	if p != nil {
		C.my_free(unsafe.Pointer(p))
	}
}

func main() {}
