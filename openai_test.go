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

func TestAzureOpenAIChatAlias(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/openai/deployments/my-deployment/chat/completions?api-version=2024-10-21", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}],"mock_output_tokens":1}`))
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"model":"my-deployment"`) {
		t.Fatalf("Azure OpenAI 别名响应不符合预期：%d %s", response.Code, response.Body.String())
	}
}
