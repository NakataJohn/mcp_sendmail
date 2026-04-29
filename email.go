package main

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// sendEmail 通过SMTP协议发送邮件
// 核心流程：建立TLS连接 -> SMTP认证 -> 设置发件人 -> 设置收件人 -> 发送邮件内容
func sendEmail(cfg *Config, to, subject, body string) error {
	from := cfg.SMTPUser
	// 组合SMTP服务器地址，格式为 "host:port"，如 "smtp.163.com:465"
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	// 构建邮件消息体（包含From/To/Subject头部和正文）
	msg := buildMessage(from, to, subject, body)

	// ========== 第一步：建立TLS加密连接 ==========
	// 端口465使用"隐式TLS"（Implicit TLS），需要先建立TLS连接再进行SMTP通信
	// 与端口587的"显式TLS"（STARTTLS）不同，465端口从一开始就是加密的
	tlsConfig := &tls.Config{
		ServerName: cfg.SMTPHost, // 用于TLS证书验证，必须与服务器主机名一致
	}

	// 直接通过TLS拨号连接SMTP服务器
	// 这与STARTTLS流程不同：STARTTLS是先建立普通TCP连接，再升级为TLS
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS connection failed: %w", err)
	}
	defer conn.Close()

	// ========== 第二步：在TLS连接上创建SMTP客户端 ==========
	// smtp.NewClient 将TLS连接包装为SMTP协议客户端
	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer client.Close()

	// ========== 第三步：SMTP认证 ==========
	// PlainAuth 使用明文认证（但已被TLS加密保护，所以实际是安全的）
	// 参数：identity(通常为空), username, password, host
	// 认证信息在TLS加密通道内传输，不会被窃听
	auth := smtp.PlainAuth("", from, cfg.SMTPPass, cfg.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP auth failed: %w", err)
	}

	// ========== 第四步：设置发件人地址 ==========
	// Mail 方法设置SMTP的信封发件人（Envelope From）
	// 注意：信封发件人可以与邮件头中的From不同，这里我们使用同一个地址
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP Mail failed: %w", err)
	}

	// ========== 第五步：设置收件人地址 ==========
	// 支持多个收件人：用逗号分隔的邮箱地址会被逐个添加
	// Rcpt 方法设置SMTP的信封收件人（Envelope To）
	recipients := strings.Split(to, ",")
	for _, rcpt := range recipients {
		rcpt = strings.TrimSpace(rcpt)
		if rcpt == "" {
			continue
		}
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("SMTP Rcpt failed for %s: %w", rcpt, err)
		}
	}

	// ========== 第六步：发送邮件内容 ==========
	// Data 方法获取一个写入器，用于写入邮件的完整内容（头部+正文）
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP Data failed: %w", err)
	}

	// 将构建好的邮件消息写入Data写入器
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("SMTP Write failed: %w", err)
	}

	// Close 会将写入器中的数据提交给SMTP服务器
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP Close failed: %w", err)
	}

	// ========== 第七步：优雅关闭连接 ==========
	// Quit 向服务器发送QUIT命令，正常结束SMTP会话
	// 比直接关闭连接更礼貌，服务器会确认收到邮件
	if err := client.Quit(); err != nil {
		return fmt.Errorf("SMTP Quit failed: %w", err)
	}

	return nil
}

// buildMessage 构建符合SMTP标准的邮件消息体
// 邮件消息 = 邮件头部（Header） + 空行 + 邮件正文（Body）
// 头部和正文之间必须用一个空行分隔
func buildMessage(from, to, subject, body string) string {
	lines := []string{
		fmt.Sprintf("From: %s", from),             // 发件人
		fmt.Sprintf("To: %s", to),                 // 收件人（显示在邮件头中）
		fmt.Sprintf("Subject: %s", subject),       // 邮件主题
		"MIME-Version: 1.0",                       // MIME版本声明
		"Content-Type: text/plain; charset=UTF-8", // 内容类型：纯文本，UTF-8编码（支持中文）
		"",   // 头部与正文之间的空行分隔符（必须）
		body, // 邮件正文内容
	}
	// SMTP协议要求行尾使用 CRLF（\r\n），不是单纯的 LF（\n）
	return strings.Join(lines, "\r\n")
}
