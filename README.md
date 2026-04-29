# MCP Email Server

基于 [mcp-go](https://github.com/mark3labs/mcp-go) 的本地邮件发送 MCP 服务。

## 功能

- 提供 `send_email` 工具，AI 对话中指定收件邮箱和内容即可发送邮件
- 邮件主题从正文第一行自动提取，超过50字截断
- 支持多个收件人（逗号分隔）

## 快速开始

### 编译

```bash
go build -o mcp-email-server.exe .
```

### 配置

在 `opencode.json` 中添加：

```json
{
  "mcp": {
    "email": {
      "type": "local",
      "command": ["D:\\opencode\\mcp\\mcp-email-server.exe"],
      "enabled": true,
      "environment": {
        "SMTP_HOST": "smtp.163.com",
        "SMTP_PORT": "465",
        "SMTP_USER": "your_email@163.com",
        "SMTP_PASS": "your_authorization_code"
      }
    }
  }
}
```

### 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `SMTP_HOST` | SMTP 服务器地址 | `smtp.qq.com` |
| `SMTP_PORT` | SMTP 端口 | `465` |
| `SMTP_USER` | 发件邮箱（必填） | - |
| `SMTP_PASS` | 授权码（必填） | - |

## 常见邮箱 SMTP 配置

| 邮箱 | SMTP_HOST | SMTP_PORT |
|------|-----------|-----------|
| QQ邮箱 | smtp.qq.com | 465 |
| 163邮箱 | smtp.163.com | 465 |
| 126邮箱 | smtp.126.com | 465 |
| Gmail | smtp.gmail.com | 587 |

## 学习资料

详细的技术原理和代码解读见 [TUTORIAL.md](./TUTORIAL.md)。