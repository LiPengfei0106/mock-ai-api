package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

type server struct {
	config         config
	logger         *slog.Logger
	model          *modelSimulator
	nextID         atomic.Uint64
	sampleSequence atomic.Uint64
	randomSeed     uint64
}

func newServer(cfg config, logger *slog.Logger) *server {
	seed := uint64(time.Now().UnixNano())
	return &server{config: cfg, logger: logger, model: newModelSimulator(seed ^ 0x517cc1b727220a95), randomSeed: seed}
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleDocsRedirect)
	mux.HandleFunc("GET /docs", handleDocs)
	mux.HandleFunc("GET /docs/", handleDocs)
	mux.HandleFunc("GET /openapi.json", handleOpenAPI)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	mux.HandleFunc("GET /v1/models/{model}", s.handleModel)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("POST /v1/completions", s.handleCompletions)
	mux.HandleFunc("POST /v1/embeddings", s.handleEmbeddings)
	mux.HandleFunc("POST /v1/responses", s.handleResponses)
	mux.HandleFunc("POST /v1/messages", s.handleAnthropicMessages)
	mux.HandleFunc("POST /v1/messages/count_tokens", s.handleAnthropicCountTokens)
	mux.HandleFunc("POST /openai/deployments/{deployment}/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("POST /openai/deployments/{deployment}/completions", s.handleCompletions)
	mux.HandleFunc("POST /openai/deployments/{deployment}/embeddings", s.handleEmbeddings)
	registerGeminiRoutes(mux, s)
	registerOllamaRoutes(mux, s)
	return recoverMiddleware(s.logger, mux)
}

func (s *server) handleHealth(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleModels(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("anthropic-version") != "" {
		writeJSON(response, http.StatusOK, map[string]any{
			"data": []map[string]string{{
				"id": s.config.Model, "type": "model", "display_name": s.config.Model,
				"created_at": "1970-01-01T00:00:00Z",
			}},
			"has_more": false, "first_id": s.config.Model, "last_id": s.config.Model,
		})
		return
	}
	if request.Header.Get("x-goog-api-key") != "" || request.URL.Query().Get("key") != "" {
		s.handleGeminiModels(response, request)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"object": "list",
		"data": []map[string]any{{
			"id":       s.config.Model,
			"object":   "model",
			"created":  0,
			"owned_by": "mock-ai-api",
		}},
	})
}

func (s *server) handleModel(response http.ResponseWriter, request *http.Request) {
	model := request.PathValue("model")
	if model == "" {
		model = s.config.Model
	}
	if request.Header.Get("x-goog-api-key") != "" || request.URL.Query().Get("key") != "" {
		writeJSON(response, http.StatusOK, geminiModel(model))
		return
	}
	if request.Header.Get("anthropic-version") != "" {
		writeJSON(response, http.StatusOK, map[string]string{
			"id": model, "type": "model", "display_name": model,
			"created_at": "1970-01-01T00:00:00Z",
		})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"id": model, "object": "model", "created": 0, "owned_by": "mock-ai-api",
	})
}

func (s *server) handleChatCompletions(response http.ResponseWriter, request *http.Request) {
	payload, options, ok := s.parseRequest(response, request, false)
	if !ok {
		return
	}
	if deployment := request.PathValue("deployment"); deployment != "" && payload.Model == "" {
		options.Model = deployment
	}
	id := "chatcmpl-mock-" + strconv.FormatUint(s.nextID.Add(1), 10)
	if payload.Stream {
		s.streamChat(response, request, id, options)
		return
	}
	if !waitContext(request.Context(), options.Latency) {
		return
	}
	content := options.Generation.Text
	writeJSON(response, http.StatusOK, map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   options.Model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]string{
				"role":    "assistant",
				"content": content,
			},
			"finish_reason": "stop",
		}},
		"usage": chatUsage(options),
	})
}

