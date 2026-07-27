package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer() *server {
	return newServer(config{
		Address:      ":8080",
		Model:        "mock-test",
		OutputTokens: intRange{Min: 3, Max: 3},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestModels(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d", response.Code, http.StatusOK)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].ID != "mock-test" {
		t.Fatalf("模型列表不符合预期：%+v", payload.Data)
	}
}

func TestChatCompletionReturnsUsage(t *testing.T) {
	body := `{"model":"custom","messages":[{"role":"user","content":"hello"}],"mock_input_tokens":7,"mock_output_tokens":2}`
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage usage `json:"usage"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "custom" || len(strings.Fields(payload.Choices[0].Message.Content)) != 2 {
		t.Fatalf("响应内容不符合预期：%+v", payload)
	}
	if payload.Usage.PromptTokens != 7 || payload.Usage.CompletionTokens != 2 || payload.Usage.TotalTokens != 9 {
		t.Fatalf("Usage 不符合预期：%+v", payload.Usage)
	}
}

func TestResponsesReturnsUsage(t *testing.T) {
	body := `{"input":"hello","max_output_tokens":2,"mock_input_tokens":4}`
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	var payload struct {
		OutputText string `json:"output_text"`
		Usage      usage  `json:"usage"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(payload.OutputText)) != 2 || payload.Usage.InputTokens != 4 || payload.Usage.OutputTokens != 2 || payload.Usage.TotalTokens != 6 {
		t.Fatalf("响应不符合预期：%+v", payload)
	}
}

func TestNonStreamControlsLatency(t *testing.T) {
	body := `{"messages":[],"mock_latency_ms":40,"mock_output_tokens":1}`
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	response := httptest.NewRecorder()
	startedAt := time.Now()
	testServer().handler().ServeHTTP(response, request)
	elapsed := time.Since(startedAt)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	if elapsed < 30*time.Millisecond {
		t.Fatalf("非流式延迟 = %s，明显小于配置值", elapsed)
	}
}

func TestChatStreamControlsTTFTAndTPS(t *testing.T) {
	body := `{"stream":true,"messages":[{"role":"user","content":"hello"}],"mock_ttft_ms":40,"mock_tps":20,"mock_output_tokens":2}`
	startedAt := time.Now()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	response := newStreamRecorder(startedAt)
	testServer().handler().ServeHTTP(response, request)

	var tokenTimes []time.Duration
	var gotUsage bool
	for index, chunk := range response.chunks {
		line := strings.TrimSpace(chunk)
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		for _, choice := range event.Choices {
			if choice.Delta.Content != "" {
				tokenTimes = append(tokenTimes, response.flushTimes[index])
			}
		}
		gotUsage = gotUsage || event.Usage != nil
	}
	if len(tokenTimes) != 2 {
		t.Fatalf("文本增量数 = %d，期望 2", len(tokenTimes))
	}
	if tokenTimes[0] < 30*time.Millisecond {
		t.Fatalf("TTFT = %s，明显小于配置值", tokenTimes[0])
	}
	if interval := tokenTimes[1] - tokenTimes[0]; interval < 40*time.Millisecond {
		t.Fatalf("Token 间隔 = %s，明显小于 20 TPS 对应间隔", interval)
	}
	if !gotUsage {
		t.Fatal("流式响应未返回 Usage")
	}
}

func TestResponsesStreamReturnsDeltaAndUsage(t *testing.T) {
	body := `{"stream":true,"input":"hello","mock_input_tokens":5,"mock_output_tokens":2}`
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	response := newStreamRecorder(time.Now())
	testServer().handler().ServeHTTP(response, request)

	var deltas int
	var completed struct {
		Type     string `json:"type"`
		Response struct {
			Output []any `json:"output"`
			Usage  usage `json:"usage"`
		} `json:"response"`
	}
	for _, chunk := range response.chunks {
		line := strings.TrimSpace(chunk)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := []byte(strings.TrimPrefix(line, "data: "))
		var eventType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &eventType); err != nil {
			t.Fatal(err)
		}
		if eventType.Type == "response.output_text.delta" {
			deltas++
		}
		if eventType.Type == "response.completed" {
			if err := json.Unmarshal(data, &completed); err != nil {
				t.Fatal(err)
			}
		}
	}
	if deltas < 1 {
		t.Fatal("Responses 流缺少文本增量")
	}
	if completed.Type != "response.completed" || len(completed.Response.Output) != 1 {
		t.Fatalf("缺少完整的 response.completed 事件：%+v", completed)
	}
	if completed.Response.Usage.InputTokens != 5 || completed.Response.Usage.OutputTokens != 2 || completed.Response.Usage.TotalTokens != 7 {
		t.Fatalf("流式 Usage 不符合预期：%+v", completed.Response.Usage)
	}
}

type streamRecorder struct {
	header     http.Header
	body       bytes.Buffer
	status     int
	flushedAt  int
	startedAt  time.Time
	chunks     []string
	flushTimes []time.Duration
}

func newStreamRecorder(startedAt time.Time) *streamRecorder {
	return &streamRecorder{header: make(http.Header), startedAt: startedAt}
}

func (recorder *streamRecorder) Header() http.Header {
	return recorder.header
}

func (recorder *streamRecorder) WriteHeader(status int) {
	recorder.status = status
}

func (recorder *streamRecorder) Write(data []byte) (int, error) {
	return recorder.body.Write(data)
}

func (recorder *streamRecorder) Flush() {
	data := recorder.body.Bytes()
	recorder.chunks = append(recorder.chunks, string(data[recorder.flushedAt:]))
	recorder.flushedAt = len(data)
	recorder.flushTimes = append(recorder.flushTimes, time.Since(recorder.startedAt))
}

func BenchmarkChatCompletion(b *testing.B) {
	handler := testServer().handler()
	body := []byte(`{"messages":[{"role":"user","content":"hello"}],"mock_output_tokens":1}`)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				b.Fatalf("状态码 = %d", response.Code)
			}
		}
	})
}
