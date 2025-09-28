package email

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

// EmailService 邮件服务结构体
type EmailService struct {
	SmtpHost     string
	SmtpPort     int
	SmtpUsername string
	SmtpPassword string
	SmtpTLS      bool
	From         string
	To           []string
	Subject      string
}

// NewEmailService 创建邮件服务实例
func NewEmailService() *EmailService {
	toStr := beego.AppConfig.String("email_to")
	var to []string
	if toStr != "" {
		to = strings.Split(toStr, ",")
		// 清理空格
		for i, addr := range to {
			to[i] = strings.TrimSpace(addr)
		}
	}

	return &EmailService{
		SmtpHost:     beego.AppConfig.String("email_smtp_host"),
		SmtpPort:     beego.AppConfig.DefaultInt("email_smtp_port", 587),
		SmtpUsername: beego.AppConfig.String("email_smtp_username"),
		SmtpPassword: beego.AppConfig.String("email_smtp_password"),
		SmtpTLS:      beego.AppConfig.DefaultBool("email_smtp_tls", true),
		From:         beego.AppConfig.String("email_from"),
		To:           to,
		Subject:      beego.AppConfig.String("email_subject"),
	}
}

// SendBackupEmail 发送备份邮件
func (e *EmailService) SendBackupEmail(backupFiles []string) error {
	if !beego.AppConfig.DefaultBool("email_backup_enabled", false) {
		logs.Info("Email backup is disabled")
		return nil
	}

	if len(backupFiles) == 0 {
		return fmt.Errorf("no backup files to send")
	}

	if len(e.To) == 0 {
		return fmt.Errorf("no recipient email addresses configured")
	}

	// 创建邮件内容
	body := e.createEmailBody(backupFiles)

	// 发送邮件
	return e.sendEmail(body, backupFiles)
}

// createEmailBody 创建邮件正文
func (e *EmailService) createEmailBody(backupFiles []string) string {
	var buffer bytes.Buffer
	buffer.WriteString("NPS 数据库备份报告\n")
	buffer.WriteString("==================\n\n")
	buffer.WriteString(fmt.Sprintf("备份时间: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	buffer.WriteString(fmt.Sprintf("备份文件数量: %d\n\n", len(backupFiles)))
	buffer.WriteString("备份文件列表:\n")
	for i, file := range backupFiles {
		buffer.WriteString(fmt.Sprintf("%d. %s\n", i+1, filepath.Base(file)))
	}
	buffer.WriteString("\n此邮件由 NPS 系统自动发送，请勿回复。")
	return buffer.String()
}

// sendEmail 发送邮件
func (e *EmailService) sendEmail(body string, attachments []string) error {
	// 创建邮件头
	headers := make(map[string]string)
	headers["From"] = e.From
	headers["To"] = strings.Join(e.To, ", ")
	headers["Subject"] = e.Subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "multipart/mixed; boundary=boundary123"

	// 创建邮件内容
	var emailBody bytes.Buffer
	for key, value := range headers {
		emailBody.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}
	emailBody.WriteString("\r\n")

	// 添加邮件正文
	emailBody.WriteString("--boundary123\r\n")
	emailBody.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	emailBody.WriteString("\r\n")
	emailBody.WriteString(body)
	emailBody.WriteString("\r\n")

	// 添加附件
	for _, file := range attachments {
		if err := e.addAttachment(&emailBody, file); err != nil {
			logs.Error("Failed to add attachment %s: %v", file, err)
			continue
		}
	}

	emailBody.WriteString("--boundary123--\r\n")

	// 发送邮件
	return e.sendSMTP(emailBody.Bytes())
}

// addAttachment 添加附件
func (e *EmailService) addAttachment(emailBody *bytes.Buffer, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	emailBody.WriteString("--boundary123\r\n")
	emailBody.WriteString(fmt.Sprintf("Content-Type: application/octet-stream\r\n"))
	emailBody.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", filepath.Base(filePath)))
	emailBody.WriteString("Content-Transfer-Encoding: base64\r\n")
	emailBody.WriteString("\r\n")

	// Base64编码
	encoded := base64.StdEncoding.EncodeToString(content)
	// 每76个字符换行
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		emailBody.WriteString(encoded[i:end])
		emailBody.WriteString("\r\n")
	}

	return nil
}

// sendSMTP 通过SMTP发送邮件
func (e *EmailService) sendSMTP(body []byte) error {
	addr := fmt.Sprintf("%s:%d", e.SmtpHost, e.SmtpPort)

	var auth smtp.Auth
	if e.SmtpUsername != "" && e.SmtpPassword != "" {
		auth = smtp.PlainAuth("", e.SmtpUsername, e.SmtpPassword, e.SmtpHost)
	}

	if e.SmtpTLS {
		return e.sendWithTLS(addr, auth, body)
	}

	return smtp.SendMail(addr, auth, e.From, e.To, body)
}

// sendWithTLS 使用TLS发送邮件
func (e *EmailService) sendWithTLS(addr string, auth smtp.Auth, body []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: e.SmtpHost})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, e.SmtpHost)
	if err != nil {
		return err
	}
	defer client.Quit()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	if err := client.Mail(e.From); err != nil {
		return err
	}

	for _, to := range e.To {
		if err := client.Rcpt(to); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	defer writer.Close()

	_, err = writer.Write(body)
	return err
}
