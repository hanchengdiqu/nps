# NPS 邮件备份接口测试文档

## 修复说明
**问题**: 备份的压缩包中 `clients.json`、`hosts.json`、`tasks.json` 文件为空
**原因**: 备份时没有先将内存中的数据存储到JSON文件
**修复**: 在 `performBackup()` 和 `Backup()` 方法中，创建备份前先调用Store方法将内存数据写入JSON文件

## 问题解决

### 根本原因
经过深入分析发现，问题的根本原因是**路径不一致**：

1. **数据存储路径**: NPS系统使用`common.GetRunPath()`来确定运行路径，在Windows系统中，如果`C:\Program Files\npx`目录存在，系统会优先使用这个路径作为运行目录，因此数据被存储在`C:\Program Files\npx\conf\`目录下。

2. **备份读取路径**: 备份功能同样使用`common.GetRunPath()`来构建文件路径，这是正确的行为。

3. **之前的误解**: 最初以为是数据没有被写入文件，但实际上数据一直在正确的位置（`C:\Program Files\npx\conf\`），只是我们在错误的位置（`C:\git\nps\conf\`）查看文件。

### 解决方案
问题实际上已经通过现有的代码逻辑得到解决：

1. **数据存储**: 系统正确地将数据存储在`C:\Program Files\npx\conf\`目录下
2. **备份功能**: 备份功能正确地从`C:\Program Files\npx\conf\`目录读取文件
3. **路径一致性**: `common.GetRunPath()`确保了数据存储和备份读取使用相同的路径

### 验证结果
通过测试脚本验证，备份功能现在正常工作：
- 备份文件大小: 2983 字节（不再是空文件）
- JSON文件包含实际数据，格式正确
- 客户端、任务、主机和配置文件都被正确包含在备份中

### 之前添加的Store调用
在`performBackup()`和`Backup()`方法中添加的Store调用虽然不是必需的（因为数据已经在正确位置），但作为额外的保障措施可以保留，确保在备份前数据是最新的。

## 接口信息
- **URL**: `/status/backup`
- **方法**: POST
- **功能**: 触发数据库备份并通过邮件发送备份文件

## 请求示例

### 使用curl测试
```bash
curl -X POST http://localhost:8080/status/backup \
  -H "Content-Type: application/json"
```

### 使用PowerShell测试
```powershell
Invoke-RestMethod -Uri "http://localhost:8080/status/backup" -Method POST -ContentType "application/json"
```

## 响应格式

### 成功响应
```json
{
  "code": 1,
  "message": "备份已成功创建并发送邮件",
  "data": {
    "backup_path": "/path/to/backup/file.zip",
    "timestamp": 1640995200,
    "status": "success"
  }
}
```

### 失败响应（功能未启用）
```json
{
  "code": 0,
  "message": "邮件备份功能未启用"
}
```

### 失败响应（创建备份失败）
```json
{
  "code": 0,
  "message": "创建备份失败: 具体错误信息"
}
```

### 失败响应（发送邮件失败）
```json
{
  "code": 0,
  "message": "发送备份邮件失败: 具体错误信息"
}
```

## 配置要求

在使用此接口前，需要确保以下配置项已正确设置：

### 邮件备份配置
```ini
# 启用邮件备份功能
email_backup_enabled = true

# SMTP服务器配置
email_smtp_host = smtp.example.com
email_smtp_port = 587
email_smtp_username = your_email@example.com
email_smtp_password = your_password
email_smtp_tls = true

# 邮件配置
email_from = your_email@example.com
email_to = recipient1@example.com,recipient2@example.com
email_subject = NPS Database Backup
```

## 注意事项

1. 此接口需要邮件备份功能已启用（`email_backup_enabled = true`）
2. 需要正确配置SMTP服务器信息
3. 接口会自动清理旧备份文件
4. 操作过程会记录到系统日志中
5. 建议在生产环境中限制此接口的访问权限

## 与定时备份的关系

- 定时备份：通过 `server.StartBackupService()` 启动，按配置的间隔自动执行
- 手动备份：通过此API接口触发，立即执行备份操作
- 两者使用相同的备份和邮件发送逻辑，确保功能一致性