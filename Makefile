.PHONY: run run-mem test build web web-build smoke

# 生产模式（需 Postgres，见 docker-compose.yml）
run:
	go run ./cmd/fleetd --seed

# 零依赖演示模式（内存存储 + mock/本地模型）
run-mem:
	go run ./cmd/fleetd --mem --seed

test:
	go test ./... -count=1 -race

build:
	go build ./...

web:
	cd web && npm run dev

web-build:
	cd web && npm run build

smoke:
	go build ./... && go test ./... -count=1 && cd web && npm run build

# 单二进制交付：前端嵌入 fleetd（一个 binary + 一个 Postgres 即部署）
release:
	cd web && npm run build
	go build -tags release -o fleetd-release ./cmd/fleetd
	@ls -lh fleetd-release | awk '{print "产物: " $$9, $$5}'

# Eino 适配器构建（F7）：eino runner 可用（gateway 桥接模式）
eino:
	go build -tags eino -o fleetd-eino ./cmd/fleetd
	go test -tags eino ./internal/runner/ -run TestEino -count=1
