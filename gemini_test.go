package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGeminiGenerateAndStream(t *testing.T) {
	body := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":2},"mock_input_tokens":5,"mock_output_tokens":2}`
	request := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-mock:generateContent?key=test", strings.NewReader(body))
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"text":"`) || !strings.Contains(response.Body.String(), `"totalTokenCount":7`) {
		t.Fatalf("Gemini 响应不符合预期：%d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-mock:streamGenerateContent?alt=sse&key=test", strings.NewReader(body))
	streamResponse := newStreamRecorder(time.Now())
	testServer().handler().ServeHTTP(streamResponse, request)
	if count := strings.Count(streamResponse.body.String(), `"parts":[{"text":`); count < 1 {
		t.Fatalf("Gemini 流缺少文本增量：%s", streamResponse.body.String())
	}
	if !strings.Contains(streamResponse.body.String(), `"usageMetadata"`) {
		t.Fatalf("Gemini 流缺少最终 Usage：%s", streamResponse.body.String())
	}
}

func TestGeminiModelsAndEmbedding(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/models?key=test", nil)
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"models/mock-test"`) {
		t.Fatalf("Gemini Models 响应不符合预期：%d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-mock:embedContent?key=test", strings.NewReader(`{"content":{"parts":[{"text":"hello"}]}}`))
	response = httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"values":[`) {
		t.Fatalf("Gemini Embedding 响应不符合预期：%d %s", response.Code, response.Body.String())
	}
}
