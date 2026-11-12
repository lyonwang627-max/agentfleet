// Package webhook 事件订阅出站推送（I7，FDE 集成叙事）：
// 任务 done/failed 等事件回调客户系统 URL——HMAC-SHA256 签名防伪造，事件过滤，
// 投递失败记录状态（重试退避第 2 轮）。
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// Matches 事件是否命中订阅（"*" 通配）。
func Matches(h *model.Webhook, kind string) bool {
	for _, e := range h.Events {
		if e == "*" || e == kind {
			return true
		}
	}
	return false
}

// Sign HMAC-SHA256：hex(secret, body)——接收方用同一 secret 重算校验。
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// DeliveryID 幂等投递标识：同一事件对同一 hook 的重试链共享该 ID——
// 接收方按 ID 去重（at-least-once 语义下重复投递的防线，对齐 Kafka offset 惯例）。
func DeliveryID(h *model.Webhook, event model.Event) string {
	return Sign(h.Secret, []byte(fmt.Sprintf("%d:%d:%s", h.ID, event.Seq, event.Kind)))
}

// Deliver 单次投递：POST JSON + 签名头（X-Fleet-Signature / X-Fleet-Event / X-Fleet-Delivery / 时间戳）。
// 纯函数便于单测（client 可注入 httptest）。
func Deliver(ctx context.Context, client *http.Client, h *model.Webhook, event model.Event, deliveryID string) error {
	if !h.Active || !Matches(h, event.Kind) {
		return nil // 未订阅：静默跳过（不是错误）
	}
	body, err := json.Marshal(map[string]any{
		"kind":      event.Kind,
		"seq":       event.Seq,
		"data":      event.Data,
		"timestamp": event.CreatedAt,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fleet-Event", event.Kind)
	req.Header.Set("X-Fleet-Delivery", deliveryID) // I7 第 3 轮：幂等去重依据（重试链内稳定）
	req.Header.Set("X-Fleet-Timestamp", fmt.Sprint(time.Now().Unix()))
	req.Header.Set("X-Fleet-Signature", Sign(h.Secret, body))
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook 投递失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 投递失败: http %d", resp.StatusCode)
	}
	return nil
}

// IngestToken 入站回调令牌（I37，Dify Endpoint 类型对读）：
// 从 hook secret 派生（无需新列）——外部系统持有 token 即可回调建任务；
// secret 本体不出创建响应之外，token 可独立轮换分发。
func IngestToken(h *model.Webhook) string {
	sum := sha256.Sum256([]byte("ingest:" + h.Secret))
	return hex.EncodeToString(sum[:])[:24]
}
