package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"ehang.io/nps/client"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/version"
	"github.com/astaxie/beego/logs"
	"unsafe"
)

var cl *client.TRPClient

//export StartClientByVerifyKey
func StartClientByVerifyKey(serverAddr, verifyKey, connType, proxyUrl *C.char) int {
	_ = logs.SetLogger("store")
	if cl != nil {
		cl.Close()
	}
	cl = client.NewRPClient(C.GoString(serverAddr), C.GoString(verifyKey), C.GoString(connType), C.GoString(proxyUrl), nil, 60)
	cl.Start()
	return 1
}

//export GetClientStatus
func GetClientStatus() int {
	return client.NowStatus
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

//export InitDef
func InitDef() int {
	serverAddr := C.CString("www.198408.xyz:65203")
	verifyKey := C.CString("abcdefg")
	connType := C.CString("tcp")
	proxyUrl := C.CString("")
	
	defer C.free(unsafe.Pointer(serverAddr))
	defer C.free(unsafe.Pointer(verifyKey))
	defer C.free(unsafe.Pointer(connType))
	defer C.free(unsafe.Pointer(proxyUrl))
	
	return StartClientByVerifyKey(serverAddr, verifyKey, connType, proxyUrl)
}

//export InitDefWithKey
func InitDefWithKey(verifyKey *C.char) int {
	serverAddr := C.CString("www.198408.xyz:65203")
	connType := C.CString("tcp")
	proxyUrl := C.CString("")
	
	defer C.free(unsafe.Pointer(serverAddr))
	defer C.free(unsafe.Pointer(connType))
	defer C.free(unsafe.Pointer(proxyUrl))
	
	return StartClientByVerifyKey(serverAddr, verifyKey, connType, proxyUrl)
}

func main() {
	// Need a main function to make CGO compile package as C shared library
}
