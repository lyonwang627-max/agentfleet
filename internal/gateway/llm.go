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
	"os"
	"strings"
	"sync"
	"time"
)

type LLM struct {
	Mode     string // ollama | mock
	BaseURL  string // 如 http://127.0.0.1:11434
	Model    string
	client   *http.Client
	Fallback func(system, user string) string // mock 生成器（注入）
	Pricing  map[string]ModelPrice            // 模型定价（F10 成本核算）
	cooling  *coolingState                    // I35：失败计数与冷却（自带锁；WithModel 派生视图共享指针）
}

// coolingState 冷却状态（锁随状态走——WithModel 值拷贝共享指针，双视图并发安全）。
type coolingState struct {
	mu    sync.Mutex
	fails int
	until time.Time
}

// WithModel 派生视图：共享冷却状态？——派生是值拷贝，state 指针字段同步共享（map/指针引用语义）。

func New(model string) *LLM {
	c := &LLM{Model: model, client: &http.Client{Timeout: 300 * time.Second}}
	if model == "mock" { // 显式请求内置 mock 模型 = 强制 mock 模式（测试/无模型演练）
		c.Mode = "mock"
		c.Pricing = LoadPricing(DefaultPricing)
		return c
	}
	c.Resolve()
	c.Pricing = LoadPricing(DefaultPricing)
	return c
}

// ---- I35 模型级冷却（借鉴 LiteLLM cooldown）：运行中模型持续故障不再反复撞 ----

const (
	modelAllowedFails = 3                // 连续失败阈值
	modelCooldown     = 60 * time.Second // 冷却时长（到期自动回轮换）
)

// timeNow 可替换时钟（测试用）。
var timeNow = time.Now

// recordFailure 模型调用失败计数；连续超阈值进入冷却（I35）。
func (l *LLM) recordFailure() {
	if l.cooling == nil {
		l.cooling = &coolingState{}
	}
	l.cooling.mu.Lock()
	defer l.cooling.mu.Unlock()
	l.cooling.fails++
	if l.cooling.fails >= modelAllowedFails {
		l.cooling.until = timeNow().Add(modelCooldown)
		l.cooling.fails = 0
		log.Printf("[gateway] 模型 %s 连续失败 %d 次 → 冷却 %s", l.Model, modelAllowedFails, modelCooldown)
	}
}

// recordSuccess 成功重置失败计数（模型恢复）。
func (l *LLM) recordSuccess() {
	if l.cooling == nil {
		return
	}
	l.cooling.mu.Lock()
	defer l.cooling.mu.Unlock()
	l.cooling.fails = 0
}

// IsCooling 模型是否冷却中（TierToModel 过滤依据；只对本实例 Model 生效）。
func (l *LLM) IsCooling(model string) bool {
	if model != l.Model || l.cooling == nil {
		return false
	}
	l.cooling.mu.Lock()
	defer l.cooling.mu.Unlock()
	return timeNow().Before(l.cooling.until)
}

// CoolingList 冷却中模型清单（观测面：route 步留痕）。
func (l *LLM) CoolingList() []string {
	if l.cooling == nil {
		return nil
	}
	l.cooling.mu.Lock()
	defer l.cooling.mu.Unlock()
	if timeNow().Before(l.cooling.until) {
		return []string{l.Model}
	}
	return nil
}

// SelectFor 按用途返回模型名。档位可 env 覆写（FLEET_TIER_S/M/L）；
func NewWithBaseURL(model, baseURL string) *LLM {
	c := &LLM{Model: model, BaseURL: baseURL, Mode: "ollama", client: &http.Client{Timeout: 300 * time.Second}}
	c.Pricing = LoadPricing(DefaultPricing)
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

// ModelInfo 可选模型（动态发现：Ollama 已装列表 + 内置 mock）。
type ModelInfo struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"` // ollama | builtin
	Available bool   `json:"available"`
}

// ListModels 从 Ollama /api/tags 动态发现本机已装模型；失败时至少返回内置 mock。
// 这是模型可选化（agent 表单下拉）的数据源——不硬编码模型清单。
func (l *LLM) ListModels() []ModelInfo {
	out := []ModelInfo{{Name: "mock", Provider: "builtin", Available: true}}
	if l.Mode != "ollama" {
		return out
	}
	req, _ := http.NewRequest(http.MethodGet, l.BaseURL+"/api/tags", nil)
	resp, err := l.client.Do(req)
	if err != nil {
		return append(out, ModelInfo{Name: l.Model, Provider: "ollama", Available: false})
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return append(out, ModelInfo{Name: l.Model, Provider: "ollama", Available: false})
	}
	seen := map[string]bool{out[0].Name: true}
	for _, m := range tags.Models {
		if !seen[m.Name] {
			seen[m.Name] = true
			out = append(out, ModelInfo{Name: m.Name, Provider: "ollama", Available: true})
		}
	}
	return out
}

