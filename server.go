package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const maxRequestBodySize = 1 << 20

type server struct {
	config         config
	logger         *slog.Logger
	model          *modelSimulator
	nextID         atomic.Uint64
	sampleSequence atomic.Uint64
	randomSeed     uint64
}

type completionRequest struct {
	Model               string          `json:"model"`
	Messages            []message       `json:"messages"`
	Input               json.RawMessage `json:"input"`
	Prompt              json.RawMessage `json:"prompt"`
	System              json.RawMessage `json:"system"`
	Stream              bool            `json:"stream"`
	MaxTokens           *int            `json:"max_tokens"`
	MaxCompletionTokens *int            `json:"max_completion_tokens"`
	MaxOutputTokens     *int            `json:"max_output_tokens"`
	MockTTFTMS          *int64          `json:"mock_ttft_ms"`
	MockTPS             *float64        `json:"mock_tps"`
	MockLatencyMS       *int64          `json:"mock_latency_ms"`
	MockInputTokens     *int            `json:"mock_input_tokens"`
	MockOutputTokens    *int            `json:"mock_output_tokens"`
	StreamOptions       map[string]any  `json:"stream_options"`
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type requestOptions struct {
	Model        string
	TTFT         time.Duration
	TPS          float64
	Latency      time.Duration
	InputTokens  int
	OutputTokens int
	MaxTokens    int
	Generation   modelGeneration
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens"`
}

func newServer(cfg config, logger *slog.Logger) *server {
	seed := uint64(time.Now().UnixNano())
	return &server{config: cfg, logger: logger, model: newModelSimulator(seed ^ 0x517cc1b727220a95), randomSeed: seed}
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
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

func (s *server) parseRequest(response http.ResponseWriter, request *http.Request, responsesAPI bool) (completionRequest, requestOptions, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodySize)
	decoder := json.NewDecoder(request.Body)
	var payload completionRequest
	if err := decoder.Decode(&payload); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid_request_error", "请求体不是有效 JSON："+err.Error())
		return completionRequest{}, requestOptions{}, false
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid_request_error", err.Error())
		return completionRequest{}, requestOptions{}, false
	}

	options, err := s.resolveOptions(payload, responsesAPI)
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid_request_error", err.Error())
		return completionRequest{}, requestOptions{}, false
	}
	return payload, options, true
}

func (s *server) resolveOptions(payload completionRequest, responsesAPI bool) (requestOptions, error) {
	random := newSampler(s.randomSeed + s.sampleSequence.Add(1))
	options := requestOptions{
		Model:        payload.Model,
		TTFT:         random.duration(s.config.TTFT),
		TPS:          random.float64(s.config.TPS),
		Latency:      random.duration(s.config.Latency),
		OutputTokens: random.int(s.config.OutputTokens),
		MaxTokens:    hardMaxOutputTokens,
	}
	if options.Model == "" {
		options.Model = s.config.Model
	}
	maxTokens := payload.MaxTokens
	if payload.MaxCompletionTokens != nil {
		maxTokens = payload.MaxCompletionTokens
	}
	if responsesAPI && payload.MaxOutputTokens != nil {
		maxTokens = payload.MaxOutputTokens
	}
	if maxTokens != nil {
		if *maxTokens < 1 {
			return requestOptions{}, errors.New("请求最大输出 Token 数必须大于 0")
		}
		options.MaxTokens = min(*maxTokens, hardMaxOutputTokens)
	}
	if payload.MockOutputTokens != nil {
		if *payload.MockOutputTokens < 1 || *payload.MockOutputTokens > hardMaxOutputTokens {
			return requestOptions{}, fmt.Errorf("mock_output_tokens 必须在 1 到 %d 之间", hardMaxOutputTokens)
		}
		options.OutputTokens = *payload.MockOutputTokens
	}
	if options.OutputTokens > options.MaxTokens {
		options.OutputTokens = options.MaxTokens
	}
	options.Generation = s.model.generate(options.OutputTokens)

	if payload.MockTTFTMS != nil {
		if *payload.MockTTFTMS < 0 || *payload.MockTTFTMS > maxDelay.Milliseconds() {
			return requestOptions{}, fmt.Errorf("mock_ttft_ms 必须在 0 到 %d 之间", maxDelay.Milliseconds())
		}
		options.TTFT = time.Duration(*payload.MockTTFTMS) * time.Millisecond
	}
	if payload.MockLatencyMS != nil {
		if *payload.MockLatencyMS < 0 || *payload.MockLatencyMS > maxDelay.Milliseconds() {
			return requestOptions{}, fmt.Errorf("mock_latency_ms 必须在 0 到 %d 之间", maxDelay.Milliseconds())
		}
		options.Latency = time.Duration(*payload.MockLatencyMS) * time.Millisecond
	}
	if payload.MockTPS != nil {
		if math.IsNaN(*payload.MockTPS) || math.IsInf(*payload.MockTPS, 0) || *payload.MockTPS < 0 || *payload.MockTPS > maxTPS {
			return requestOptions{}, fmt.Errorf("mock_tps 必须在 0 到 %d 之间", maxTPS)
		}
		options.TPS = *payload.MockTPS
	}

	if payload.MockInputTokens != nil {
		if *payload.MockInputTokens < 0 || *payload.MockInputTokens > maxInputTokens {
			return requestOptions{}, fmt.Errorf("mock_input_tokens 必须在 0 到 %d 之间", maxInputTokens)
		}
		options.InputTokens = *payload.MockInputTokens
	} else if responsesAPI {
		options.InputTokens = estimateInputTokens(payload.Input)
	} else {
		size := utf8.RuneCount(payload.Prompt) + utf8.RuneCount(payload.System)
		for _, item := range payload.Messages {
			size += utf8.RuneCount(item.Content)
		}
		options.InputTokens = estimatedTokens(size)
	}
	return options, nil
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

type eventStream struct {
	response http.ResponseWriter
	flusher  http.Flusher
}

func newEventStream(response http.ResponseWriter) (*eventStream, bool) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		return nil, false
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	return &eventStream{response: response, flusher: flusher}, true
}

