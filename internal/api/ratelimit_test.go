package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 令牌桶算法（F8-a）：burst 内全过、耗尽后 429、时间回填、闲置回收。
func TestTokenBucketAllow(t *testing.T) {
	b := &tokenBucket{tokens: 3, last: time.Now(), rate: 60.0 / 60.0, burst: 3} // 1/s，burst 3
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !b.allow(now) {
			t.Fatalf("第 %d 次应放行（burst 内）", i+1)
		}
	}
	if b.allow(now) {
		t.Fatal("耗尽后应拒绝")
	}
	// 1 秒后回填 1 个 token
	if !b.allow(now.Add(time.Second)) {
		t.Fatal("回填后应放行")
	}
}

func TestRateLimiterIPIsolationAnd429(t *testing.T) {
	rl := NewRateLimiter(2) // 2 次/分钟
	if rl.Allow("1.1.1.1") == false || rl.Allow("1.1.1.1") == false {
		t.Fatal("burst 内应放行")
	}
	if rl.Allow("1.1.1.1") {
		t.Fatal("第三个请求应拒绝")
	}
	if !rl.Allow("2.2.2.2") {
		t.Fatal("另一 IP 不应被连带限流")
	}
	// nil 限流器 = 关闭，恒放行
	var nilRL *RateLimiter
	if !nilRL.Allow("3.3.3.3") {
		t.Fatal("未启用限流应恒放行")
	}
}

func TestRateLimitMiddleware429(t *testing.T) {
	s := &Server{limiter: NewRateLimiter(1)}
	h := s.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// 同 IP 第一次过、第二次 429
	r1 := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	r1.RemoteAddr = "9.9.9.9:1234"
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("首次应 200，得到 %d", w1.Code)
	}
	r2 := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	r2.RemoteAddr = "9.9.9.9:1235"
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("第二次应 429，得到 %d", w2.Code)
	}
	// GET 不受限流
	r3 := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	r3.RemoteAddr = "9.9.9.9:1236"
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if w3.Code != http.StatusOK {
		t.Fatalf("GET 不应被限流，得到 %d", w3.Code)
	}
}
