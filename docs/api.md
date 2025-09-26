# web api

需要开启请先去掉`nps.conf`中`auth_key`的注释并配置一个合适的密钥
## webAPI验证说明
- 采用auth_key的验证方式
- 在提交的每个请求后面附带两个参数，`auth_key` 和`timestamp`

```
auth_key的生成方式为：md5(配置文件中的auth_key+当前时间戳)
```

```
timestamp为当前时间戳
```
```
curl --request POST \
  --url http://127.0.0.1:8080/client/list \
  --data 'auth_key=2a0000d9229e7dbcf79dd0f5e04bb084&timestamp=1553045344&start=0&limit=10'
```
**注意：** 为保证安全，时间戳的有效范围为20秒内，所以每次提交请求必须重新生成。

## 获取服务端时间
由于服务端与api请求的客户端时间差异不能太大，所以提供了一个可以获取服务端时间的接口

```
POST /auth/gettime
```

## 获取服务端authKey

如果想获取authKey，服务端提供获取authKey的接口

```
POST /auth/getauthkey
```
将返回加密后的authKey，采用aes cbc加密，请使用与服务端配置文件中cryptKey相同的密钥进行解密

**注意：** nps配置文件中`auth_crypt_key`需为16位
- 解密密钥长度128
- 偏移量与密钥相同
- 补码方式pkcs5padding
- 解密串编码方式 十六进制

## 系统统计接口

### 获取系统统计数据

```
POST /status/stats
```

**接口说明：** 获取NPS服务器的实时统计数据，包括客户端、隧道、流量等信息。

**请求参数：**

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| auth_key | string | 是 | MD5(配置文件中的auth_key+当前时间戳) |
| timestamp | int | 是 | 当前时间戳 |

**响应格式：**

```json
{
  "code": 1,
  "data": {
    "active_clients": 0,
    "total_clients": 1,
    "active_tunnels": 2,
    "total_tunnels": 0,
    "today_in_flow": 0,
    "today_out_flow": 0,
    "today_total_flow": 0,
    "domain_count": 0,
    "timestamp": 1758891915
  }
}
```

**响应字段说明：**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| code | int | 状态码，1表示成功 |
| data | object | 统计数据对象 |
| active_clients | int | 当前活跃的客户端数量 |
| total_clients | int | 客户端总数量（排除公共客户端） |
| active_tunnels | int | 当前活跃的隧道数量 |
| total_tunnels | int | 隧道总数量 |
| today_in_flow | int | 今日入站流量（字节） |
| today_out_flow | int | 今日出站流量（字节） |
| today_total_flow | int | 今日总流量（字节） |
| domain_count | int | 域名解析数量（主机配置数量） |
| timestamp | int | 服务器当前时间戳 |

**请求示例：**

```bash
# 1. 获取服务器时间戳
curl -X GET http://localhost:8081/auth/gettime

# 2. 计算auth_key（假设配置的auth_key为"test"，时间戳为1758891904）
# auth_key = MD5("test1758891904") = "79fa79afc005a820c956c5cfa86aec6c"

# 3. 发送请求
curl -X POST \
  -d "auth_key=79fa79afc005a820c956c5cfa86aec6c&timestamp=1758891904" \
  http://localhost:8081/status/stats
```

**注意事项：**
- 时间戳有效期为20秒，超时需要重新获取
- 流量数据单位为字节
- 活跃客户端指当前在线的客户端
- 活跃隧道指当前正在运行的隧道

## 详细文档
- **[详见](webapi.md)** (感谢@avengexyz)
