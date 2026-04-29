# 从零开始：用 Go 编写一个本地 MCP 邮件服务

> 本文记录了从编写 MCP 服务代码、配置 OpenCode、到深入理解 MCP 通信机制的完整过程，适合作为 MCP 开发的入门学习资料。

---

## 目录

1. [什么是 MCP？为什么需要它？](#1-什么是-mcp为什么需要它)
2. [项目结构设计](#2-项目结构设计)
3. [编写 MCP 服务代码](#3-编写-mcp-服务代码)
   - 3.1 [main.go — MCP 协议层](#31-maingo--mcp-协议层)
   - 3.2 [config.go — 配置层](#32-configgo--配置层)
   - 3.3 [email.go — SMTP 邮件发送层](#33-emailgo--smtp-邮件发送层)
4. [在 OpenCode 中配置 MCP 服务](#4-在-opencode-中配置-mcp-服务)
5. [测试验证](#5-测试验证)
6. [深入理解：ServeStdio 的工作原理](#6-深入理解servestdio-的工作原理)
   - 6.1 [不是"启动服务"，是"进入等待循环"](#61-不是启动服务是进入等待循环)
   - 6.2 [通信机制：管道（Pipe），不是网络](#62-通信机制管道pipe不是网络)
   - 6.3 [生命周期：跟着 OpenCode 走](#63-生命周期跟着-opencode-走)
   - 6.4 [一条消息的完整处理流程](#64-一条消息的完整处理流程)
   - 6.5 [Worker Pool 并发模型](#65-worker-pool-并发模型)
   - 6.6 [为什么不用 HTTP 端口？](#66-为什么不用-http-端口)
7. [SMTP 邮件发送详解](#7-smtp-邮件发送详解)
8. [常见邮箱 SMTP 配置参考](#8-常见邮箱-smtp-配置参考)
9. [总结](#9-总结)

---

## 1. 什么是 MCP？为什么需要它？

**MCP（Model Context Protocol）** 是一种标准化的协议，让 AI 模型能够调用外部工具。它的核心思想是：

> AI 模型本身只能"思考"和"生成文本"，但现实世界需要"行动"——发邮件、查数据库、操作文件等。MCP 就是连接"思考"和"行动"的桥梁。

### MCP 的角色分工

```
┌─────────────┐     MCP 协议      ┌─────────────────┐
│  AI 客户端   │  ← JSON-RPC →   │  MCP 工具服务     │
│  (OpenCode)  │                  │  (我们的邮件服务)  │
│             │                  │                  │
│  "帮我发邮件" │ ── 请求 ──▶      │  执行 SMTP 发送   │
│             │ ── 响应 ──▶      │  "发送成功！"      │
└─────────────┘                  └─────────────────┘
```

- **AI 客户端**（如 OpenCode）：负责理解用户意图，决定调用哪个工具
- **MCP 工具服务**（我们的程序）：负责实际执行操作，返回结果

MCP 就是一个"翻译官"——AI 客户端说 JSON-RPC，MCP 框架翻译成 Go 函数调用，Go 函数再通过 SMTP 协议跟邮件服务器对话。

---

## 2. 项目结构设计

```
mcp-email-server/
├── main.go       # MCP 协议层：注册工具，处理调用，启动服务
├── config.go     # 配置层：从环境变量读取 SMTP 账号信息
├── email.go      # 业务层：通过 SMTP 协议发送邮件
├── go.mod        # Go 模块定义
├── go.sum        # 依赖锁定
└── .env.example  # 配置示例文件
```

三个文件各司其职：

| 文件 | 职责 | 关键概念 |
|------|------|----------|
| `main.go` | MCP 协议层 | 工具注册、JSON-RPC 通信、stdio 传输 |
| `config.go` | 配置层 | 环境变量注入、必填校验 |
| `email.go` | 业务层 | TLS 连接、SMTP 认证、邮件发送 |

---

## 3. 编写 MCP 服务代码

### 3.1 main.go — MCP 协议层

这是核心入口，理解它就理解了 MCP 服务的骨架。整体流程分为五步：

```
加载配置 → 创建MCP服务器 → 定义工具 → 注册工具+处理函数 → 启动stdio传输
```

#### 第一步：加载 SMTP 配置

```go
cfg, err := loadConfig()
if err != nil {
    log.Fatalf("Failed to load config: %v", err)
}
```

从环境变量读取 SMTP 邮箱和授权码，没有这两项就无法发送邮件，直接退出。

#### 第二步：创建 MCP 服务器实例

```go
mcpServer := server.NewMCPServer(
    "mcp-email-server",
    "1.0.0",
    server.WithToolCapabilities(true),
)
```

参数分别是：服务器名称、版本号、功能选项。`WithToolCapabilities(true)` 表示这个服务器提供**工具（Tool）**能力——告诉 AI 客户端"我有工具可以调用"。

MCP 服务器支持三种能力：
- **Tools**（工具）：AI 可以调用的函数，如发送邮件
- **Resources**（资源）：AI 可以读取的数据，如文件内容
- **Prompts**（提示词）：预定义的提示词模板

我们的邮件服务只需要 Tool 能力。

#### 第三步：定义 send_email 工具

```go
tool := mcp.NewTool("send_email",
    mcp.WithDescription("Send an email to a specified address..."),
    mcp.WithString("to",
        mcp.Description("Recipient email address"),
        mcp.Required(),
    ),
    mcp.WithString("content",
        mcp.Description("Email body content"),
        mcp.Required(),
    ),
)
```

MCP 工具由三个部分组成：

| 组成 | 说明 |
|------|------|
| **名称** | `send_email`——AI 调用时使用这个标识 |
| **描述** | 告诉 AI 这个工具做什么、什么时候该用。**这段描述是给 AI 模型看的**，模型根据描述判断何时调用 |
| **参数** | 定义输入格式：`to`（收件邮箱）和 `content`（邮件正文），都是必填 |

> **重要提示**：工具的 Description 是 AI 模型的"决策依据"。写得好，模型就能准确判断何时调用；写得差，模型可能误调用或不调用。

#### 第四步：注册工具并绑定处理函数

```go
mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    to := request.GetString("to", "")
    content := request.GetString("content", "")

    // 参数校验
    if to == "" {
        return mcp.NewToolResultError("recipient email address is required"), nil
    }

    // 自动生成主题
    subject := generateSubject(content)

    // 发送邮件
    err := sendEmail(cfg, to, subject, content)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("failed to send email: %v", err)), nil
    }

    // 返回成功结果
    return &mcp.CallToolResult{
        Content: []mcp.Content{
            mcp.TextContent{
                Type: "text",
                Text: fmt.Sprintf("Email sent successfully to %s with subject: %s", to, subject),
            },
        },
    }, nil
})
```

**几个关键细节：**

1. **`request.GetString("to", "")`**：从请求的 Arguments 中按名称取值，第二个参数是默认值
2. **错误返回方式**：MCP 协议要求 `error` 返回 `nil`，把错误信息放在 `CallToolResult` 中。`NewToolResultError` 会创建一个包含错误文本的结果
3. **`CallToolResult.Content`**：是一个数组，可以包含多种类型的内容（文本、图片等），这里使用 `TextContent` 返回纯文本

#### 主题自动生成策略

```go
func generateSubject(content string) string {
    lines := strings.Split(strings.TrimSpace(content), "\n")
    firstLine := strings.TrimSpace(lines[0])

    maxLen := 50
    runes := []rune(firstLine)
    if len(runes) > maxLen {
        firstLine = string(runes[:maxLen]) + "..."
    }

    if firstLine == "" {
        return "Notification"
    }
    return firstLine
}
```

策略很简单：
- 取邮件正文的**第一个非空行**作为主题
- 超过50个字符则截断加 `...`
- 使用 `[]rune` 而非直接切片，避免中文字符被截断乱码
- 内容为空则默认主题 "Notification"

#### 第五步：启动 stdio 传输

```go
if err := server.ServeStdio(mcpServer); err != nil {
    log.Fatalf("Server error: %v", err)
}
```

用 stdio（标准输入输出）方式启动服务器。详细原理见[第6节](#6-深入理解servestdio-的工作原理)。

---

### 3.2 config.go — 配置层

```go
type Config struct {
    SMTPHost string  // SMTP服务器地址
    SMTPPort int     // SMTP服务器端口
    SMTPUser string  // 发件人邮箱（同时用作认证用户名）
    SMTPPass string  // SMTP授权码（不是邮箱登录密码！）
}
```

设计思路是**环境变量注入**：

- OpenCode 的 `opencode.json` 中 `environment` 字段设置的变量，启动 MCP 进程时会自动注入为环境变量
- 所以 `SMTP_USER` 和 `SMTP_PASS` 不需要硬编码在代码里
- `SMTP_HOST` 默认值是 `smtp.qq.com`，使用163邮箱需要在配置中覆盖为 `smtp.163.com`
- `SMTP_PORT` 默认 `465`，这是 SSL/TLS 加密端口

```go
func loadConfig() (*Config, error) {
    cfg := &Config{}
    cfg.SMTPHost = getEnv("SMTP_HOST", "smtp.qq.com")
    cfg.SMTPPort = getEnvInt("SMTP_PORT", 465)
    cfg.SMTPUser = getEnv("SMTP_USER", "")
    cfg.SMTPPass = getEnv("SMTP_PASS", "")

    // 必填校验
    if cfg.SMTPUser == "" {
        return nil, fmt.Errorf("SMTP_USER environment variable is required")
    }
    if cfg.SMTPPass == "" {
        return nil, fmt.Errorf("SMTP_PASS environment variable is required")
    }
    return cfg, nil
}
```

> **什么是授权码？** 授权码不是邮箱的登录密码。它是在邮箱设置中开启 SMTP 服务后，邮箱提供商单独生成的一串密码，专门用于第三方程序通过 SMTP 发送邮件。每个邮箱提供商的获取方式不同，通常在邮箱设置 → POP3/SMTP 中开启。

---

### 3.3 email.go — SMTP 邮件发送层

发送一封邮件要经过 **7 步 SMTP 对话**，就像寄信要经过邮局的多个环节：

```
1. tls.Dial    → 建立加密通道
2. NewClient   → 在加密通道上创建SMTP客户端
3. Auth        → 身份验证
4. Mail        → 设置发件人
5. Rcpt        → 设置收件人（支持多个）
6. Data        → 写入邮件内容
7. Quit        → 优雅关闭连接
```

#### 为什么用 tls.Dial 而不是普通连接？

端口465用的是**"隐式TLS"（Implicit TLS）**，连接从一开始就加密：

```go
tlsConfig := &tls.Config{
    ServerName: cfg.SMTPHost,  // 用于TLS证书验证
}
conn, err := tls.Dial("tcp", addr, tlsConfig)
```

与端口587的**"显式TLS"（STARTTLS）**不同：
- **465端口**：先建立TLS加密连接，再进行SMTP通信（`tls.Dial` → `smtp.NewClient`）
- **587端口**：先建立普通TCP连接，再协商升级为TLS（`net.Dial` → `smtp.NewClient` → `client.StartTLS`）

465更简单直接，且大多数国内邮箱（QQ、163）默认支持465。

#### SMTP 认证

```go
auth := smtp.PlainAuth("", from, cfg.SMTPPass, cfg.SMTPHost)
client.Auth(auth)
```

`PlainAuth` 使用明文认证，但因为已经在 TLS 加密通道内，所以实际是安全的——认证信息不会被窃听。

参数含义：`identity`（通常为空）、`username`（邮箱地址）、`password`（授权码）、`host`（SMTP服务器）。

#### 设置收件人（支持多人）

```go
recipients := strings.Split(to, ",")
for _, rcpt := range recipients {
    rcpt = strings.TrimSpace(rcpt)
    if rcpt == "" {
        continue
    }
    client.Rcpt(rcpt)
}
```

用逗号分隔多个邮箱地址，逐个添加为收件人。

#### 构建邮件消息体

```go
func buildMessage(from, to, subject, body string) string {
    lines := []string{
        fmt.Sprintf("From: %s", from),
        fmt.Sprintf("To: %s", to),
        fmt.Sprintf("Subject: %s", subject),
        "MIME-Version: 1.0",
        "Content-Type: text/plain; charset=UTF-8",
        "",           // ★ 头部与正文之间的空行（必须！）
        body,
    }
    return strings.Join(lines, "\r\n")  // ★ 行尾必须用 CRLF
}
```

两个关键细节：
1. **头部与正文之间必须有一个空行**（`""`），这是 SMTP 协议的硬性要求
2. **行尾必须用 `\r\n`（CRLF）**，不能只用 `\n`（LF），这也是 SMTP 协议规定
3. **`Content-Type: text/plain; charset=UTF-8`** 确保中文内容正确显示

---

## 4. 在 OpenCode 中配置 MCP 服务

OpenCode 的配置文件位于 `~/.config/opencode/opencode.json`（Windows 为 `C:\Users\<用户名>\.config\opencode\opencode.json`）。

### 正确的配置格式

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
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

### 配置字段说明

| 字段 | 说明 |
|------|------|
| `type` | `"local"` 表示本地 MCP 服务（stdio 传输） |
| `command` | 启动命令，**必须是数组格式**，第一个元素是可执行文件路径 |
| `enabled` | 是否启用 |
| `environment` | 环境变量注入，启动子进程时自动设置 |

### 常见错误

❌ **错误格式（Claude/Cursor 风格）**：

```json
{
  "mcpServers": {
    "email": {
      "command": "mcp-email-server.exe",  // 字符串而非数组
      "env": {                             // "env" 而非 "environment"
        "SMTP_USER": "..."
      }
    }
  }
}
```

✅ **正确格式（OpenCode 风格）**：

```json
{
  "mcp": {
    "email": {
      "type": "local",
      "command": ["mcp-email-server.exe"],  // 数组格式
      "environment": {                       // "environment"
        "SMTP_USER": "..."
      }
    }
  }
}
```

> 不同 AI 工具的 MCP 配置格式不同。OpenCode 使用 `mcp` + `type` + `command`（数组）+ `environment`，Claude Desktop 使用 `mcpServers` + `command`（字符串）+ `env`。切换工具时注意格式差异。

---

## 5. 测试验证

配置完成后，重启 OpenCode，然后在对话中直接让 AI 发邮件：

```
请向 xxx@xxx.com 发送一份问候邮件，内容你来定
```

AI 会自动识别意图，调用 `send_email` 工具，传入 `to` 和 `content` 参数。你只需要提供邮箱和内容，主题由代码自动从正文第一行提取。

---

## 6. 深入理解：ServeStdio 的工作原理

这是很多人困惑的地方。让我们从源码层面彻底搞清楚。

### 6.1 不是"启动服务"，是"进入等待循环"

看看 `ServeStdio` 的源码（`stdio.go:857`），它做了三件事：

```go
func ServeStdio(server *MCPServer, opts ...StdioOption) error {
    // 1. 创建 StdioServer 包装器
    s := NewStdioServer(server)

    // 2. 注册信号监听，收到 SIGTERM/SIGINT 就取消 context
    ctx, cancel := context.WithCancel(context.Background())
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
    go func() { <-sigChan; cancel() }()

    // 3. 把 os.Stdin 和 os.Stdout 交给 Listen
    return s.Listen(ctx, os.Stdin, os.Stdout)
}
```

**它不监听任何网络端口！** 它用的是 `os.Stdin` 和 `os.Stdout`——进程的标准输入和标准输出。

### 6.2 通信机制：管道（Pipe），不是网络

OpenCode 启动你的程序时，**不是像调用 HTTP 服务**，而是作为子进程启动，然后通过管道对接 stdin/stdout：

```
┌─────────────┐                    ┌──────────────────────┐
│  OpenCode    │                    │  mcp-email-server    │
│  (父进程)    │                    │  (子进程)             │
│             │── stdin 管道 ────▶ │                      │
│             │                    │  读取 os.Stdin        │
│             │                    │  处理 JSON-RPC 请求   │
│             │◀── stdout 道 ────│  写入 os.Stdout       │
└─────────────┘                    └──────────────────────┘
```

- OpenCode 向你的进程的 **stdin** 写入 JSON-RPC 请求
- 你的进程从 **stdout** 写出 JSON-RPC 响应
- **没有 HTTP、没有端口、没有 WebSocket**——纯粹的进程间管道通信

### 6.3 生命周期：跟着 OpenCode 走

看 `Listen` 方法（`stdio.go:521`）的核心流程：

```go
func (s *StdioServer) Listen(ctx, stdin, stdout) error {
    // 初始化工具调用队列（容量100）
    s.toolCallQueue = make(chan *toolCallWork, s.queueSize)

    // 注册唯一的 stdio 会话
    s.server.RegisterSession(ctx, &stdioSessionInstance)

    // 启动5个 worker goroutine 处理工具调用
    for i := 0; i < s.workerPoolSize; i++ {
        go s.toolCallWorker(ctx)
    }

    // 启动通知处理 goroutine
    go s.handleNotifications(ctx, stdout)

    // ★ 核心：循环读取 stdin，直到 EOF 或信号
    err := s.processInputStream(ctx, reader, stdout)

    // 优雅关闭：关闭队列，等待 worker 完成
    close(s.toolCallQueue)
    s.workerWg.Wait()
    return err
}
```

关键点：

| 事件 | 行为 |
|------|------|
| 发完一次邮件 | 回复响应，继续等待下一条 stdin 输入，**不退出** |
| OpenCode 正常关闭 | 关闭管道 → 子进程读到 EOF → 优雅退出 |
| 收到 SIGTERM/SIGINT | 取消 context → 所有 goroutine 退出 → 优雅退出 |
| OpenCode 异常崩溃 | 管道关闭 → 子进程读到 EOF → 退出 |

整个生命周期：

```
OpenCode启动 → 创建子进程 → 管道对接 → MCP初始化握手 → 等待请求...
                                                    ↓
                                  收到 tools/call → 发邮件 → 回复结果 → 继续等待
                                                    ↓
                                  收到 tools/call → 发邮件 → 回复结果 → 继续等待
                                                    ↓
OpenCode关闭 → 关闭管道 → 子进程读到EOF → 优雅退出
```

### 6.4 一条消息的完整处理流程

当 OpenCode 发送"发邮件"请求时，实际流转是：

```
① OpenCode 写入 stdin:
   {"jsonrpc":"2.0","id":1,"method":"tools/call",
    "params":{"name":"send_email","arguments":{"to":"xxx","content":"xxx"}}}

② processInputStream 逐行读取 stdin → 读到这一行JSON

③ processMessage 解析 JSON → 发现 method 是 "tools/call"

④ 把消息放入 toolCallQueue（工作队列）

⑤ 其中一个 worker goroutine 取出消息

⑥ worker 调用 MCPServer.HandleMessage → 找到 send_email 的 handler → 执行发邮件函数

⑦ handler 返回 CallToolResult

⑧ worker 把结果写入 stdout:
   {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Email sent successfully..."}]}}

⑨ OpenCode 从管道读到响应 → 呈现给用户
```

每条消息是一行 JSON，以换行符 `\n` 为分隔。这种格式称为 **JSON-RPC over Line-Delimited Stream**。

### 6.5 Worker Pool 并发模型

工具调用不是同步阻塞的，而是放入队列由 Worker Pool 处理：

```
stdin ──▶ processInputStream ──▶ toolCallQueue ──▶ Worker 1 ──▶ stdout
                                                    Worker 2
                                                    Worker 3
                                                    Worker 4
                                                    Worker 5
```

- 默认5个 worker goroutine 并发处理
- 队列容量100
- 如果队列满了，退化为同步处理（fallback）

这种设计的好处：一个慢工具（比如发邮件需要5秒）不会阻塞其他请求。

### 6.6 为什么不用 HTTP 端口？

stdio 方式的优势：

| 优势 | 说明 |
|------|------|
| **零配置** | 不需要选端口、处理端口冲突、配置防火墙 |
| **天然安全** | 管道只存在于父子进程之间，外部无法访问 |
| **简单可靠** | 不需要HTTP服务器、路由、中间件 |
| **适合本地工具** | MCP 服务是本地辅助工具，不需要对外暴露 |

如果需要远程访问（比如团队共享），MCP 也支持 HTTP 传输（`StreamableHTTPServer`），配置中把 `type` 改为 `"remote"` 并指定 `url`，但本地工具用 stdio 就够了。

---

## 7. SMTP 邮件发送详解

### SMTP 协议对话流程

SMTP 是一个"问答式"协议，每一步都有确认：

```
客户端                          服务器
  │                               │
  │── TLS连接建立 ──────────────▶ │  （端口465，隐式TLS）
  │                               │
  │── EHLO ────────────────────▶ │  （打招呼，告知身份）
  │◀── 250 OK ────────────────── │
  │                               │
  │── AUTH PLAIN ──────────────▶ │  （认证：邮箱+授权码）
  │◀── 235 Authentication ok ── │
  │                               │
  │── MAIL FROM ───────────────▶ │  （设置发件人）
  │◀── 250 OK ────────────────── │
  │                               │
  │── RCPT TO ─────────────────▶ │  （设置收件人）
  │◀── 250 OK ────────────────── │
  │                               │
  │── DATA ────────────────────▶ │  （开始传输邮件内容）
  │◀── 354 End data with <CR><LF>.<CR><LF> │
  │                               │
  │── 邮件内容 ─────────────────▶ │  （From/To/Subject头部+空行+正文）
  │── .<CR><LF> ───────────────▶ │  （结束标记）
  │◀── 250 OK ────────────────── │
  │                               │
  │── QUIT ────────────────────▶ │  （优雅关闭）
  │◀── 221 Bye ───────────────── │
```

### 隐式TLS vs 显式TLS

| 端口 | 方式 | 流程 | 适用场景 |
|------|------|------|----------|
| 465 | 隐式TLS | `tls.Dial` → `smtp.NewClient` | QQ邮箱、163邮箱 |
| 587 | 显式TLS (STARTTLS) | `net.Dial` → `smtp.NewClient` → `StartTLS` | Gmail、企业邮箱 |

我们的代码使用465端口（隐式TLS），因为国内主流邮箱默认支持465。

---

## 8. 常见邮箱 SMTP 配置参考

| 邮箱 | SMTP_HOST | SMTP_PORT | 授权码获取方式 |
|------|-----------|-----------|---------------|
| QQ邮箱 | smtp.qq.com | 465 | 邮箱设置 → POP3/SMTP → 生成授权码 |
| 163邮箱 | smtp.163.com | 465 | 邮箱设置 → POP3/SMTP → 设置客户端授权密码 |
| 126邮箱 | smtp.126.com | 465 | 同163邮箱 |
| Gmail | smtp.gmail.com | 587 | Google账号 → 安全 → 应用密码 |
| Outlook | smtp.office365.com | 587 | 账号安全 → 应用密码 |

---

## 9. 总结

本文覆盖了 MCP 本地服务开发的完整链路：

1. **MCP 协议理解**：工具注册、JSON-RPC 通信、CallToolResult 返回格式
2. **代码架构**：三层分离（协议层/配置层/业务层），职责清晰
3. **配置注入**：环境变量由 OpenCode 自动注入，敏感信息不硬编码
4. **通信机制**：stdio 管道通信，不是HTTP端口，子进程随父进程生命周期
5. **SMTP 发送**：7步协议对话，隐式TLS，邮件格式规范
6. **并发模型**：Worker Pool 处理工具调用，避免阻塞

**一句话总结：MCP 服务就是一个"翻译官"——AI 客户端说 JSON-RPC，MCP 框架翻译成 Go 函数调用，Go 函数再通过 SMTP 协议跟邮件服务器对话。整个服务通过 stdin/stdout 管道与 AI 客户端通信，随 OpenCode 的启动而启动，随 OpenCode 的关闭而退出。**