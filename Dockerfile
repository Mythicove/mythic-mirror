# ---- build ----
FROM golang:1.26-alpine AS build
WORKDIR /src
# proxy.golang.org 在大陆不可达；goproxy.cn 为国内公共 Go 代理（全球亦可用）
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o /mirror ./cmd/server

# ---- run ----
FROM alpine:3.20
RUN adduser -D -u 10001 app && mkdir -p /data && chown app:app /data
COPY --from=build /mirror /usr/local/bin/mirror
USER app
EXPOSE 8080
ENTRYPOINT ["mirror"]
