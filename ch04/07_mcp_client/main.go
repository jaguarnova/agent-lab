// ch04/07_mcp_client/main.go
// 阶段四练习 1：不依赖 SDK，手写一个最小 MCP Client（stdio 传输）。
//
// MCP 协议要点（JSON-RPC 2.0 over stdio）：
//   - 传输：Client 与 Server 之间是两条 stdin/stdout 管道，每条消息一行 JSON
//   - 握手：initialize（协商协议版本与能力）→ initialized（通知）→ 正常调用
//   - 调用：tools/list（发现工具）→ tools/call（执行）
//   - 消息 ID：request 带 id，response 用同 id 对应；notification 无 id
//
// 本练习演示完整生命周期：spawn server → initialize → tools/list → tools/call → shutdown
//
// 前置：npx（Node.js）。官方 filesystem server 用 npx 拉起。
// 用法：go run ./ch04/07_mcp_client <被读的文件路径> [mcp server args...]
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// ---------- JSON-RPC 2.0 结构 ----------

type RPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// ---------- MCP 类型 ----------

// Tool 是 tools/list 返回的工具定义——和 OpenAI function calling 的 schema 几乎同构。
// 这就是 MCP 的核心价值：一套工具描述标准，跨 Client 复用。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type CallToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// ---------- MCP Client ----------

type MCPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID atomic.Int64
	mu     sync.Mutex // 一把锁串行化请求-响应对（MCP stdio 允许并发，练习从简）
}

// Start 启动 MCP server 子进程并完成握手（initialize + initialized）
func Start(command string, args ...string) (*MCPClient, error) {
	cmd := exec.Command(command, args...)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr // server 的日志直接透传，避免静默
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 MCP server 失败: %w", err)
	}

	c := &MCPClient{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}

	// 握手：initialize（必须带协议版本与能力声明）
	// 协议版本按日期命名（如 2024-11-05），client/server 协商取共同支持的最高版本
	initResult, err := c.Call("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "agent-lab-client", "version": "0.1.0"},
	})
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize 失败: %w", err)
	}
	var serverInfo struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	json.Unmarshal(initResult, &serverInfo)

	// initialized 是通知（无 id，不需要响应），标志着握手完成
	if err := c.Notify("notifications/initialized", nil); err != nil {
		c.Close()
		return nil, err
	}
	fmt.Printf("✓ 握手完成，server: %s %s\n", serverInfo.ServerInfo.Name, serverInfo.ServerInfo.Version)
	return c, nil
}

// Call 发送请求并等待同 ID 响应
func (c *MCPClient) Call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)
	req, _ := json.Marshal(RPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if _, err := c.stdin.Write(append(req, '\n')); err != nil { // 行分隔：一条消息一个 \n
		return nil, err
	}

	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("读 server 响应失败: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var resp RPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue // 忽略非 JSON 行（server 日志等）
		}
		if resp.ID != id {
			continue // 不是本请求的响应（notification 或其他并发请求），继续读
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("RPC 错误 %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// Notify 发送通知（无 id，无响应）
func (c *MCPClient) Notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	_, err := c.stdin.Write(append(req, '\n'))
	return err
}

// ListTools 发现 server 提供的工具
func (c *MCPClient) ListTools() ([]Tool, error) {
	result, err := c.Call("tools/list", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return nil, err
	}
	return r.Tools, nil
}

// CallTool 执行工具。isError=true 表示工具业务失败（区别于协议失败）
func (c *MCPClient) CallTool(name string, args map[string]any) (string, error) {
	result, err := c.Call("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var r CallToolResult
	if err := json.Unmarshal(result, &r); err != nil {
		return "", err
	}
	var texts []string
	for _, ct := range r.Content {
		if ct.Type == "text" {
			texts = append(texts, ct.Text)
		}
	}
	out := strings.Join(texts, "\n")
	if r.IsError {
		return out, fmt.Errorf("工具返回错误: %s", out)
	}
	return out, nil
}

func (c *MCPClient) Close() {
	c.stdin.Close()
	c.cmd.Process.Kill()
	c.cmd.Wait()
}

// ---------- 演示流程 ----------

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: go run ./ch04/07_mcp_client <要读的文件> [额外 server 参数如目录]")
		os.Exit(1)
	}
	target := os.Args[1]
	dir := "."
	if len(os.Args) > 2 {
		dir = os.Args[2]
	}

	// 官方 filesystem server：npx 拉起，限定可访问目录（安全边界由 server 执行）
	client, err := Start("npx", "-y", "@modelcontextprotocol/server-filesystem", dir)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// 1. 发现：这个 server 有哪些工具？
	tools, err := client.ListTools()
	if err != nil {
		panic(err)
	}
	fmt.Printf("✓ 发现 %d 个工具：\n", len(tools))
	for _, t := range tools {
		fmt.Printf("  - %s: %s\n", t.Name, firstLine(t.Description))
	}

	// 2. 调用：read_file 读指定文件——注意 Client 没有实现任何文件逻辑
	argsJSON, _ := json.Marshal(map[string]any{"path": target})
	fmt.Printf("\n✓ 调用 read_file(%s):\n", target)
	out, err := client.CallTool("read_file", map[string]any{"path": target})
	if err != nil {
		fmt.Printf("  失败: %v\n（提示：路径必须限定在目录 %s 内，这是 server 侧的安全边界）\n", err, dir)
		return
	}
	fmt.Printf("%s\n（共 %d 字节）\n", firstLines(out, 5), len(argsJSON)+len(out))
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i > 0 {
		return s[:i]
	}
	return s
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
