package version

/**
### 4. 构建时的灵活性
虽然当前代码中两个值相同，但这种设计为将来提供了灵活性：

- 可以通过构建脚本的 -ldflags 参数在编译时动态替换版本号
- 可以让显示版本和协议版本独立管理
- 便于实现向后兼容策略
### 5. API 一致性
提供函数接口 GetVersion() 而不是直接使用常量，可以：

- 保持 API 的一致性和可扩展性
- 未来可以在函数中添加额外的逻辑（如版本格式化、环境检测等）
- 便于单元测试和模拟
*/

// 用于显示给用户的版本信息,对外显示的完整版本号（如 "0.26.11"）
const VERSION = "0.26.10"

// Compulsory minimum version, Minimum downward compatibility to this version
//主要用于 核心协议版本 的校验，确保客户端和服务端能够正常通信。
//返回的是 最低兼容版本 。
//如果客户端的版本低于服务端的最低兼容版本，服务端将拒绝连接。
// 发送版本信息用于协议校验
// c.WriteLenContent([]byte(version.GetVersion()))
// c.WriteLenContent([]byte(version.VERSION))

// // 验证客户端和服务端版本是否匹配
//
//	if crypt.Md5(version.GetVersion()) != string(b) {
//	    logs.Error("The client does not match the server version...")
//	}
func GetVersion() string {
	return "0.26.10"
}
