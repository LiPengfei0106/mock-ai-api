# Mock AI API

这是一个面向 AI API 开发、集成测试和性能测试的多协议 Mock 服务。服务使用 Go 标准库实现，不依赖外部组件，支持高并发、流式和非流式响应，并返回各平台原生格式的 Usage。

服务分为两层：模型模拟层从内置词表随机生成文本，并统一负责输出 Token 数、动态流式分块、TTFT 和 TPS 时序；协议适配层只把同一份模型输出封装为各平台的响应格式。调整模型行为时不需要修改平台协议实现。

## 快速开始

已发布的镜像同时支持 `linux/amd64` 和 `linux/arm64`。以下示例使用明确的版本标签 `riendfly/mock-ai-api:0.1.0`，并将宿主机的 `18080` 端口绑定到 `0.0.0.0`。本机可通过 `http://127.0.0.1:18080` 访问，远程客户端需要使用宿主机实际 IP。

监听 `0.0.0.0` 会允许其他主机访问服务，应通过防火墙或安全组限制来源，不要直接暴露到不可信网络。

### Docker 命令

```bash
docker run -d \
  --name mock-ai-api \
  --restart unless-stopped \
  --init \
  --read-only \
  --security-opt no-new-privileges:true \
  -p 0.0.0.0:18080:8080 \
  -e MOCK_ADDR=0.0.0.0:8080 \
  -e MOCK_MODEL=mock-gpt \
  -e MOCK_TTFT=300ms..800ms \
  -e MOCK_TPS=20..60 \
  -e MOCK_LATENCY=50ms..200ms \
  -e MOCK_OUTPUT_TOKENS=8..32 \
  riendfly/mock-ai-api:0.1.0

curl http://127.0.0.1:18080/healthz
```

### docker-compose.yml

```yaml
name: mock-ai-api

services:
  mock-ai-api:
    image: riendfly/mock-ai-api:0.1.0
    restart: unless-stopped
    init: true
    ports:
      - "0.0.0.0:18080:8080"
    environment:
      MOCK_ADDR: "0.0.0.0:8080"
      MOCK_MODEL: "mock-gpt"
      MOCK_TTFT: "300ms..800ms"
      MOCK_TPS: "20..60"
      MOCK_LATENCY: "50ms..200ms"
      MOCK_OUTPUT_TOKENS: "8..32"
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]
      interval: 10s
      timeout: 3s
      retries: 3
      start_period: 3s
    read_only: true
    security_opt:
      - no-new-privileges:true
```

将以上内容保存为 `docker-compose.yml` 后启动并检查服务：

```bash
docker compose up -d
docker compose ps
curl http://127.0.0.1:18080/healthz
```

## 支持的接口

### 通用

| 能力 | 方法 | 路径 |
| --- | --- | --- |
| 健康检查 | `GET` | `/healthz` |

### OpenAI

| 能力 | 方法 | 路径 | 流式格式 |
| --- | --- | --- | --- |
| 模型列表 | `GET` | `/v1/models` | - |
| 模型详情 | `GET` | `/v1/models/{model}` | - |
| Chat Completions | `POST` | `/v1/chat/completions` | SSE，`chat.completion.chunk` |
| Responses | `POST` | `/v1/responses` | SSE，`response.output_text.delta` |
| Completions | `POST` | `/v1/completions` | SSE，`text_completion` |
| Embeddings | `POST` | `/v1/embeddings` | - |

### Azure OpenAI

| 能力 | 方法 | 路径 | 流式格式 |
| --- | --- | --- | --- |
| Chat Completions | `POST` | `/openai/deployments/{deployment}/chat/completions` | SSE，`chat.completion.chunk` |
| Completions | `POST` | `/openai/deployments/{deployment}/completions` | SSE，`text_completion` |
| Embeddings | `POST` | `/openai/deployments/{deployment}/embeddings` | - |

### Anthropic

| 能力 | 方法 | 路径 | 流式格式 |
| --- | --- | --- | --- |
| 模型列表 | `GET` | `/v1/models` | - |
| 模型详情 | `GET` | `/v1/models/{model}` | - |
| Messages | `POST` | `/v1/messages` | Anthropic 具名 SSE |
| Token Count | `POST` | `/v1/messages/count_tokens` | - |

### Google Gemini

| 能力 | 方法 | 路径 | 流式格式 |
| --- | --- | --- | --- |
| 模型列表 | `GET` | `/v1beta/models` | - |
| 模型详情 | `GET` | `/v1beta/models/{model}` | - |
| 内容生成 | `POST` | `/v1beta/models/{model}:generateContent` | - |
| 流式内容生成 | `POST` | `/v1beta/models/{model}:streamGenerateContent` | SSE |
| Token Count | `POST` | `/v1beta/models/{model}:countTokens` | - |
| Embedding | `POST` | `/v1beta/models/{model}:embedContent` | - |
| 批量 Embedding | `POST` | `/v1beta/models/{model}:batchEmbedContents` | - |

生成、Token Count 和 Embedding 接口同时接受 `/v1` 与 `/v1beta` 前缀。

### Ollama

| 能力 | 方法 | 路径 | 流式格式 |
| --- | --- | --- | --- |
| 模型列表 | `GET` | `/api/tags` | - |
| 模型详情 | `POST` | `/api/show` | - |
| 对话生成 | `POST` | `/api/chat` | NDJSON |
| 文本生成 | `POST` | `/api/generate` | NDJSON |
| Embedding | `POST` | `/api/embed`、`/api/embeddings` | - |

