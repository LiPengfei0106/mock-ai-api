package main

import (
	"strings"
	"sync/atomic"
	"time"
)

const streamEventsPerSecond = 20

// 内置词表刻意保持通用，不依赖具体平台协议或请求主题。
var modelVocabulary = []string{
	"analysis", "approach", "available", "based", "because", "clear", "context", "data",
	"details", "effective", "ensure", "example", "expected", "first", "following", "important",
	"include", "information", "method", "model", "next", "output", "process", "provide",
	"request", "response", "result", "service", "simple", "system", "testing", "therefore",
	"this", "use", "value", "when", "with", "works", "can", "will",
}

type modelSimulator struct {
	seed     uint64
	sequence atomic.Uint64
}

type modelGeneration struct {
	Tokens []string
	Text   string
}

type modelChunk struct {
	Text       string
	TokenCount int
	Delay      time.Duration
}

func newModelSimulator(seed uint64) *modelSimulator {
	return &modelSimulator{seed: seed}
}

func (model *modelSimulator) generate(tokenCount int) modelGeneration {
	random := newSampler(model.seed + model.sequence.Add(1))
	tokens := make([]string, tokenCount)
	for index := range tokens {
		tokens[index] = modelVocabulary[random.next()%uint64(len(modelVocabulary))]
	}
	return modelGeneration{Tokens: tokens, Text: renderModelTokens(tokens, true)}
}

// chunks 将模型 Token 合并为动态事件。TPS 越高，单事件携带的 Token 越多，
// 每个事件后的等待时间严格按该事件 Token 数除以 TPS 计算。
func (model *modelSimulator) chunks(generation modelGeneration, tps float64) []modelChunk {
	if len(generation.Tokens) == 0 {
		return nil
	}
	random := newSampler(model.seed + model.sequence.Add(1))
	baseSize := 1
	if tps > 0 {
		baseSize = max(1, int(tps/streamEventsPerSecond+0.5))
	} else {
		baseSize = 4
	}
	chunks := make([]modelChunk, 0, (len(generation.Tokens)+baseSize-1)/baseSize)
	for offset := 0; offset < len(generation.Tokens); {
		minimum := max(1, baseSize/2)
		maximum := max(minimum, baseSize+baseSize/2)
		size := minimum
		if maximum > minimum {
			size += int(random.next() % uint64(maximum-minimum+1))
		}
		size = min(size, len(generation.Tokens)-offset)
		delay := time.Duration(0)
		if tps > 0 {
			delay = time.Duration(float64(time.Second) * float64(size) / tps)
		}
		chunks = append(chunks, modelChunk{
			Text:       renderModelTokens(generation.Tokens[offset:offset+size], offset == 0),
			TokenCount: size,
			Delay:      delay,
		})
		offset += size
	}
	return chunks
}

func renderModelTokens(tokens []string, first bool) string {
	if len(tokens) == 0 {
		return ""
	}
	var builder strings.Builder
	for index, token := range tokens {
		if index > 0 || !first {
			builder.WriteByte(' ')
		}
		if index == 0 && first {
			builder.WriteString(strings.ToUpper(token[:1]))
			builder.WriteString(token[1:])
		} else {
			builder.WriteString(token)
		}
	}
	return builder.String()
}
