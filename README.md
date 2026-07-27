# Mock AI API

这是一个面向 AI API 开发、集成测试和性能测试的多协议 Mock 服务。服务使用 Go 标准库实现，不依赖外部组件，支持高并发、流式和非流式响应，并返回各平台原生格式的 Usage。

服务分为两层：模型模拟层从内置词表随机生成文本，并统一负责输出 Token 数、动态流式分块、TTFT 和 TPS 时序；协议适配层只把同一份模型输出封装为各平台的响应格式。调整模型行为时不需要修改平台协议实现。

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

## 启动

要求 Go 1.22 或更高版本：

```bash
go run .
```

默认监听 `:8080`。也可以构建后运行：

```bash
go build -o mock-ai-api .
./mock-ai-api
```

## 请求示例

非流式 Chat Completions：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "mock-gpt",
    "messages": [{"role": "user", "content": "hello"}],
    "max_tokens": 16
  }'
```

流式请求可以通过 Mock 扩展字段覆盖当前请求的行为：

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
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

Anthropic Messages 示例：

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'x-api-key: any-value' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{
    "model": "mock-claude",
    "max_tokens": 16,
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

Embeddings 返回固定的 8 维浮点向量。字符串或整数 Token 数组作为单个输入，字符串数组或二维 Token 数组作为批量输入。该向量只用于接口解析和吞吐测试，不表示真实语义。

## 控制参数

| 请求字段 | 含义 | 限制 |
| --- | --- | --- |
| `mock_ttft_ms` | 流式首个文本 Token 前的等待时间 | `0` 到 1 小时 |
| `mock_tps` | 流式输出速率；`0` 表示不等待 | `0` 到 1000000 |
| `mock_latency_ms` | 非流式响应延迟 | `0` 到 1 小时 |
| `mock_input_tokens` | Usage 中的输入 Token 数 | 不小于 `0` |
| `mock_output_tokens` | 实际输出及 Usage 中的输出 Token 数 | 不超过服务端上限 |

请求中的 Mock 扩展字段都是精确值，优先于服务端配置。没有传入 `mock_input_tokens` 时，模型层按输入 JSON 的 Unicode 字符数除以 4 估算。该数值只用于稳定地产生压测指标，不等同于任何真实模型的分词结果。

`max_tokens`、`max_completion_tokens` 和 `max_output_tokens` 仍按对应接口语义限制实际输出，超过服务安全上限时自动钳制。若请求同时设置标准最大值和 `mock_output_tokens`，实际输出取两者较小值。

模型层使用内置常用词表，每个请求随机生成不同的文本。流式输出不会固定为一个 Token 一条事件：模型根据 TPS 把 Token 合并为动态长度的 chunk，TPS 越高，单事件通常包含越多 Token；每个 chunk 的计划生成时间为 `chunk Token 数 / TPS`，因此总生成时长与 Token 数和 TPS 保持一致。`MOCK_TPS=0` 表示不等待。

Chat Completions 的流式响应始终在结束前返回一个带 Usage 的空 `choices` 事件，不要求客户端传入 `stream_options.include_usage`。这是为了让压测工具能够稳定统计 Token 吞吐量。

服务聚焦文本生成、Token 统计和向量接口，不执行工具调用、图片/音频生成、文件处理、Batch 或微调任务。请求中的这些扩展字段会被忽略，不应使用本服务验证对应业务语义。

## 随机范围配置

数值和时长配置支持 `最小值..最大值` 语法，每个请求独立从该范围内采样。继续填写单值时，最小值和最大值相同，行为与旧配置完全一致。

完整的随机配置示例：

```bash
MOCK_TTFT=300ms..800ms \
MOCK_TPS=20..60 \
MOCK_LATENCY=50ms..200ms \
MOCK_OUTPUT_TOKENS=8..32 \
go run .
```

## 环境变量

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `MOCK_ADDR` | `:8080` | 监听地址 |
| `MOCK_MODEL` | `mock-gpt` | 默认模型及模型列表中的模型名 |
| `MOCK_TTFT` | `0s` | 流式 TTFT 范围，使用 Go 时长格式 |
| `MOCK_TPS` | `0` | 流式 TPS 范围，`0` 表示不等待 |
| `MOCK_LATENCY` | `0s` | 非流式响应延迟范围 |
| `MOCK_OUTPUT_TOKENS` | `16` | 输出 Token 数范围 |

例如固定模拟 800ms TTFT 和 40 TPS：

```bash
MOCK_TTFT=800ms MOCK_TPS=40 MOCK_OUTPUT_TOKENS=32 go run .
```

## 容器运行

本地构建并运行：

```bash
docker build -t mock-ai-api:dev .
docker run --rm -p 8080:8080 \
  -e MOCK_TTFT=500ms \
  -e MOCK_TPS=20 \
  mock-ai-api:dev
