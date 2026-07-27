package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (s *server) handleAnthropicMessages(response http.ResponseWriter, request *http.Request) {
	payload, options, ok := s.parseAnthropicRequest(response, request)
	if !ok {
		return
	}
	id := "msg_mock_" + strconv.FormatUint(s.nextID.Add(1), 10)
	if payload.Stream {
		s.streamAnthropicMessage(response, request, id, options)
		return
	}
	if !waitContext(request.Context(), options.Latency) {
		return
	}
	writeJSON(response, http.StatusOK, anthropicMessage(id, options, options.Generation.Text))
}

func anthropicMessage(id string, options requestOptions, text string) map[string]any {
	return map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": options.Model,
		"content":     []map[string]string{{"type": "text", "text": text}},
		"stop_reason": "end_turn", "stop_sequence": nil,
		"usage": anthropicUsage{InputTokens: options.InputTokens, OutputTokens: options.OutputTokens},
	}
}

func (s *server) streamAnthropicMessage(response http.ResponseWriter, request *http.Request, id string, options requestOptions) {
	stream, ok := newEventStream(response)
	if !ok {
		writeAnthropicError(response, http.StatusInternalServerError, "api_error", "当前 HTTP 响应不支持流式传输")
		return
	}
	if !stream.sendEvent("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": options.Model,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": anthropicUsage{InputTokens: options.InputTokens},
		},
	}) || !stream.sendEvent("content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]string{"type": "text", "text": ""},
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
		if !stream.sendEvent("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]string{"type": "text_delta", "text": chunk.Text},
		}) {
			return
		}
	}
	if len(chunks) > 0 && !waitContext(request.Context(), chunks[len(chunks)-1].Delay) {
		return
	}
	if !stream.sendEvent("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}) ||
		!stream.sendEvent("message_delta", map[string]any{
			"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
			"usage": anthropicUsage{OutputTokens: options.OutputTokens},
		}) {
		return
	}
	stream.sendEvent("message_stop", map[string]string{"type": "message_stop"})
}

func (s *server) handleAnthropicCountTokens(response http.ResponseWriter, request *http.Request) {
	_, options, ok := s.parseAnthropicRequest(response, request)
	if !ok {
		return
	}
	writeJSON(response, http.StatusOK, map[string]int{"input_tokens": options.InputTokens})
}

func (s *server) parseAnthropicRequest(response http.ResponseWriter, request *http.Request) (completionRequest, requestOptions, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodySize)
	decoder := json.NewDecoder(request.Body)
	var payload completionRequest
	if err := decoder.Decode(&payload); err != nil {
		writeAnthropicError(response, http.StatusBadRequest, "invalid_request_error", "请求体不是有效 JSON："+err.Error())
		return completionRequest{}, requestOptions{}, false
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeAnthropicError(response, http.StatusBadRequest, "invalid_request_error", err.Error())
		return completionRequest{}, requestOptions{}, false
	}
	options, err := s.resolveOptions(payload, false)
	if err != nil {
		writeAnthropicError(response, http.StatusBadRequest, "invalid_request_error", err.Error())
		return completionRequest{}, requestOptions{}, false
	}
	return payload, options, true
}

func writeAnthropicError(response http.ResponseWriter, status int, errorType string, message string) {
	writeJSON(response, status, map[string]any{
		"type": "error", "error": map[string]string{"type": errorType, "message": message},
	})
}
