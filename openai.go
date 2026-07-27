package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

func (s *server) handleCompletions(response http.ResponseWriter, request *http.Request) {
	payload, options, ok := s.parseRequest(response, request, false)
	if !ok {
		return
	}
	if deployment := request.PathValue("deployment"); deployment != "" && payload.Model == "" {
		options.Model = deployment
	}
	id := "cmpl-mock-" + strconv.FormatUint(s.nextID.Add(1), 10)
	if payload.Stream {
		s.streamCompletion(response, request, id, options)
		return
	}
	if !waitContext(request.Context(), options.Latency) {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"id": id, "object": "text_completion", "created": time.Now().Unix(), "model": options.Model,
		"choices": []map[string]any{{
			"index": 0, "text": options.Generation.Text,
			"logprobs": nil, "finish_reason": "stop",
		}},
		"usage": chatUsage(options),
	})
}

func (s *server) streamCompletion(response http.ResponseWriter, request *http.Request, id string, options requestOptions) {
	stream, ok := newEventStream(response)
	if !ok {
		writeAPIError(response, http.StatusInternalServerError, "server_error", "当前 HTTP 响应不支持流式传输")
		return
	}
	created := time.Now().Unix()
	if !waitContext(request.Context(), options.TTFT) {
		return
	}
	chunks := s.model.chunks(options.Generation, options.TPS)
	for index, chunk := range chunks {
		if index > 0 && !waitContext(request.Context(), chunks[index-1].Delay) {
			return
		}
		if !stream.send(map[string]any{
			"id": id, "object": "text_completion", "created": created, "model": options.Model,
			"choices": []map[string]any{{"index": 0, "text": chunk.Text, "logprobs": nil, "finish_reason": nil}},
		}) {
			return
		}
	}
	if len(chunks) > 0 && !waitContext(request.Context(), chunks[len(chunks)-1].Delay) {
		return
	}
	if !stream.send(map[string]any{
		"id": id, "object": "text_completion", "created": created, "model": options.Model,
		"choices": []map[string]any{{"index": 0, "text": "", "logprobs": nil, "finish_reason": "stop"}},
		"usage":   chatUsage(options),
	}) {
		return
	}
	stream.done()
}

func (s *server) handleEmbeddings(response http.ResponseWriter, request *http.Request) {
	payload, options, ok := s.parseRequest(response, request, true)
	if !ok {
		return
	}
	if deployment := request.PathValue("deployment"); deployment != "" && payload.Model == "" {
		options.Model = deployment
	}
	if payload.Stream {
		writeAPIError(response, http.StatusBadRequest, "invalid_request_error", "Embeddings 接口不支持 stream")
		return
	}
	if !waitContext(request.Context(), options.Latency) {
		return
	}
	inputCount := embeddingInputCount(payload.Input)
	data := make([]map[string]any, inputCount)
	for index := range data {
		// 固定短向量可用于验证解析和吞吐，不模拟真实语义。
		data[index] = map[string]any{
			"object": "embedding", "index": index,
			"embedding": mockEmbedding(),
		}
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"object": "list", "data": data, "model": options.Model,
		"usage": map[string]int{"prompt_tokens": options.InputTokens, "total_tokens": options.InputTokens},
	})
}

func embeddingInputCount(input json.RawMessage) int {
	var values []json.RawMessage
	if json.Unmarshal(input, &values) == nil && len(values) > 0 && len(values[0]) > 0 && (values[0][0] == '"' || values[0][0] == '[') {
		return len(values)
	}
	return 1
}
