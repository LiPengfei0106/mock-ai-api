package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"
	"unicode/utf8"
)

type completionRequest struct {
	Model               string          `json:"model"`
	Messages            []message       `json:"messages"`
	Input               json.RawMessage `json:"input"`
	Prompt              json.RawMessage `json:"prompt"`
	System              json.RawMessage `json:"system"`
	Stream              bool            `json:"stream"`
	MaxTokens           *int            `json:"max_tokens"`
	MaxCompletionTokens *int            `json:"max_completion_tokens"`
	MaxOutputTokens     *int            `json:"max_output_tokens"`
	MockTTFTMS          *int64          `json:"mock_ttft_ms"`
	MockTPS             *float64        `json:"mock_tps"`
	MockLatencyMS       *int64          `json:"mock_latency_ms"`
	MockInputTokens     *int            `json:"mock_input_tokens"`
	MockOutputTokens    *int            `json:"mock_output_tokens"`
	StreamOptions       map[string]any  `json:"stream_options"`
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type requestOptions struct {
	Model        string
	TTFT         time.Duration
	TPS          float64
	Latency      time.Duration
	InputTokens  int
	OutputTokens int
	MaxTokens    int
	Generation   modelGeneration
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens"`
}

func (s *server) parseRequest(response http.ResponseWriter, request *http.Request, responsesAPI bool) (completionRequest, requestOptions, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodySize)
	decoder := json.NewDecoder(request.Body)
	var payload completionRequest
	if err := decoder.Decode(&payload); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid_request_error", "请求体不是有效 JSON："+err.Error())
		return completionRequest{}, requestOptions{}, false
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid_request_error", err.Error())
		return completionRequest{}, requestOptions{}, false
	}

	options, err := s.resolveOptions(payload, responsesAPI)
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid_request_error", err.Error())
		return completionRequest{}, requestOptions{}, false
	}
	return payload, options, true
}

func (s *server) resolveOptions(payload completionRequest, responsesAPI bool) (requestOptions, error) {
	random := newSampler(s.randomSeed + s.sampleSequence.Add(1))
	options := requestOptions{
		Model:        payload.Model,
		TTFT:         random.duration(s.config.TTFT),
		TPS:          random.float64(s.config.TPS),
		Latency:      random.duration(s.config.Latency),
		OutputTokens: random.int(s.config.OutputTokens),
		MaxTokens:    hardMaxOutputTokens,
	}
	if options.Model == "" {
		options.Model = s.config.Model
	}
	maxTokens := payload.MaxTokens
	if payload.MaxCompletionTokens != nil {
		maxTokens = payload.MaxCompletionTokens
	}
	if responsesAPI && payload.MaxOutputTokens != nil {
		maxTokens = payload.MaxOutputTokens
	}
	if maxTokens != nil {
		if *maxTokens < 1 {
			return requestOptions{}, errors.New("请求最大输出 Token 数必须大于 0")
		}
		options.MaxTokens = min(*maxTokens, hardMaxOutputTokens)
	}
	if payload.MockOutputTokens != nil {
		if *payload.MockOutputTokens < 1 || *payload.MockOutputTokens > hardMaxOutputTokens {
			return requestOptions{}, fmt.Errorf("mock_output_tokens 必须在 1 到 %d 之间", hardMaxOutputTokens)
		}
		options.OutputTokens = *payload.MockOutputTokens
	}
	if options.OutputTokens > options.MaxTokens {
		options.OutputTokens = options.MaxTokens
	}
	options.Generation = s.model.generate(options.OutputTokens)

	if payload.MockTTFTMS != nil {
		if *payload.MockTTFTMS < 0 || *payload.MockTTFTMS > maxDelay.Milliseconds() {
			return requestOptions{}, fmt.Errorf("mock_ttft_ms 必须在 0 到 %d 之间", maxDelay.Milliseconds())
		}
		options.TTFT = time.Duration(*payload.MockTTFTMS) * time.Millisecond
	}
	if payload.MockLatencyMS != nil {
		if *payload.MockLatencyMS < 0 || *payload.MockLatencyMS > maxDelay.Milliseconds() {
			return requestOptions{}, fmt.Errorf("mock_latency_ms 必须在 0 到 %d 之间", maxDelay.Milliseconds())
		}
		options.Latency = time.Duration(*payload.MockLatencyMS) * time.Millisecond
	}
	if payload.MockTPS != nil {
		if math.IsNaN(*payload.MockTPS) || math.IsInf(*payload.MockTPS, 0) || *payload.MockTPS < 0 || *payload.MockTPS > maxTPS {
			return requestOptions{}, fmt.Errorf("mock_tps 必须在 0 到 %d 之间", maxTPS)
		}
		options.TPS = *payload.MockTPS
	}

	if payload.MockInputTokens != nil {
		if *payload.MockInputTokens < 0 || *payload.MockInputTokens > maxInputTokens {
			return requestOptions{}, fmt.Errorf("mock_input_tokens 必须在 0 到 %d 之间", maxInputTokens)
		}
		options.InputTokens = *payload.MockInputTokens
	} else if responsesAPI {
		options.InputTokens = estimateInputTokens(payload.Input)
	} else {
		size := utf8.RuneCount(payload.Prompt) + utf8.RuneCount(payload.System)
		for _, item := range payload.Messages {
			size += utf8.RuneCount(item.Content)
		}
		options.InputTokens = estimatedTokens(size)
	}
	return options, nil
}

func chatUsage(options requestOptions) usage {
	return usage{
		PromptTokens:     options.InputTokens,
		CompletionTokens: options.OutputTokens,
		TotalTokens:      options.InputTokens + options.OutputTokens,
	}
}

func responseUsage(options requestOptions) usage {
	return usage{
		InputTokens:  options.InputTokens,
		OutputTokens: options.OutputTokens,
		TotalTokens:  options.InputTokens + options.OutputTokens,
	}
}

func estimateInputTokens(raw json.RawMessage) int {
	return estimatedTokens(utf8.RuneCount(raw))
}

func estimatedTokens(runes int) int {
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}

// sampler 是请求内独占的伪随机生成器，避免万级并发下争用全局随机数锁。
type sampler struct {
	state uint64
}

func newSampler(seed uint64) *sampler {
	return &sampler{state: seed}
}

func (random *sampler) next() uint64 {
	random.state += 0x9e3779b97f4a7c15
	value := random.state
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func (random *sampler) int(value intRange) int {
	if value.Min == value.Max {
		return value.Min
	}
	span := uint64(value.Max-value.Min) + 1
	return value.Min + int(random.next()%span)
}

func (random *sampler) duration(value durationRange) time.Duration {
	if value.Min == value.Max {
		return value.Min
	}
	span := uint64(value.Max-value.Min) + 1
	return value.Min + time.Duration(random.next()%span)
}

func (random *sampler) float64(value floatRange) float64 {
	if value.Min == value.Max {
		return value.Min
	}
	// 取高 53 位构造 [0, 1) 均匀分布浮点数。
	fraction := float64(random.next()>>11) * (1.0 / (1 << 53))
	return value.Min + fraction*(value.Max-value.Min)
}
