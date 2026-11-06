//go:build !release

// Package web 前端交付：默认（开发模式）不嵌入，前端独立 Vite dev server / 静态托管。
package web

import "net/http"

// DistHandler 单二进制交付模式（go build -tags release，需先 npm run build 产出 dist）。
func DistHandler() (http.Handler, bool) { return nil, false }
