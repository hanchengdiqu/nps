package backup

import (
"archive/zip"
"fmt"
"io"
"os"
"path/filepath"
"time"

"ehang.io/nps/lib/common"
"github.com/astaxie/beego"
"github.com/astaxie/beego/logs"
)

// BackupService 备份服务结构体
type BackupService struct {
BackupDir     string
RetentionDays int
}

// NewBackupService 创建备份服务实例
func NewBackupService() *BackupService {
backupDir := filepath.Join(common.GetRunPath(), "backups")
if err := os.MkdirAll(backupDir, 0755); err != nil {
logs.Error("Failed to create backup directory: %v", err)
}

return &BackupService{
BackupDir:     backupDir,
RetentionDays: beego.AppConfig.DefaultInt("email_backup_retention", 30),
}
}

// CreateBackup 创建备份
func (b *BackupService) CreateBackup() (string, error) {
timestamp := time.Now().Format("20060102_150405")
backupFileName := fmt.Sprintf("nps_backup_%s.zip", timestamp)
backupPath := filepath.Join(b.BackupDir, backupFileName)

// 创建ZIP文件
zipFile, err := os.Create(backupPath)
if err != nil {
return "", err
}
defer zipFile.Close()

zipWriter := zip.NewWriter(zipFile)
defer zipWriter.Close()

// 备份JSON数据文件
jsonFiles := []string{
filepath.Join(common.GetRunPath(), "conf", "clients.json"),
filepath.Join(common.GetRunPath(), "conf", "tasks.json"),
filepath.Join(common.GetRunPath(), "conf", "hosts.json"),
filepath.Join(common.GetRunPath(), "conf", "nps.conf"),
}

var backupFiles []string
for _, file := range jsonFiles {
if common.FileExists(file) {
if err := b.addFileToZip(zipWriter, file); err != nil {
logs.Error("Failed to add file %s to backup: %v", file, err)
continue
}
backupFiles = append(backupFiles, file)
}
}

if len(backupFiles) == 0 {
return "", fmt.Errorf("no files to backup")
}

logs.Info("Backup created: %s", backupPath)
return backupPath, nil
}

// addFileToZip 添加文件到ZIP
func (b *BackupService) addFileToZip(zipWriter *zip.Writer, filePath string) error {
file, err := os.Open(filePath)
if err != nil {
return err
}
defer file.Close()

info, err := file.Stat()
if err != nil {
return err
}

header, err := zip.FileInfoHeader(info)
if err != nil {
return err
}

header.Name = filepath.Base(filePath)
header.Method = zip.Deflate

writer, err := zipWriter.CreateHeader(header)
if err != nil {
return err
}

_, err = io.Copy(writer, file)
return err
}

// CleanOldBackups 清理旧备份
func (b *BackupService) CleanOldBackups() error {
files, err := filepath.Glob(filepath.Join(b.BackupDir, "nps_backup_*.zip"))
if err != nil {
return err
}

cutoffTime := time.Now().AddDate(0, 0, -b.RetentionDays)

for _, file := range files {
info, err := os.Stat(file)
if err != nil {
continue
}

if info.ModTime().Before(cutoffTime) {
if err := os.Remove(file); err != nil {
logs.Error("Failed to remove old backup %s: %v", file, err)
} else {
logs.Info("Removed old backup: %s", file)
}
}
}

return nil
}
