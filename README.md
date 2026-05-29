# sub2api-fast-proxy

一个轻量 Go 反向代理，用来给 Sub2API 前面挂一个 fast 专用端点。

默认行为：

- 上游：必须通过 `UPSTREAM_URL` 显式配置
- 监听：`:8787`
- 对 `/v1/responses`、`/v1/chat/completions`、`/v1/completions` 的 JSON body 强制写入 `service_tier: "priority"`
- 如果请求存在重复的顶层 `service_tier`，直接拒绝请求，避免后写字段绕过 fast
- 对 `/v1/messages` 自动追加 `anthropic-beta: fast-mode-2026-02-01`
- 保留用户原始 `Authorization`、SSE/streaming 响应和 WebSocket upgrade

## 本地运行

```bash
cd ~/github/sub2api-fast-proxy
UPSTREAM_URL=http://127.0.0.1:8080 go run ./cmd/sub2api-fast-proxy
```

健康检查：

```bash
curl -s http://127.0.0.1:8787/healthz
```

真实请求示例：

```bash
curl http://127.0.0.1:8787/v1/responses \
  -H 'Authorization: Bearer sk-...' \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-5.5","input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}'
```

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `LISTEN_ADDR` | `:8787` | 监听地址 |
| `UPSTREAM_URL` | 必填 | Sub2API 上游地址 |
| `UPSTREAM_HOST_HEADER` | 空 | 可选：转发给上游的 `Host`，适合用固定 IP 直连但保留域名路由 |
| `UPSTREAM_TLS_SERVER_NAME` | 空 | 可选：连接 HTTPS 固定 IP 时使用的 TLS SNI |
| `FORCE_SERVICE_TIER` | `priority` | 强制写入的 OpenAI `service_tier`；`fast` 会被归一化为 `priority` |
| `OPENAI_JSON_PATHS` | `/v1/responses,/v1/chat/completions,/v1/completions` | 需要注入 JSON body 的路径 |
| `ANTHROPIC_FAST_PATHS` | `/v1/messages` | 需要追加 Anthropic fast beta 的路径 |
| `ANTHROPIC_FAST_BETA` | `fast-mode-2026-02-01` | Anthropic fast beta token |
| `MAX_BODY_BYTES` | `256mb` | 单个可注入请求体上限 |
| `STRICT_INJECTION` | `true` | 注入失败时拒绝请求，防止误以为走了 fast |
| `LOG_LEVEL` | `info` | `debug`、`info`、`warn`、`error` |
| `LOG_FORMAT` | `text` | 设为 `json` 输出 JSON 日志 |

## 构建

```bash
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/sub2api-fast-proxy ./cmd/sub2api-fast-proxy
```

## Docker

本地构建：

```bash
docker build -t sub2api-fast-proxy:test .
docker run --rm -p 127.0.0.1:8787:8787 \
  --add-host host.docker.internal:host-gateway \
  -e UPSTREAM_URL=http://host.docker.internal:8080 \
  sub2api-fast-proxy:test
```

## GHCR 发布

仓库内置 GitHub Actions：`.github/workflows/docker-build.yml`。

触发方式：

- push 到 `main`：发布 `ghcr.io/<owner>/<repo>:main` 和 `:latest`
- push `v*` tag：发布对应 tag，例如 `ghcr.io/akuma-real/sub2api-fast-proxy:v0.1.0`
- 手动 `workflow_dispatch`

仓库需要允许 Actions 写入 Packages。镜像默认名会自动转小写。

## Docker Compose 部署

```bash
cd ~/deploy/sub2api-fast-proxy
cp ~/github/sub2api-fast-proxy/deploy/docker-compose.yml .
cp ~/github/sub2api-fast-proxy/deploy/.env.example .env
docker compose pull
docker compose up -d
docker compose ps
```

默认 `.env`：

```dotenv
IMAGE=ghcr.io/akuma-real/sub2api-fast-proxy:latest
UPSTREAM_URL=http://sub2api:8080
UPSTREAM_HOST_HEADER=
UPSTREAM_TLS_SERVER_NAME=
BIND_ADDR=127.0.0.1
HOST_PORT=8787
MEM_LIMIT=512m
MEM_RESERVATION=128m
```

如果上游域名在容器内 DNS 不稳定，或者希望直连后端 IP，可以这样配置：

```dotenv
UPSTREAM_URL=https://149.104.5.18
UPSTREAM_HOST_HEADER=sub.ggapi.cc
UPSTREAM_TLS_SERVER_NAME=sub.ggapi.cc
```

只暴露 `127.0.0.1:8787`，公网入口交给 Caddy。

## Caddy 反代

Caddy：

```caddyfile
fast.example.com {
	encode zstd gzip

	reverse_proxy 127.0.0.1:8787 {
		header_up Host {host}
		header_up X-Real-IP {remote_host}
		header_up X-Forwarded-For {remote_host}
		header_up X-Forwarded-Proto {scheme}
		header_up X-Forwarded-Host {host}
	}
}
```

配置后 reload：

```bash
caddy validate --config /etc/caddy/Caddyfile
systemctl reload caddy
```