func (stream *eventStream) send(value any) bool {
	return stream.sendEvent("", value)
}

func (stream *eventStream) sendEvent(event string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if event != "" {
		if _, err = stream.response.Write([]byte("event: " + event + "\n")); err != nil {
			return false
		}
	}
	if _, err = stream.response.Write([]byte("data: ")); err != nil {
		return false
	}
	if _, err = stream.response.Write(data); err != nil {
		return false
	}
	if _, err = stream.response.Write([]byte("\n\n")); err != nil {
		return false
	}
	stream.flusher.Flush()
	return true
}

func (stream *eventStream) done() {
	_, _ = stream.response.Write([]byte("data: [DONE]\n\n"))
	stream.flusher.Flush()
}

func chatUsage(options requestOptions) usage {
	return usage{
		PromptTokens:     options.InputTokens,
		CompletionTokens: options.OutputTokens,
		TotalTokens:      options.InputTokens + options.OutputTokens,
	}
}

func responseUsage(options requestOptions) usage {
	return usage{
		InputTokens:  options.InputTokens,
		OutputTokens: options.OutputTokens,
		TotalTokens:  options.InputTokens + options.OutputTokens,
	}
}

func estimateInputTokens(raw json.RawMessage) int {
	return estimatedTokens(utf8.RuneCount(raw))
}

func estimatedTokens(runes int) int {
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}

// sampler 是请求内独占的伪随机生成器，避免万级并发下争用全局随机数锁。
type sampler struct {
	state uint64
}

func newSampler(seed uint64) *sampler {
	return &sampler{state: seed}
}

func (random *sampler) next() uint64 {
	random.state += 0x9e3779b97f4a7c15
	value := random.state
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func (random *sampler) int(value intRange) int {
	if value.Min == value.Max {
		return value.Min
	}
	span := uint64(value.Max-value.Min) + 1
	return value.Min + int(random.next()%span)
}

func (random *sampler) duration(value durationRange) time.Duration {
	if value.Min == value.Max {
		return value.Min
	}
	span := uint64(value.Max-value.Min) + 1
	return value.Min + time.Duration(random.next()%span)
}

func (random *sampler) float64(value floatRange) float64 {
	if value.Min == value.Max {
		return value.Min
	}
	// 取高 53 位构造 [0, 1) 均匀分布浮点数。
	fraction := float64(random.next()>>11) * (1.0 / (1 << 53))
	return value.Min + fraction*(value.Max-value.Min)
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("请求体包含无效的尾随内容：%w", err)
	}
	return errors.New("请求体只能包含一个 JSON 值")
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeAPIError(response http.ResponseWriter, status int, errorType, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errorType,
			"param":   nil,
			"code":    nil,
		},
	})
}

func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("request panic", "error", recovered, "method", request.Method, "path", request.URL.Path)
				writeAPIError(response, http.StatusInternalServerError, "server_error", "服务内部错误")
			}
		}()
		next.ServeHTTP(response, request)
	})
}
