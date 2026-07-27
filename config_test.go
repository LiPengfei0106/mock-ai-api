package main

import (
	"testing"
	"time"
)

func TestLoadConfigSupportsRanges(t *testing.T) {
	setDefaultConfigEnv(t)
	t.Setenv("MOCK_ADDR", ":9090")
	t.Setenv("MOCK_MODEL", "mock-config")
	t.Setenv("MOCK_TTFT", "100ms..200ms")
	t.Setenv("MOCK_TPS", "20.5..40.5")
	t.Setenv("MOCK_LATENCY", "30ms..80ms")
	t.Setenv("MOCK_OUTPUT_TOKENS", "4..12")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != ":9090" || cfg.Model != "mock-config" {
		t.Fatalf("基础配置不符合预期：%+v", cfg)
	}
	if cfg.TTFT != (durationRange{Min: 100 * time.Millisecond, Max: 200 * time.Millisecond}) ||
		cfg.TPS != (floatRange{Min: 20.5, Max: 40.5}) ||
		cfg.Latency != (durationRange{Min: 30 * time.Millisecond, Max: 80 * time.Millisecond}) ||
		cfg.OutputTokens != (intRange{Min: 4, Max: 12}) {
		t.Fatalf("范围配置不符合预期：%+v", cfg)
	}
}

func TestLoadConfigSupportsExactValues(t *testing.T) {
	setDefaultConfigEnv(t)
	t.Setenv("MOCK_TTFT", "120ms")
	t.Setenv("MOCK_TPS", "25.5")
	t.Setenv("MOCK_LATENCY", "30ms")
	t.Setenv("MOCK_OUTPUT_TOKENS", "8")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TTFT.Min != 120*time.Millisecond || cfg.TTFT.Min != cfg.TTFT.Max ||
		cfg.TPS.Min != 25.5 || cfg.TPS.Min != cfg.TPS.Max ||
		cfg.Latency.Min != 30*time.Millisecond || cfg.OutputTokens.Min != 8 || cfg.OutputTokens.Min != cfg.OutputTokens.Max {
		t.Fatalf("单值配置不符合预期：%+v", cfg)
	}
}

func TestLoadConfigRejectsInvalidRanges(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "范围倒置", key: "MOCK_TPS", value: "30..10"},
		{name: "范围缺少上界", key: "MOCK_TTFT", value: "100ms.."},
		{name: "输出过大", key: "MOCK_OUTPUT_TOKENS", value: "1..1000001"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setDefaultConfigEnv(t)
			t.Setenv(test.key, test.value)
			if _, err := loadConfig(); err == nil {
				t.Fatalf("配置 %s=%q 应返回错误", test.key, test.value)
			}
		})
	}
}

func setDefaultConfigEnv(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{
		"MOCK_ADDR": ":8080", "MOCK_MODEL": "mock-gpt", "MOCK_TTFT": "0s",
		"MOCK_TPS": "0", "MOCK_LATENCY": "0s", "MOCK_OUTPUT_TOKENS": "16",
	} {
		t.Setenv(name, value)
	}
}
