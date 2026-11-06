// Package gateway 统一 LLM 接入：OpenAI 兼容探测 + Ollama 原生结构化通道 + mock 降级。
// 设计沿用 offer-agent 验证过的经验（2026-11）：
//   - HTTP 客户端禁用系统代理（trust_env=false），否则本地服务会被代理 120s 掐断；
//   - 结构化输出走 Ollama 原生 /api/chat（think:false + format:"json"），
//     OpenAI 兼容端点的 max_tokens 会被思考块吃掉导致 JSON 截断；
//   - 全部失败进入 mock，平台功能不因模型不可用而瘫痪。
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type LLM struct {
	Mode     string // ollama | mock
	BaseURL  string // 如 http://127.0.0.1:11434
	Model    string
	client   *http.Client
	Fallback func(system, user string) string // mock 生成器（注入）
}

func New(model string) *LLM {
	c := &LLM{Model: model, client: &http.Client{Timeout: 300 * time.Second}}
	c.Resolve()
	return c
}

func (l *LLM) Resolve() {
	for _, base := range []string{"http://127.0.0.1:11434"} {
		req, _ := http.NewRequest(http.MethodGet, base+"/api/tags", nil)
		resp, err := l.client.Do(req)
		if err == nil {
			resp.Body.Close()
			l.Mode, l.BaseURL = "ollama", base
			log.Printf("[gateway] llm resolved: ollama @ %s (model=%s)", base, l.Model)
			return
		}
	}
	l.Mode = "mock"
	log.Printf("[gateway] llm unavailable -> mock mode")
}

func (l *LLM) Healthy() bool { return l.Mode == "ollama" }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chat 普通文本生成（chat 场景）。
func (l *LLM) Chat(ctx context.Context, system, user string, maxTokens int) (string, int, error) {
	if l.Mode == "mock" {
		if l.Fallback != nil {
			return l.Fallback(system, user), 64, nil
		}
		return "(mock) " + user, 64, nil
	}
	return l.native(ctx, system, user, false, maxTokens)
}

// ChatJSON 结构化生成：Ollama 原生 format:"json"，语法级保证合法 JSON。
func (l *LLM) ChatJSON(ctx context.Context, system, user string, maxTokens int) (json.RawMessage, int, error) {
	if l.Mode == "mock" {
		if l.Fallback != nil {
			return json.RawMessage(l.Fallback(system, user)), 64, nil
		}
		return json.RawMessage(`{"reply":"(mock) 已处理任务请求。","tokens":64}`), 64, nil
	}
	raw, tokens, err := l.native(ctx, system, user, true, maxTokens)
	return json.RawMessage(raw), tokens, err
}

func (l *LLM) native(ctx context.Context, system, user string, forceJSON bool, maxTokens int) (string, int, error) {
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	payload := map[string]any{
		"model":    l.Model,
		"messages": []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		"stream":   false,
		"think":    false,
		"options":  map[string]any{"temperature": 0.3, "num_predict": maxTokens},
	}
	if forceJSON {
		payload["format"] = "json"
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.BaseURL+"/api/chat", strings.NewReader(string(body)))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("ollama chat: http %d", resp.StatusCode)
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		EvalCount int `json:"eval_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, err
	}
	return out.Message.Content, out.EvalCount, nil
}
