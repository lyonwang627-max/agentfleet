package runner

// ShutdownMCP 优雅退出时关闭全部 MCP 子进程连接。
func ShutdownMCP() {
	mcpMu.Lock()
	defer mcpMu.Unlock()
	for name, c := range mcpServers {
		c.Close()
		delete(mcpServers, name)
	}
}
