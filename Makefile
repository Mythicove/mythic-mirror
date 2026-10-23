.PHONY: run build test migrate generate check-spec

run:
	go run ./cmd/server

build:
	go build -o bin/mirror ./cmd/server

test:
	go test ./...

migrate:
	goose -dir migrations postgres "$${MIRROR_DATABASE_URL}" up

# 代码生成：sqlc（internal/pgstore/query.sql → internal/pgstore/db）。
# OpenAPI 契约的 TS 类型在 web 仓库生成（pnpm gen:api，ADR-0009）；
# Go 侧暂未采用 oapi-codegen，spec↔路由一致性由漂移测试保证。
generate:
	sqlc generate

# 契约漂移门：spec 里记录的每个操作必须真实注册在路由上。
# 默认在 ../api/specs 找契约，可用 MYTHICOVE_API_SPECS 覆盖。
check-spec:
	go test ./internal/httpapi/ -run TestSpecMatchesRoutes -count=1 -v
