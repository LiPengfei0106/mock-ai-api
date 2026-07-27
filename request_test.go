package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInvalidMockParameter(t *testing.T) {
	body := `{"messages":[],"mock_tps":-1}`
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，期望 %d", response.Code, http.StatusBadRequest)
	}
}

func TestResolveOptionsSamplesConfiguredRanges(t *testing.T) {
	application := newServer(config{
		Model:        "mock-random",
		TTFT:         durationRange{Min: 10 * time.Millisecond, Max: 20 * time.Millisecond},
		TPS:          floatRange{Min: 10, Max: 20},
		Latency:      durationRange{Min: 30 * time.Millisecond, Max: 40 * time.Millisecond},
		OutputTokens: intRange{Min: 4, Max: 8},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	seenTTFT := make(map[time.Duration]struct{})
	seenTPS := make(map[float64]struct{})
	seenLatency := make(map[time.Duration]struct{})
	seenOutput := make(map[int]struct{})
	for index := 0; index < 500; index++ {
		options, err := application.resolveOptions(completionRequest{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if options.TTFT < 10*time.Millisecond || options.TTFT > 20*time.Millisecond {
			t.Fatalf("TTFT 超出范围：%s", options.TTFT)
		}
		if options.TPS < 10 || options.TPS > 20 {
			t.Fatalf("TPS 超出范围：%f", options.TPS)
		}
		if options.Latency < 30*time.Millisecond || options.Latency > 40*time.Millisecond {
			t.Fatalf("Latency 超出范围：%s", options.Latency)
		}
		if options.OutputTokens < 4 || options.OutputTokens > 8 || options.OutputTokens > options.MaxTokens {
			t.Fatalf("输出 Token 不符合范围或上限：output=%d max=%d", options.OutputTokens, options.MaxTokens)
		}
		if len(options.Generation.Tokens) != options.OutputTokens || len(strings.Fields(options.Generation.Text)) != options.OutputTokens {
			t.Fatalf("模型输出与 Token 数不一致：%+v", options)
		}
		seenTTFT[options.TTFT] = struct{}{}
		seenTPS[options.TPS] = struct{}{}
		seenLatency[options.Latency] = struct{}{}
		seenOutput[options.OutputTokens] = struct{}{}
	}
	for name, count := range map[string]int{
		"TTFT": len(seenTTFT), "TPS": len(seenTPS),
		"Latency": len(seenLatency), "OutputTokens": len(seenOutput),
	} {
		if count < 2 {
			t.Fatalf("%s 未产生不同的随机值", name)
		}
	}
}

func TestResolveOptionsRequestValuesOverrideRanges(t *testing.T) {
	application := newServer(config{
		Model:        "mock-random",
		TTFT:         durationRange{Min: time.Second, Max: 2 * time.Second},
		TPS:          floatRange{Min: 10, Max: 20},
		Latency:      durationRange{Min: time.Second, Max: 2 * time.Second},
		OutputTokens: intRange{Min: 4, Max: 8},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ttft := int64(123)
	tps := 44.5
	latency := int64(321)
	input := 99
	output := 9
	options, err := application.resolveOptions(completionRequest{
		MockTTFTMS:       &ttft,
		MockTPS:          &tps,
		MockLatencyMS:    &latency,
		MockInputTokens:  &input,
		MockOutputTokens: &output,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if options.TTFT != 123*time.Millisecond || options.TPS != tps || options.Latency != 321*time.Millisecond || options.InputTokens != input || options.OutputTokens != output || len(options.Generation.Tokens) != output {
		t.Fatalf("请求覆盖未完整生效：%+v", options)
	}
}

func TestStandardMaxTokensCapsOutput(t *testing.T) {
	application := newServer(config{
		Model:        "mock-random",
		OutputTokens: intRange{Min: 8, Max: 8},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	maximum := 3
	options, err := application.resolveOptions(completionRequest{MaxTokens: &maximum}, false)
	if err != nil {
		t.Fatal(err)
	}
	if options.MaxTokens != 3 || options.OutputTokens != 3 || len(options.Generation.Tokens) != 3 {
		t.Fatalf("标准 max_tokens 未限制模型输出：%+v", options)
	}
}
