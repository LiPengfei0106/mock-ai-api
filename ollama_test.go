package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaChatAndStream(t *testing.T) {
	body := `{"model":"llama-mock","messages":[{"role":"user","content":"hello"}],"stream":false,"mock_input_tokens":3,"mock_output_tokens":2}`
	request := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"content":"`) || !strings.Contains(response.Body.String(), `"eval_count":2`) {
		t.Fatalf("Ollama Chat 响应不符合预期：%d %s", response.Code, response.Body.String())
	}

	body = `{"model":"llama-mock","prompt":"hello","stream":true,"mock_output_tokens":2}`
	request = httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body))
	streamResponse := newStreamRecorder(time.Now())
	testServer().handler().ServeHTTP(streamResponse, request)
	if count := strings.Count(streamResponse.body.String(), `"response":"`); count < 2 {
		t.Fatalf("Ollama 流事件不完整：%s", streamResponse.body.String())
	}
	if !strings.Contains(streamResponse.body.String(), `"done_reason":"stop"`) {
		t.Fatalf("Ollama 流缺少结束事件：%s", streamResponse.body.String())
	}
}

func TestOllamaModelsAndEmbedding(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"model":"mock-test"`) {
		t.Fatalf("Ollama Tags 响应不符合预期：%d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/embed", strings.NewReader(`{"model":"embed-mock","input":["a","b"],"mock_input_tokens":2}`))
	response = httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Count(response.Body.String(), "0.0125") != 2 {
		t.Fatalf("Ollama Embed 响应不符合预期：%d %s", response.Code, response.Body.String())
	}
}
