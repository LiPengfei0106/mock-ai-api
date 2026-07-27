package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type geminiRequest struct {
	completionRequest
	Contents         json.RawMessage   `json:"contents"`
	Requests         []json.RawMessage `json:"requests"`
	GenerationConfig struct {
		MaxOutputTokens *int `json:"maxOutputTokens"`
	} `json:"generationConfig"`
}

func registerGeminiRoutes(mux *http.ServeMux, s *server) {
	for _, version := range []string{"v1", "v1beta"} {
		mux.HandleFunc("POST /"+version+"/models/{action}", s.handleGeminiAction)
	}
	mux.HandleFunc("GET /v1beta/models", s.handleGeminiModels)
	mux.HandleFunc("GET /v1beta/models/{model}", s.handleGeminiModel)
}

func (s *server) handleGeminiAction(response http.ResponseWriter, request *http.Request) {
	action := request.PathValue("action")
	separator := strings.LastIndexByte(action, ':')
	if separator <= 0 {
		writeGeminiError(response, http.StatusNotFound, "NOT_FOUND", "未知的 Gemini 接口")
		return
	}
	request.SetPathValue("model", action[:separator])
	switch action[separator+1:] {
	case "generateContent":
		s.handleGeminiGenerate(response, request)
	case "streamGenerateContent":
		s.handleGeminiStreamGenerate(response, request)
	case "countTokens":
		s.handleGeminiCountTokens(response, request)
	case "embedContent":
		s.handleGeminiEmbed(response, request)
	case "batchEmbedContents":
		s.handleGeminiBatchEmbed(response, request)
	default:
		writeGeminiError(response, http.StatusNotFound, "NOT_FOUND", "未知的 Gemini 接口")
	}
}

func (s *server) handleGeminiModels(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"models": []map[string]any{geminiModel(s.config.Model)},
	})
}

func (s *server) handleGeminiModel(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, geminiModel(request.PathValue("model")))
}

func geminiModel(model string) map[string]any {
	return map[string]any{
		"name": "models/" + model, "version": "mock", "displayName": model,
		"description": "Mock AI API model", "inputTokenLimit": maxInputTokens,
		"outputTokenLimit":           hardMaxOutputTokens,
		"supportedGenerationMethods": []string{"generateContent", "countTokens", "embedContent", "batchEmbedContents"},
	}
}

func (s *server) handleGeminiGenerate(response http.ResponseWriter, request *http.Request) {
	_, options, ok := s.parseGeminiRequest(response, request)
	if !ok || !waitContext(request.Context(), options.Latency) {
		return
	}
	writeJSON(response, http.StatusOK, geminiResponse(s.nextID.Add(1), options, options.Generation.Text))
}

func (s *server) handleGeminiStreamGenerate(response http.ResponseWriter, request *http.Request) {
	_, options, ok := s.parseGeminiRequest(response, request)
	if !ok {
		return
	}
	stream, ok := newEventStream(response)
	if !ok {
		writeGeminiError(response, http.StatusInternalServerError, "INTERNAL", "当前 HTTP 响应不支持流式传输")
		return
	}
	if !waitContext(request.Context(), options.TTFT) {
		return
	}
	id := s.nextID.Add(1)
	chunks := s.model.chunks(options.Generation, options.TPS)
	for index, chunk := range chunks {
		if index > 0 && !waitContext(request.Context(), chunks[index-1].Delay) {
			return
		}
		event := geminiResponse(id, options, chunk.Text)
		if index < len(chunks)-1 {
			candidate := event["candidates"].([]map[string]any)[0]
			delete(candidate, "finishReason")
			delete(event, "usageMetadata")
		}
		if !stream.send(event) {
			return
		}
	}
	if len(chunks) > 0 {
		waitContext(request.Context(), chunks[len(chunks)-1].Delay)
	}
}

func geminiResponse(id uint64, options requestOptions, text string) map[string]any {
	return map[string]any{
		"candidates": []map[string]any{{
			"content":      map[string]any{"parts": []map[string]string{{"text": text}}, "role": "model"},
			"finishReason": "STOP", "index": 0,
		}},
		"usageMetadata": map[string]int{
			"promptTokenCount": options.InputTokens, "candidatesTokenCount": options.OutputTokens,
			"totalTokenCount": options.InputTokens + options.OutputTokens,
		},
		"modelVersion": options.Model, "responseId": "gemini-mock-" + strconv.FormatUint(id, 10),
	}
}

func (s *server) handleGeminiCountTokens(response http.ResponseWriter, request *http.Request) {
	_, options, ok := s.parseGeminiRequest(response, request)
	if !ok {
		return
	}
	writeJSON(response, http.StatusOK, map[string]int{"totalTokens": options.InputTokens})
}

func (s *server) handleGeminiEmbed(response http.ResponseWriter, request *http.Request) {
	_, options, ok := s.parseGeminiRequest(response, request)
	if !ok || !waitContext(request.Context(), options.Latency) {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"embedding": map[string]any{"values": mockEmbedding()}})
}

func (s *server) handleGeminiBatchEmbed(response http.ResponseWriter, request *http.Request) {
	payload, options, ok := s.parseGeminiRequest(response, request)
	if !ok || !waitContext(request.Context(), options.Latency) {
		return
	}
	count := 1
	if len(payload.Requests) > 0 {
		count = len(payload.Requests)
	}
	embeddings := make([]map[string]any, count)
	for index := range embeddings {
		embeddings[index] = map[string]any{"values": mockEmbedding()}
	}
	writeJSON(response, http.StatusOK, map[string]any{"embeddings": embeddings})
}

func (s *server) parseGeminiRequest(response http.ResponseWriter, request *http.Request) (geminiRequest, requestOptions, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodySize)
	decoder := json.NewDecoder(request.Body)
	var payload geminiRequest
	if err := decoder.Decode(&payload); err != nil {
		writeGeminiError(response, http.StatusBadRequest, "INVALID_ARGUMENT", "请求体不是有效 JSON："+err.Error())
		return geminiRequest{}, requestOptions{}, false
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeGeminiError(response, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return geminiRequest{}, requestOptions{}, false
	}
	payload.Model = request.PathValue("model")
	payload.Input = payload.Contents
	if len(payload.Input) == 0 {
		// Batch Embed 的 requests 也参与输入 Token 估算。
		encoded, _ := json.Marshal(payload)
		payload.Input = encoded
	}
	payload.MaxOutputTokens = payload.GenerationConfig.MaxOutputTokens
	options, err := s.resolveOptions(payload.completionRequest, true)
	if err != nil {
		writeGeminiError(response, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return geminiRequest{}, requestOptions{}, false
	}
	return payload, options, true
}

func writeGeminiError(response http.ResponseWriter, status int, errorStatus string, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]any{"code": status, "message": message, "status": errorStatus},
	})
}

func mockEmbedding() []float64 {
	return []float64{0.0125, -0.025, 0.05, -0.1, 0.2, -0.4, 0.8, -0.16}
}
