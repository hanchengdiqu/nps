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
	"sync"
	"time"
	"unsafe"
)

var (
	cl                    *client.TRPClient
	asyncClient          *client.TRPClient
	autoReconnectEnabled bool
	reconnectInterval    int = 5 // 默认5秒重连间隔
	stopChan             chan bool
	asyncMutex           sync.Mutex
)

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

//export StartClientByVerifyKeyAsync
func StartClientByVerifyKeyAsync(serverAddr, verifyKey, connType, proxyUrl *C.char) int {
	asyncMutex.Lock()
	defer asyncMutex.Unlock()
	
	_ = logs.SetLogger("store")
	
	// 停止现有的异步客户端
	if autoReconnectEnabled {
		StopAutoReconnect()
	}
	
	// 保存C字符串参数到Go字符串，避免在goroutine中访问已释放的内存
	serverAddrStr := C.GoString(serverAddr)
	verifyKeyStr := C.GoString(verifyKey)
	connTypeStr := C.GoString(connType)
	proxyUrlStr := C.GoString(proxyUrl)
	
	// 创建新的客户端
	asyncClient = client.NewRPClient(serverAddrStr, verifyKeyStr, connTypeStr, proxyUrlStr, nil, 60)
	
	// 启用自动重连
	autoReconnectEnabled = true
	stopChan = make(chan bool, 1)
	
	// 在goroutine中启动客户端和重连逻辑
	go func() {
		for autoReconnectEnabled {
			select {
			case <-stopChan:
				return
			default:
				// 启动客户端（这会阻塞直到连接断开）
				asyncClient.Start()
				
				// 如果自动重连被禁用，退出循环
				if !autoReconnectEnabled {
					return
				}
				
				// 等待重连间隔
				select {
				case <-stopChan:
					return
				case <-time.After(time.Duration(reconnectInterval) * time.Second):
					// 创建新的客户端实例进行重连
					asyncClient = client.NewRPClient(serverAddrStr, verifyKeyStr, connTypeStr, proxyUrlStr, nil, 60)
				}
			}
		}
	}()
	
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

//export StopAutoReconnect
func StopAutoReconnect() {
	asyncMutex.Lock()
	defer asyncMutex.Unlock()
	
	if autoReconnectEnabled {
		autoReconnectEnabled = false
		if stopChan != nil {
			select {
			case stopChan <- true:
			default:
			}
		}
		if asyncClient != nil {
			asyncClient.Close()
		}
	}
}

//export SetReconnectInterval
func SetReconnectInterval(seconds int) int {
	if seconds < 1 {
		return 0 // 无效的间隔时间
	}
	asyncMutex.Lock()
	defer asyncMutex.Unlock()
	
	reconnectInterval = seconds
	return 1
}

//export IsAutoReconnectEnabled
func IsAutoReconnectEnabled() int {
	if autoReconnectEnabled {
		return 1
	}
	return 0
}

//export GetReconnectInterval
func GetReconnectInterval() int {
	return reconnectInterval
}

func main() {
	// Need a main function to make CGO compile package as C shared library
}
