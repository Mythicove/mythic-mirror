.PHONY: run build test migrate generate

run:
	go run ./cmd/server

build:
	go build -o bin/mirror ./cmd/server

test:
	go test ./...

migrate:
	goose -dir migrations postgres "$${MIRROR_DATABASE_URL}" up

# M1 起：从 api 仓库按固定 tag/commit 拉 spec，oapi-codegen 生成 Go server 类型（ADR-0009）
generate:
	@echo "TODO: oapi-codegen (M1)"
