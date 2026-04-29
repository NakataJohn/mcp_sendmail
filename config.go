package main

import (
	"fmt"
	"os"
	"strconv"
)

// Config 保存SMTP邮件服务器的连接配置
// 这些值通过环境变量传入，在 opencode.json 的 environment 字段中设置
type Config struct {
	SMTPHost string // SMTP服务器地址，如 smtp.163.com、smtp.qq.com
	SMTPPort int    // SMTP服务器端口，465为SSL/TLS端口，587为STARTTLS端口
	SMTPUser string // 发件人邮箱地址，同时也用作SMTP认证的用户名
	SMTPPass string // SMTP授权码（不是邮箱登录密码），需要在邮箱设置中开启SMTP服务后获取
}

// loadConfig 从环境变量加载SMTP配置
// 环境变量由 OpenCode 的 MCP 配置中的 environment 字段注入
// 必填项（SMTP_USER、SMTP_PASS）如果缺失则返回错误
func loadConfig() (*Config, error) {
	cfg := &Config{}

	// 读取各配置项，未设置时使用默认值
	// 默认SMTP_HOST为QQ邮箱服务器，默认端口465为SSL加密端口
	cfg.SMTPHost = getEnv("SMTP_HOST", "smtp.qq.com")
	cfg.SMTPPort = getEnvInt("SMTP_PORT", 465)
	cfg.SMTPUser = getEnv("SMTP_USER", "")
	cfg.SMTPPass = getEnv("SMTP_PASS", "")

	// 校验必填项：邮箱地址和授权码不能为空
	// 没有这两项，SMTP认证会失败，邮件无法发送
	if cfg.SMTPUser == "" {
		return nil, fmt.Errorf("SMTP_USER environment variable is required (sender email address)")
	}
	if cfg.SMTPPass == "" {
		return nil, fmt.Errorf("SMTP_PASS environment variable is required (authorization code)")
	}

	return cfg, nil
}

// getEnv 读取字符串类型的环境变量，不存在或为空时返回默认值
func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// getEnvInt 读取整数类型的环境变量，不存在或解析失败时返回默认值
// 用于 SMTP_PORT 等数字配置项
func getEnvInt(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		// 环境变量值不是合法数字时，使用默认值而不是报错
		return fallback
	}
	return n
}
