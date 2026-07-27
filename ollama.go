package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type ollamaRequest struct {
	completionRequest
	StreamValue *bool `json:"stream"`
	Options     struct {
		NumPredict *int `json:"num_predict"`
	} `json:"options"`
}

func registerOllamaRoutes(mux *http.ServeMux, s *server) {
	mux.HandleFunc("GET /api/tags", s.handleOllamaTags)
	mux.HandleFunc("POST /api/show", s.handleOllamaShow)
	mux.HandleFunc("POST /api/chat", s.handleOllamaChat)
	mux.HandleFunc("POST /api/generate", s.handleOllamaGenerate)
	mux.HandleFunc("POST /api/embed", s.handleOllamaEmbed)
	mux.HandleFunc("POST /api/embeddings", s.handleOllamaLegacyEmbedding)
}

func (s *server) handleOllamaTags(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"models": []map[string]any{ollamaModel(s.config.Model)},
	})
}

func (s *server) handleOllamaShow(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		Model string `json:"model"`
		Name  string `json:"name"`
	}
	if !decodeJSON(response, request, &payload, writeOllamaError) {
		return
	}
	model := payload.Model
	if model == "" {
		model = payload.Name
	}
	if model == "" {
		model = s.config.Model
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"license": "mock", "modelfile": "# Mock AI API", "parameters": "", "template": "{{ .Prompt }}",
		"details":      ollamaModel(model)["details"],
		"model_info":   map[string]any{"general.architecture": "mock", "general.parameter_count": 0},
		"capabilities": []string{"completion", "embedding"},
	})
}

func ollamaModel(model string) map[string]any {
	return map[string]any{
		"name": model, "model": model, "modified_at": time.Unix(0, 0).UTC().Format(time.RFC3339Nano),
		"size": 0, "digest": "sha256:mock",
		"details": map[string]any{"format": "mock", "family": "mock", "families": []string{"mock"}, "parameter_size": "0", "quantization_level": "none"},
	}
}

func (s *server) handleOllamaChat(response http.ResponseWriter, request *http.Request) {
	payload, options, stream, ok := s.parseOllamaRequest(response, request, false)
	if !ok {
		return
	}
	if stream {
		s.streamOllama(response, request, options, true)
		return
	}
	if !waitContext(request.Context(), options.Latency) {
		return
	}
	writeJSON(response, http.StatusOK, ollamaChatResponse(options, options.Generation.Text, true))
	_ = payload
}

func (s *server) handleOllamaGenerate(response http.ResponseWriter, request *http.Request) {
	_, options, stream, ok := s.parseOllamaRequest(response, request, false)
	if !ok {
		return
	}
	if stream {
		s.streamOllama(response, request, options, false)
		return
	}
	if !waitContext(request.Context(), options.Latency) {
		return
	}
	writeJSON(response, http.StatusOK, ollamaGenerateResponse(options, options.Generation.Text, true))
}

func (s *server) streamOllama(response http.ResponseWriter, request *http.Request, options requestOptions, chat bool) {
	stream, ok := newJSONLineStream(response)
	if !ok {
		writeOllamaError(response, http.StatusInternalServerError, "当前 HTTP 响应不支持流式传输")
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
		var value any
		if chat {
			value = ollamaChatResponse(options, chunk.Text, false)
		} else {
			value = ollamaGenerateResponse(options, chunk.Text, false)
		}
		if !stream.send(value) {
			return
		}
	}
	if len(chunks) > 0 && !waitContext(request.Context(), chunks[len(chunks)-1].Delay) {
		return
	}
	if chat {
		stream.send(ollamaChatResponse(options, "", true))
	} else {
		stream.send(ollamaGenerateResponse(options, "", true))
	}
}

func ollamaChatResponse(options requestOptions, content string, done bool) map[string]any {
	value := ollamaBaseResponse(options, done)
	value["message"] = map[string]string{"role": "assistant", "content": content}
	return value
}

func ollamaGenerateResponse(options requestOptions, content string, done bool) map[string]any {
	value := ollamaBaseResponse(options, done)
	value["response"] = content
	return value
}

func ollamaBaseResponse(options requestOptions, done bool) map[string]any {
	value := map[string]any{
		"model": options.Model, "created_at": time.Now().UTC().Format(time.RFC3339Nano), "done": done,
	}
	if done {
		value["done_reason"] = "stop"
		value["total_duration"] = int64(options.Latency)
		value["load_duration"] = 0
		value["prompt_eval_count"] = options.InputTokens
		value["prompt_eval_duration"] = 0
		value["eval_count"] = options.OutputTokens
		if options.TPS > 0 {
			value["eval_duration"] = int64(float64(time.Second) * float64(options.OutputTokens) / options.TPS)
		} else {
			value["eval_duration"] = 0
		}
	}
	return value
}

func (s *server) handleOllamaEmbed(response http.ResponseWriter, request *http.Request) {
	payload, options, _, ok := s.parseOllamaRequest(response, request, true)
	if !ok || !waitContext(request.Context(), options.Latency) {
		return
	}
	count := embeddingInputCount(payload.Input)
	embeddings := make([][]float64, count)
	for index := range embeddings {
		embeddings[index] = mockEmbedding()
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"model": options.Model, "embeddings": embeddings, "total_duration": int64(options.Latency),
		"load_duration": 0, "prompt_eval_count": options.InputTokens,
	})
}

func (s *server) handleOllamaLegacyEmbedding(response http.ResponseWriter, request *http.Request) {
	_, options, _, ok := s.parseOllamaRequest(response, request, true)
	if !ok || !waitContext(request.Context(), options.Latency) {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"embedding": mockEmbedding()})
}

func (s *server) parseOllamaRequest(response http.ResponseWriter, request *http.Request, embedding bool) (ollamaRequest, requestOptions, bool, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodySize)
	decoder := json.NewDecoder(request.Body)
	var payload ollamaRequest
	if err := decoder.Decode(&payload); err != nil {
		writeOllamaError(response, http.StatusBadRequest, "请求体不是有效 JSON："+err.Error())
		return ollamaRequest{}, requestOptions{}, false, false
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeOllamaError(response, http.StatusBadRequest, err.Error())
		return ollamaRequest{}, requestOptions{}, false, false
	}
	if payload.Options.NumPredict != nil {
		payload.MaxTokens = payload.Options.NumPredict
	}
	options, err := s.resolveOptions(payload.completionRequest, embedding)
	if err != nil {
		writeOllamaError(response, http.StatusBadRequest, err.Error())
		return ollamaRequest{}, requestOptions{}, false, false
	}
	stream := true
	if payload.StreamValue != nil {
		stream = *payload.StreamValue
	}
	return payload, options, stream, true
}

type jsonLineStream struct {
	response http.ResponseWriter
	flusher  http.Flusher
}

func newJSONLineStream(response http.ResponseWriter) (*jsonLineStream, bool) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		return nil, false
	}
	response.Header().Set("Content-Type", "application/x-ndjson")
	response.WriteHeader(http.StatusOK)
	return &jsonLineStream{response: response, flusher: flusher}, true
}

func (stream *jsonLineStream) send(value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	data = append(data, '\n')
	if _, err = stream.response.Write(data); err != nil {
		return false
	}
	stream.flusher.Flush()
	return true
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any, writeError func(http.ResponseWriter, int, string)) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodySize)
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(target); err != nil {
		writeError(response, http.StatusBadRequest, "请求体不是有效 JSON："+err.Error())
		return false
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func writeOllamaError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message, "request_id": "ollama-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)})
}
