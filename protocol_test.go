package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompletions(t *testing.T) {
	body := `{"model":"legacy","prompt":"hello","mock_input_tokens":4,"mock_output_tokens":2}`
	request := httptest.NewRequest(http.MethodPost, "/v1/completions", strings.NewReader(body))
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)

	var payload struct {
		Object  string `json:"object"`
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
		Usage usage `json:"usage"`
	}
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Object != "text_completion" || len(strings.Fields(payload.Choices[0].Text)) != 2 || payload.Usage.TotalTokens != 6 {
		t.Fatalf("Completions 响应不符合预期：%+v", payload)
	}
}

func TestOpenAICompletionsStream(t *testing.T) {
	body := `{"prompt":"hello","stream":true,"mock_output_tokens":2}`
	request := httptest.NewRequest(http.MethodPost, "/v1/completions", strings.NewReader(body))
	response := newStreamRecorder(time.Now())
	testServer().handler().ServeHTTP(response, request)

	if !strings.Contains(response.body.String(), `"object":"text_completion"`) ||
		!strings.Contains(response.body.String(), `"text":"`) ||
		!strings.Contains(response.body.String(), "data: [DONE]") {
		t.Fatalf("Completions 流不完整：%s", response.body.String())
	}
}

func TestOpenAIEmbeddings(t *testing.T) {
	body := `{"model":"embed","input":["hello","world"],"mock_input_tokens":7}`
	request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(body))
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)

	var payload struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 2 || len(payload.Data[0].Embedding) != 8 || payload.Usage.TotalTokens != 7 {
		t.Fatalf("Embeddings 响应不符合预期：%+v", payload)
	}
}

func TestEmbeddingTokenArrayIsSingleInput(t *testing.T) {
	for _, input := range []string{`[1,2,3]`, `[[1,2],[3,4]]`} {
		body := `{"input":` + input + `,"mock_input_tokens":4}`
		request := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(body))
		response := httptest.NewRecorder()
		testServer().handler().ServeHTTP(response, request)

		var payload struct {
			Data []any `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		expected := 1
		if strings.HasPrefix(input, "[[") {
			expected = 2
		}
		if len(payload.Data) != expected {
			t.Fatalf("输入 %s 生成 %d 个向量，期望 %d", input, len(payload.Data), expected)
		}
	}
}

func TestAnthropicMessage(t *testing.T) {
	body := `{"model":"claude-mock","max_tokens":10,"messages":[{"role":"user","content":"hello"}],"mock_input_tokens":6,"mock_output_tokens":2}`
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	request.Header.Set("anthropic-version", "2023-06-01")
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)

	var payload struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage anthropicUsage `json:"usage"`
	}
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Type != "message" || payload.Content[0].Type != "text" || len(strings.Fields(payload.Content[0].Text)) != 2 || payload.Usage.InputTokens != 6 || payload.Usage.OutputTokens != 2 {
		t.Fatalf("Anthropic Messages 响应不符合预期：%+v", payload)
	}
}

func TestAnthropicMessageStream(t *testing.T) {
	body := `{"stream":true,"max_tokens":2,"messages":[{"role":"user","content":"hello"}],"mock_input_tokens":5,"mock_output_tokens":2}`
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	request.Header.Set("anthropic-version", "2023-06-01")
	response := newStreamRecorder(time.Now())
	testServer().handler().ServeHTTP(response, request)

	stream := response.body.String()
	for _, event := range []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"} {
		if !strings.Contains(stream, "event: "+event+"\n") {
			t.Fatalf("Anthropic 流缺少 %s 事件：%s", event, stream)
		}
	}
	if count := strings.Count(stream, `"type":"text_delta"`); count < 1 {
		t.Fatal("Anthropic 流缺少文本增量")
	}
}

func TestAnthropicCountTokensAndModels(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}],"mock_input_tokens":9}`))
	request.Header.Set("anthropic-version", "2023-06-01")
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"input_tokens":9`) {
		t.Fatalf("Token Count 响应不符合预期：%d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("anthropic-version", "2023-06-01")
	response = httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"display_name":"mock-test"`) || !strings.Contains(response.Body.String(), `"has_more":false`) {
		t.Fatalf("Anthropic Models 响应不符合预期：%d %s", response.Code, response.Body.String())
	}
}

func TestAnthropicErrorShape(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"mock_tps":-1}`))
	request.Header.Set("anthropic-version", "2023-06-01")
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"type":"error"`) || !strings.Contains(response.Body.String(), `"type":"invalid_request_error"`) {
		t.Fatalf("Anthropic 错误响应不符合预期：%d %s", response.Code, response.Body.String())
	}
}

func TestAzureOpenAIChatAlias(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/openai/deployments/my-deployment/chat/completions?api-version=2024-10-21", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}],"mock_output_tokens":1}`))
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"model":"my-deployment"`) {
		t.Fatalf("Azure OpenAI 别名响应不符合预期：%d %s", response.Code, response.Body.String())
	}
}

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