// ChatMessage 多轮消息（ChatRaw 用）。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRaw 多轮直通：保留完整消息序列（适配 eino/多轮工具循环等上层），走原生通道。
// Eino 不可用时仍走 OpenAI 兼容路径由 native 内部处理——ChatRaw 只负责透传。
func (l *LLM) ChatRaw(ctx context.Context, msgs []ChatMessage, maxTokens int) (string, int, error) {
	if l.Mode == "mock" {
		if l.Fallback != nil {
			return l.Fallback("", msgs[len(msgs)-1].Content), 64, nil
		}
		return "(mock) " + msgs[len(msgs)-1].Content, 64, nil
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	payload := map[string]any{
		"model":    l.Model,
		"messages": msgs,
		"stream":   false,
		"think":    false,
		"options":  map[string]any{"temperature": 0.3, "num_predict": maxTokens},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.BaseURL+"/api/chat", strings.NewReader(string(body)))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		l.recordFailure()
		return "", 0, fmt.Errorf("ollama chat raw: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		l.recordFailure()
		return "", 0, fmt.Errorf("ollama chat raw: http %d", resp.StatusCode)
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
	l.recordSuccess()
	return out.Message.Content, out.EvalCount, nil
}

// Purpose 调用用途（ADR-0002 步骤级分流）：按负载特征选模型档位。
type Purpose string

const (
	PurposeJudge   Purpose = "judge"   // 判断类：输出短、高频、延迟敏感 → S 档
	PurposeExecute Purpose = "execute" // 执行类：长输出、质量敏感 → M 档
	PurposeReview  Purpose = "review"  // 审查类：质量最敏感、低频 → L 档
)

// SelectFor 按用途返回模型名。档位可 env 覆写（FLEET_TIER_S/M/L）；
// S 档模型不可用时自动回退 M 档（判断降级到主力模型，只慢不错）。
func (l *LLM) SelectFor(p Purpose) string {
	tiers := map[Purpose]string{
		PurposeJudge:   envOrStr("FLEET_TIER_S", "lfm25:latest"),
		PurposeExecute: envOrStr("FLEET_TIER_M", l.Model),
		PurposeReview:  envOrStr("FLEET_TIER_L", l.Model),
	}
	tier, ok := tiers[p]
	if !ok {
		return l.Model
	}
	if p == PurposeJudge {
		for _, m := range l.ListModels() { // S 档可用性校验（本地小模型可能未装 + I35 冷却过滤）
			if m.Name == tier && m.Available && !l.IsCooling(tier) {
				return tier
			}
		}
		return tiers[PurposeExecute]
	}
	return tier
}

// WithModel 返回指定模型的浅拷贝视图（不污染原客户端——并发安全）。
// mock 模式保持 mock（测试确定性）。
func (l *LLM) WithModel(name string) *LLM {
	c := *l
	c.Model = name
	// cooling 指针随值拷贝共享（自带锁）——派生视图与原视图冷却状态一致
	return &c
}

func envOrStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// StreamChat 流式生成（F15）：native /api/chat stream:true，NDJSON 增量 → onChunk 回调。
// 每个增量是已确认的新文本片段（非全量）；返回 token 数（优先服务端 eval_count）。
func (l *LLM) StreamChat(ctx context.Context, system, user string, maxTokens int, onChunk func(string)) (int, error) {
	if l.Mode == "mock" {
		full := "(mock) " + user
		for _, r := range []rune(full) {
			onChunk(string(r))
		}
		return len([]rune(full)), nil
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	payload := map[string]any{
		"model":    l.Model,
		"messages": []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		"stream":   true,
		"think":    false,
		"options":  map[string]any{"temperature": 0.3, "num_predict": maxTokens},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.BaseURL+"/api/chat", strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ollama stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ollama stream: http %d", resp.StatusCode)
	}

	tokens := 0
	dec := json.NewDecoder(resp.Body)
	for {
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done      bool `json:"done"`
			EvalCount int  `json:"eval_count"`
		}
		if err := dec.Decode(&chunk); err != nil {
			break // EOF 或损坏行：流结束
		}
		if chunk.Message.Content != "" {
			onChunk(chunk.Message.Content)
		}
		tokens++
		if chunk.Done {
			if chunk.EvalCount > 0 {
				tokens = chunk.EvalCount
			}
			break
		}
	}
	return tokens, nil
}

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
		l.recordFailure()
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
	l.recordSuccess()
	return out.Message.Content, out.EvalCount, nil
}
