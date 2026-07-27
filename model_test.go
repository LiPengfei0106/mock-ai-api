package main

import (
	"testing"
	"time"
)

func TestModelSimulatorUsesVocabularyAndVariesOutput(t *testing.T) {
	model := newModelSimulator(42)
	vocabulary := make(map[string]struct{}, len(modelVocabulary))
	for _, token := range modelVocabulary {
		vocabulary[token] = struct{}{}
	}
	seen := make(map[string]struct{})
	for index := 0; index < 20; index++ {
		generation := model.generate(12)
		if len(generation.Tokens) != 12 || generation.Text == "" {
			t.Fatalf("模型输出长度不符合预期：%+v", generation)
		}
		for _, token := range generation.Tokens {
			if _, ok := vocabulary[token]; !ok {
				t.Fatalf("生成了词表外 Token：%q", token)
			}
		}
		seen[generation.Text] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatal("多次模型请求未产生不同文本")
	}
}

func TestModelChunksScaleWithTPS(t *testing.T) {
	model := newModelSimulator(42)
	generation := model.generate(100)
	lowTPS := model.chunks(generation, 10)
	highTPS := model.chunks(generation, 200)
	if len(highTPS) >= len(lowTPS) {
		t.Fatalf("高 TPS 未减少事件数：low=%d high=%d", len(lowTPS), len(highTPS))
	}
	assertChunks := func(t *testing.T, chunks []modelChunk, expectedDuration time.Duration) {
		t.Helper()
		var tokens int
		var duration time.Duration
		for _, chunk := range chunks {
			if chunk.Text == "" || chunk.TokenCount < 1 {
				t.Fatalf("无效模型分块：%+v", chunk)
			}
			tokens += chunk.TokenCount
			duration += chunk.Delay
		}
		if tokens != 100 || duration < expectedDuration-time.Millisecond || duration > expectedDuration+time.Millisecond {
			t.Fatalf("分块计划不符合预期：tokens=%d duration=%s expected=%s", tokens, duration, expectedDuration)
		}
	}
	assertChunks(t, lowTPS, 10*time.Second)
	assertChunks(t, highTPS, 500*time.Millisecond)
}
