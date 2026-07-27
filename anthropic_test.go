package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