```

发布到 Docker Hub 时，先登录并设置自己的 Docker Hub 用户名。建议使用明确的版本标签：

```bash
export DOCKERHUB_USERNAME=your-dockerhub-username
docker login
docker build -t "${DOCKERHUB_USERNAME}/mock-ai-api:0.1.0" .
docker push "${DOCKERHUB_USERNAME}/mock-ai-api:0.1.0"
```

在 Apple Silicon 等 ARM64 环境中发布同时支持 AMD64 和 ARM64 的镜像：

```bash
export DOCKERHUB_USERNAME=your-dockerhub-username
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t "${DOCKERHUB_USERNAME}/mock-ai-api:0.1.0" \
  --push .
```

### GitHub Actions 自动发布

仓库包含 `.github/workflows/publish-docker.yml`，可通过 GitHub 托管 Runner 自动测试、构建并发布 `linux/amd64` 和 `linux/arm64` 镜像。发布前需要在 GitHub 仓库的 `Settings > Secrets and variables > Actions` 中配置：

| Secret | 含义 |
| --- | --- |
| `DOCKERHUB_USERNAME` | Docker Hub 用户名 |
| `DOCKERHUB_TOKEN` | 具有目标仓库读写权限的 Docker Hub Access Token |

建议先在 Docker Hub 创建 `mock-ai-api` 仓库，并为自动发布创建独立的 Access Token，不要使用账号密码。

推送语义化版本标签会自动发布对应的精确版本镜像：

```bash
git tag v0.1.0
git push origin v0.1.0
```

也可以在 GitHub 仓库的 `Actions > 发布 Docker 镜像 > Run workflow` 中输入 `0.1.0` 手动发布。工作流不会自动发布或覆盖 `latest` 标签。

### Docker Compose

项目默认构建本地镜像 `mock-ai-api:dev`，并仅发布到 `127.0.0.1:18080`。

构建并后台启动：

```bash
docker compose up -d --build
docker compose ps
curl http://127.0.0.1:18080/healthz
```

已有本地镜像时可跳过构建：

```bash
docker compose up -d --no-build
```

无需环境文件即可使用默认值。需要自定义时，可参考 `.env.example` 创建 `.env`；Docker Compose 会自动读取同目录的 `.env`。也可以临时覆盖单项配置：

```bash
MOCK_HOST_PORT=28080 \
MOCK_TTFT=300ms..800ms \
MOCK_TPS=20..60 \
MOCK_OUTPUT_TOKENS=8..32 \
docker compose up -d --build
```

Compose 专用变量如下，它们不属于服务端应用配置：

| 变量 | 默认值 | 含义 |
| --- | --- | --- |
| `MOCK_IMAGE_REPOSITORY` | `mock-ai-api` | 镜像仓库；使用 Docker Hub 镜像时填写 `<用户名>/mock-ai-api` |
| `MOCK_IMAGE_TAG` | `dev` | 镜像标签 |
| `MOCK_HOST_IP` | `127.0.0.1` | 发布端口绑定的宿主机地址；需要远程访问时显式改为 `0.0.0.0` |
| `MOCK_HOST_PORT` | `18080` | 发布到宿主机的 HTTP 端口 |
| `MOCK_CONTAINER_PORT` | `8080` | 容器内监听端口，同时用于端口映射和健康检查 |

查看日志、检查最终生效配置和停止服务：

```bash
docker compose logs -f mock-ai-api
docker compose config
docker compose down
```

不同工作区需要同时启动时，应为每个实例设置不同的 `MOCK_HOST_PORT`，并可通过 `docker compose -p <项目名>` 隔离容器和网络资源。服务不校验 API Key；`MOCK_HOST_IP=0.0.0.0` 会监听宿主机所有网卡，不应直接暴露到不可信网络。
