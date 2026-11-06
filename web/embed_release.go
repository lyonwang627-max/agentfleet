//go:build release

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distEmbed embed.FS

// DistHandler release 构建：前端 dist 嵌入二进制，根路径服务 SPA（客户端路由回退 index.html）。
// 一个 binary + 一个 Postgres 即完成交付（FDE 私有化部署叙事）。
func DistHandler() (http.Handler, bool) {
	sub, err := fs.Sub(distEmbed, "dist")
	if err != nil {
		return nil, false
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(sub, p); err != nil {
				r.URL.Path = "/" // SPA 客户端路由：未命中静态文件一律回退 index.html
			}
		}
		fileServer.ServeHTTP(w, r)
	}), true
}
