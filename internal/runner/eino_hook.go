package runner

import "github.com/lyonwang/agentfleet/internal/gateway"

// EinoRunnerFactory 仅在 `-tags eino` 构建时由适配器 init 赋值；
// 主构建恒为 nil，main.go 据此决定是否注册 eino runner。
var EinoRunnerFactory func(gw *gateway.LLM) Runner