### 协议识别

共享的 `/v1/models` 路径按请求特征返回对应平台结构：

| 请求特征 | 响应协议 |
| --- | --- |
| 存在 `anthropic-version` 请求头 | Anthropic |
| 存在 `x-goog-api-key` 请求头或 `key` 查询参数 | Google Gemini |
| 其他情况 | OpenAI |

服务不校验 API Key，请求中可以携带任意 `Authorization`、`x-api-key`、`anthropic-version` 或 `key` 值。

## 请求与配置

### 请求示例

请求级 Mock 字段可以精确覆盖当前请求的服务端配置。以下示例固定输入和输出 Token 数，便于压测工具稳定统计吞吐量：

```bash
curl -N http://127.0.0.1:18080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "mock-gpt",
    "messages": [{"role": "user", "content": "hello"}],
    "stream": true,
    "mock_ttft_ms": 500,
    "mock_tps": 20,
    "mock_input_tokens": 10,
    "mock_output_tokens": 16
  }'
```

`/v1/responses`、`/v1/completions` 和 `/v1/messages` 使用相同的 Mock 扩展字段。Chat Completions 流以 `chat.completion.chunk` 返回，Responses 流以 `response.output_text.delta` 返回，Completions 流以 `text_completion` 返回，Anthropic 与 Gemini 使用原生 SSE，Ollama 使用逐行 JSON。

Embeddings 返回固定的 8 维浮点向量。字符串或整数 Token 数组作为单个输入，字符串数组或二维 Token 数组作为批量输入。该向量只用于接口解析和吞吐测试，不表示真实语义。

### 请求级参数

| 请求字段 | 含义 | 限制 |
| --- | --- | --- |
| `mock_ttft_ms` | 流式首个文本 Token 前的等待时间 | `0` 到 `3600000` 毫秒 |
| `mock_tps` | 流式输出速率；`0` 表示不等待 | `0` 到 `1000000` |
| `mock_latency_ms` | 非流式响应延迟 | `0` 到 `3600000` 毫秒 |
| `mock_input_tokens` | Usage 中的输入 Token 数 | `0` 到 `1000000000` |
| `mock_output_tokens` | 实际输出及 Usage 中的输出 Token 数 | `1` 到 `1000000` |

请求级参数只接受精确值，不支持范围，并且优先于服务端环境变量。`mock_input_tokens` 没有对应的环境变量；未传入时，服务按消息、Prompt 或 Input 中 Unicode 字符数的四分之一估算。该数值仅用于产生稳定的压测指标，不等同于真实模型的分词结果。

`max_tokens`、`max_completion_tokens` 和 `max_output_tokens` 仍按对应接口语义限制实际输出，超过服务安全上限时自动钳制。若请求同时设置标准最大值和 `mock_output_tokens`，实际输出取两者较小值。

模型层使用内置常用词表，每个请求随机生成不同的文本。流式输出不会固定为一个 Token 一条事件：模型根据 TPS 把 Token 合并为动态长度的 chunk，TPS 越高，单事件通常包含越多 Token；每个 chunk 的计划生成时间为 `chunk Token 数 / TPS`，因此总生成时长与 Token 数和 TPS 保持一致。`MOCK_TPS=0` 表示不等待。

Chat Completions 的流式响应始终在结束前返回一个带 Usage 的空 `choices` 事件，不要求客户端传入 `stream_options.include_usage`。这是为了让压测工具能够稳定统计 Token 吞吐量。

服务聚焦文本生成、Token 统计和向量接口，不执行工具调用、图片/音频生成、文件处理、Batch 或微调任务。请求中的这些扩展字段会被忽略，不应使用本服务验证对应业务语义。

### 服务端环境变量

TTFT、TPS、延迟和输出 Token 数支持单值或 `最小值..最大值`，使用范围时会为每个请求独立随机采样。

| 变量 | 默认值 | 取值限制 | 含义 |
| --- | --- | --- | --- |
| `MOCK_ADDR` | `:8080` | 非空监听地址 | 服务监听地址 |
| `MOCK_MODEL` | `mock-gpt` | 非空字符串 | 默认模型及模型列表中的模型名 |
| `MOCK_TTFT` | `0s` | `0s` 到 `1h`，Go 时长格式 | 流式首个文本 Token 前的等待时间 |
| `MOCK_TPS` | `0` | `0` 到 `1000000` | 流式输出速率；`0` 表示不等待 |
| `MOCK_LATENCY` | `0s` | `0s` 到 `1h`，Go 时长格式 | 非流式响应延迟 |
| `MOCK_OUTPUT_TOKENS` | `16` | `1` 到 `1000000` | 默认输出 Token 数 |

## 本地开发

要求 Go 1.22 或更高版本：

```bash
go test ./...
go run .
```

默认监听 `:8080`。项目内的 `compose.yaml` 用于构建本地开发镜像 `mock-ai-api:dev`，默认仅绑定 `127.0.0.1:18080`：

```bash
docker compose up -d --build
docker compose ps
curl http://127.0.0.1:18080/healthz
```

无需环境文件即可使用默认值；需要调整镜像、端口或 Mock 参数时，可复制 `.env.example` 为 `.env`。查看日志或停止服务：

```bash
docker compose logs -f mock-ai-api
docker compose down
```