func (s *server) handleResponses(response http.ResponseWriter, request *http.Request) {
	payload, options, ok := s.parseRequest(response, request, true)
	if !ok {
		return
	}
	id := "resp_mock_" + strconv.FormatUint(s.nextID.Add(1), 10)
	if payload.Stream {
		s.streamResponse(response, request, id, options)
		return
	}
	if !waitContext(request.Context(), options.Latency) {
		return
	}
	content := options.Generation.Text
	writeJSON(response, http.StatusOK, map[string]any{
		"id":         id,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     "completed",
		"model":      options.Model,
		"output": []map[string]any{{
			"id":     "msg_" + id,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]string{{
				"type": "output_text",
				"text": content,
			}},
		}},
		"output_text": content,
		"usage":       responseUsage(options),
	})
}

func (s *server) streamChat(response http.ResponseWriter, request *http.Request, id string, options requestOptions) {
	stream, ok := newEventStream(response)
	if !ok {
		writeAPIError(response, http.StatusInternalServerError, "server_error", "当前 HTTP 响应不支持流式传输")
		return
	}
	created := time.Now().Unix()
	if !stream.send(map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": created, "model": options.Model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]string{"role": "assistant"}, "finish_reason": nil}},
	}) {
		return
	}
	if !waitContext(request.Context(), options.TTFT) {
		return
	}
	chunks := s.model.chunks(options.Generation, options.TPS)
	for index, chunk := range chunks {
		if index > 0 && !waitContext(request.Context(), chunks[index-1].Delay) {
			return
		}
		if !stream.send(map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": options.Model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]string{"content": chunk.Text}, "finish_reason": nil}},
		}) {
			return
		}
	}
	if len(chunks) > 0 && !waitContext(request.Context(), chunks[len(chunks)-1].Delay) {
		return
	}
	if !stream.send(map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": created, "model": options.Model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	}) {
		return
	}
	// 为便于压测统计，流式响应始终返回最终 Usage，不依赖 stream_options。
	if !stream.send(map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": created, "model": options.Model,
		"choices": []any{}, "usage": chatUsage(options),
	}) {
		return
	}
	stream.done()
}

func (s *server) streamResponse(response http.ResponseWriter, request *http.Request, id string, options requestOptions) {
	stream, ok := newEventStream(response)
	if !ok {
		writeAPIError(response, http.StatusInternalServerError, "server_error", "当前 HTTP 响应不支持流式传输")
		return
	}
	created := time.Now().Unix()
	base := map[string]any{
		"id": id, "object": "response", "created_at": created, "status": "in_progress", "model": options.Model,
	}
	if !stream.send(map[string]any{"type": "response.created", "response": base}) {
		return
	}
	if !waitContext(request.Context(), options.TTFT) {
		return
	}
	var text bytes.Buffer
	chunks := s.model.chunks(options.Generation, options.TPS)
	for index, chunk := range chunks {
		if index > 0 && !waitContext(request.Context(), chunks[index-1].Delay) {
			return
		}
		delta := chunk.Text
		text.WriteString(delta)
		if !stream.send(map[string]any{
			"type": "response.output_text.delta", "item_id": "msg_" + id,
			"output_index": 0, "content_index": 0, "delta": delta,
		}) {
			return
		}
	}
	if len(chunks) > 0 && !waitContext(request.Context(), chunks[len(chunks)-1].Delay) {
		return
	}
	if !stream.send(map[string]any{
		"type": "response.output_text.done", "item_id": "msg_" + id,
		"output_index": 0, "content_index": 0, "text": text.String(),
	}) {
		return
	}
	base["status"] = "completed"
	base["output_text"] = text.String()
	base["output"] = []map[string]any{{
		"id":     "msg_" + id,
		"type":   "message",
		"status": "completed",
		"role":   "assistant",
		"content": []map[string]string{{
			"type": "output_text",
			"text": text.String(),
		}},
	}}
	base["usage"] = responseUsage(options)
	stream.send(map[string]any{"type": "response.completed", "response": base})
}
