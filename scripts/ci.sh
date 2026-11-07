#!/bin/bash
# CI 一键校验（Q3）：格式 → vet → 全量测试（-race）→ 前端构建
# 零外部工具链依赖（gofmt/go vet 为 Go 自带）；golangci-lint 引入后在此追加。
set -e
cd "$(dirname "$0")/.."

echo "── 1/4 gofmt 校验 ──"
UNFORMATTED=$(gofmt -l ./cmd ./internal)
if [ -n "$UNFORMATTED" ]; then
  echo "以下文件未格式化："
  echo "$UNFORMATTED"
  exit 1
fi
echo "ok"

echo "── 2/4 go vet ──"
go vet ./...
echo "ok"

echo "── 3/4 go test（-race）──"
go test ./... -count=1 -race
echo "ok"

echo "── 4/4 前端构建 ──"
cd web
npm run build --silent
cd ..
echo "ok"

echo ""
echo "✅ CI 全部通过"
