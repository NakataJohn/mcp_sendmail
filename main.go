package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// 程序入口：加载配置 -> 创建MCP服务器 -> 注册工具 -> 启动stdio传输
func main() {
	// 第一步：加载SMTP配置（从环境变量读取邮箱和授权码）
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 第二步：创建MCP服务器实例
	// 参数分别是：服务器名称、版本号、功能选项
	// WithToolCapabilities(true) 表示这个服务器提供工具（Tool）能力
	mcpServer := server.NewMCPServer(
		"mcp-email-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// 第三步：定义 send_email 工具
	// MCP工具由三个部分组成：名称、描述、参数定义
	// 这里的描述会被AI模型看到，帮助它判断何时调用此工具
	tool := mcp.NewTool("send_email",
		mcp.WithDescription("Send an email to a specified address. The subject is automatically generated from the email content. You need to provide the recipient's email address and the email body content."),
		// 定义 "to" 参数：收件人邮箱地址，必填
		mcp.WithString("to",
			mcp.Description("Recipient email address"),
			mcp.Required(),
		),
		// 定义 "content" 参数：邮件正文内容，必填
		mcp.WithString("content",
			mcp.Description("Email body content"),
			mcp.Required(),
		),
	)

	// 第四步：将工具注册到服务器，并绑定处理函数
	// 当AI模型调用 send_email 工具时，会执行这个匿名函数
	mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// 从请求中提取参数值
		// GetString 方法会从 request.Params.Arguments 中按名称取值，第二个参数是默认值
		to := request.GetString("to", "")
		content := request.GetString("content", "")

		// 参数校验：确保必填参数不为空
		// 注意：MCP协议要求返回 error 为 nil，把错误信息放在 Result 中
		// NewToolResultError 会创建一个包含错误文本的 CallToolResult
		if to == "" {
			return mcp.NewToolResultError("recipient email address is required"), nil
		}
		if content == "" {
			return mcp.NewToolResultError("email content is required"), nil
		}

		// 根据邮件内容自动生成主题（取正文第一行）
		subject := generateSubject(content)

		// 调用 SMTP 发送邮件
		err := sendEmail(cfg, to, subject, content)
		if err != nil {
			// 发送失败时返回错误信息（同样 error 为 nil，错误在 Result 中）
			return mcp.NewToolResultError(fmt.Sprintf("failed to send email: %v", err)), nil
		}

		// 发送成功时返回成功信息
		// CallToolResult 的 Content 是一个数组，可以包含多种类型的内容
		// 这里使用 TextContent 返回纯文本结果
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: fmt.Sprintf("Email sent successfully to %s with subject: %s", to, subject),
				},
			},
		}, nil
	})

	// 第五步：启动MCP服务器，使用 stdio 传输方式
	// stdio 表示通过标准输入/输出与AI客户端通信（最常用的本地MCP通信方式）
	// 服务器会持续运行，等待客户端的JSON-RPC请求
	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// generateSubject 从邮件正文自动提取主题
// 策略：取正文的第一行作为主题，超过50字符则截断加 "..."
func generateSubject(content string) string {
	// 先清理空白，再按换行符拆分
	lines := strings.Split(strings.TrimSpace(content), "\n")
	firstLine := strings.TrimSpace(lines[0])

	// 限制主题长度为50个字符（使用 rune 避免中文字符被截断乱码）
	maxLen := 50
	runes := []rune(firstLine)
	if len(runes) > maxLen {
		firstLine = string(runes[:maxLen]) + "..."
	}

	// 如果内容为空，使用默认主题 "Notification"
	if firstLine == "" {
		return "Notification"
	}

	return firstLine
}
