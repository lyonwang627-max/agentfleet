package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 网关限流（F8-a）：IP 级 token bucket，只限变更类端点（POST/PATCH/DELETE），查询不受限。
// 默认关闭（rate=0），FLEET_RATE_LIMIT（次/分钟）开启。
// 升级路径：引入 API key 认证后限流键从 IP 换 key（桶逻辑不变）；分布式部署换 Redis 后端。

type tokenBucket struct {
	tokens float64
	last   time.Time
	rate   float64 // tokens per second
	burst  float64
}

func (b *tokenBucket) allow(now time.Time) bool {
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimiter IP → bucket；闲置桶定期回收防内存膨胀。
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     float64 // per second
	burst    float64
	perMin   int
	lastGC   time.Time
	rejected atomic.Int64 // 累计拒绝数（用量看板标红依据）
}

// Stats 限流状态（观测端点用）。
func (rl *RateLimiter) Stats() (enabled bool, perMinute int, rejected int64) {
	if rl == nil {
		return false, 0, 0
	}
	return true, rl.perMin, rl.rejected.Load()
}

func NewRateLimiter(perMinute int) *RateLimiter {
	if perMinute <= 0 {
		return nil
	}
	return &RateLimiter{
		buckets: map[string]*tokenBucket{},
		rate:    float64(perMinute) / 60.0,
		burst:   float64(perMinute),
		perMin:  perMinute,
		lastGC:  time.Now(),
	}
}

// Allow 判定该 IP 是否放行；限流器未启用恒 true。
func (rl *RateLimiter) Allow(ip string) bool {
	if rl == nil {
		return true
	}
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 每 10 分钟回收一次 10 分钟未活跃的桶
	if now.Sub(rl.lastGC) > 10*time.Minute {
		for k, b := range rl.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.lastGC = now
	}

	b, ok := rl.buckets[ip]
	if !ok {
		b = &tokenBucket{tokens: rl.burst, last: now, rate: rl.rate, burst: rl.burst}
		rl.buckets[ip] = b
	}
	return b.allow(now)
}

// clientIP 取真实客户端 IP（X-Forwarded-For 优先，反代场景）。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

// limiterKey 限流键：认证成功后为 key 名（auth 中间件写入 ctx），否则为 IP。
func limiterKey(r *http.Request) string {
	if v, ok := r.Context().Value(clientKeyCtx{}).(string); ok {
		return v
	}
	return clientIP(r)
}

// rateLimitMiddleware 限流中间件：仅对变更类方法生效；拒绝时 429 + Retry-After。
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	if s.limiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPatch, http.MethodDelete:
			if !s.limiter.Allow(limiterKey(r)) { // 认证后按 key 名限流，否则按 IP（F8 升级路径）
				s.limiter.rejected.Add(1)
				w.Header().Set("Retry-After", strconv.Itoa(60))
				writeErr(w, http.StatusTooManyRequests, errors.New("请求过于频繁，稍后再试（限流：变更类接口按 IP 令牌桶）"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
