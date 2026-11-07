package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// API Key 认证（I19）：渐进安全——api_keys 表为空时开放（引导友好），
// 一旦存在 key 即强制校验（自助启用的安全升级，不破坏现有部署）。
// 明文只在创建响应出现一次，库存 SHA-256 哈希。限流键随之从 IP 升级为 key。

// generateAPIKey 生成 fleet- 前缀的随机密钥。
func generateAPIKey() (plaintext, prefix string, err error) {
	buf := make([]byte, 24)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	plaintext = "fleet-" + hex.EncodeToString(buf)
	prefix = plaintext[:14]
	return plaintext, prefix, nil
}

func hashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// authMiddleware 认证中间件：authRequired 时校验 X-API-Key。
// 豁免：/metrics（Prometheus 抓取）、/api/v1/health（探活）、/api/v1/auth/keys*（管理端点需开放以引导首把 key）。
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authRequired() {
			next.ServeHTTP(w, r)
			return
		}
		if isAuthExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		key := strings.TrimSpace(r.Header.Get("X-API-Key"))
		if key == "" {
			writeErr(w, http.StatusUnauthorized, errors.New("缺少 X-API-Key 请求头（认证已启用）"))
			return
		}
		found, err := s.Store.FindAPIKeyByHash(r.Context(), hashKey(key))
		if err != nil {
			writeErr(w, http.StatusForbidden, errors.New("API key 无效"))
			return
		}
		_ = s.Store.TouchAPIKey(r.Context(), found.ID)
		r = r.WithContext(context.WithValue(r.Context(), clientKeyCtx{}, "key:"+found.Name))
		next.ServeHTTP(w, r)
	})
}

func isAuthExempt(path string) bool {
	return path == "/metrics" || path == "/api/v1/health" || strings.HasPrefix(path, "/api/v1/auth/keys")
}

func (s *Server) authRequired() bool {
	has, _ := s.Store.HasAnyAPIKey(context.Background())
	return has
}

// ---- 管理端点 ----

type keyCreateIn struct {
	Name string `json:"name"`
}

func (s *Server) createAuthKey(w http.ResponseWriter, r *http.Request) {
	var in keyCreateIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name 必填"))
		return
	}
	plaintext, prefix, err := generateAPIKey()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	id, err := s.Store.CreateAPIKey(r.Context(), in.Name, hashKey(plaintext), prefix)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": in.Name, "key": plaintext,
		"warning": "明文仅此一次展示，请立即保存；库存为 SHA-256 哈希"})
}

func (s *Server) listAuthKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.ListAPIKeys(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys, "total": len(keys)})
}

func (s *Server) revokeAuthKey(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteAPIKey(r.Context(), pathID(r)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pathID(r), "revoked": true})
}

// clientKeyCtx 限流键上下文标记：认证成功后限流键从 IP 升级为 key 名（F8 升级路径闭合）。
type clientKeyCtx struct{}
