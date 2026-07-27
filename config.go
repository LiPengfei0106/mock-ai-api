package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress      = ":8080"
	defaultModel        = "mock-gpt"
	defaultOutputTokens = 16
	maxDelay            = time.Hour
	maxTPS              = 1_000_000
	maxInputTokens      = 1_000_000_000
	hardMaxOutputTokens = 1_000_000
)

type durationRange struct {
	Min time.Duration
	Max time.Duration
}

func (value durationRange) String() string {
	if value.Min == value.Max {
		return value.Min.String()
	}
	return value.Min.String() + ".." + value.Max.String()
}

type floatRange struct {
	Min float64
	Max float64
}

func (value floatRange) String() string {
	minimum := strconv.FormatFloat(value.Min, 'f', -1, 64)
	if value.Min == value.Max {
		return minimum
	}
	return minimum + ".." + strconv.FormatFloat(value.Max, 'f', -1, 64)
}

type intRange struct {
	Min int
	Max int
}

func (value intRange) String() string {
	minimum := strconv.Itoa(value.Min)
	if value.Min == value.Max {
		return minimum
	}
	return minimum + ".." + strconv.Itoa(value.Max)
}

type config struct {
	Address      string
	Model        string
	TTFT         durationRange
	TPS          floatRange
	Latency      durationRange
	OutputTokens intRange
}

func loadConfig() (config, error) {
	cfg := config{
		Address: envOrDefault("MOCK_ADDR", defaultAddress),
		Model:   envOrDefault("MOCK_MODEL", defaultModel),
	}

	var err error
	if cfg.TTFT, err = parseDurationRangeEnv("MOCK_TTFT", 0); err != nil {
		return config{}, err
	}
	if cfg.TPS, err = parseFloatRangeEnv("MOCK_TPS", 0); err != nil {
		return config{}, err
	}
	if cfg.Latency, err = parseDurationRangeEnv("MOCK_LATENCY", 0); err != nil {
		return config{}, err
	}
	if cfg.OutputTokens, err = parseIntRangeEnv("MOCK_OUTPUT_TOKENS", defaultOutputTokens); err != nil {
		return config{}, err
	}

	if strings.TrimSpace(cfg.Address) == "" {
		return config{}, fmt.Errorf("MOCK_ADDR 不能为空")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return config{}, fmt.Errorf("MOCK_MODEL 不能为空")
	}
	if cfg.TTFT.Min < 0 || cfg.TTFT.Max > maxDelay {
		return config{}, fmt.Errorf("MOCK_TTFT 必须在 0 到 %s 之间", maxDelay)
	}
	if cfg.Latency.Min < 0 || cfg.Latency.Max > maxDelay {
		return config{}, fmt.Errorf("MOCK_LATENCY 必须在 0 到 %s 之间", maxDelay)
	}
	if math.IsNaN(cfg.TPS.Min) || math.IsNaN(cfg.TPS.Max) || math.IsInf(cfg.TPS.Min, 0) || math.IsInf(cfg.TPS.Max, 0) || cfg.TPS.Min < 0 || cfg.TPS.Max > maxTPS {
		return config{}, fmt.Errorf("MOCK_TPS 必须在 0 到 %d 之间", maxTPS)
	}
	if cfg.OutputTokens.Min < 1 || cfg.OutputTokens.Max > hardMaxOutputTokens {
		return config{}, fmt.Errorf("MOCK_OUTPUT_TOKENS 必须在 1 到 %d 之间", hardMaxOutputTokens)
	}
	return cfg, nil
}

func envOrDefault(name, fallback string) string {
	if value, exists := os.LookupEnv(name); exists {
		return value
	}
	return fallback
}

func parseDurationRangeEnv(name string, fallback time.Duration) (durationRange, error) {
	value, exists := os.LookupEnv(name)
	if !exists || value == "" {
		return durationRange{Min: fallback, Max: fallback}, nil
	}
	parts, err := splitRange(name, value)
	if err != nil {
		return durationRange{}, err
	}
	minimum, err := time.ParseDuration(parts[0])
	if err != nil {
		return durationRange{}, fmt.Errorf("%s 最小值不是有效时长：%w", name, err)
	}
	maximum, err := time.ParseDuration(parts[1])
	if err != nil {
		return durationRange{}, fmt.Errorf("%s 最大值不是有效时长：%w", name, err)
	}
	if minimum > maximum {
		return durationRange{}, fmt.Errorf("%s 最小值不能大于最大值", name)
	}
	return durationRange{Min: minimum, Max: maximum}, nil
}

func parseFloatRangeEnv(name string, fallback float64) (floatRange, error) {
	value, exists := os.LookupEnv(name)
	if !exists || value == "" {
		return floatRange{Min: fallback, Max: fallback}, nil
	}
	parts, err := splitRange(name, value)
	if err != nil {
		return floatRange{}, err
	}
	minimum, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return floatRange{}, fmt.Errorf("%s 最小值不是有效数字：%w", name, err)
	}
	maximum, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return floatRange{}, fmt.Errorf("%s 最大值不是有效数字：%w", name, err)
	}
	if minimum > maximum {
		return floatRange{}, fmt.Errorf("%s 最小值不能大于最大值", name)
	}
	return floatRange{Min: minimum, Max: maximum}, nil
}

func parseIntRangeEnv(name string, fallback int) (intRange, error) {
	value, exists := os.LookupEnv(name)
	if !exists || value == "" {
		return intRange{Min: fallback, Max: fallback}, nil
	}
	return parseIntRange(name, value)
}

func parseIntRange(name, value string) (intRange, error) {
	parts, err := splitRange(name, value)
	if err != nil {
		return intRange{}, err
	}
	minimum, err := strconv.Atoi(parts[0])
	if err != nil {
		return intRange{}, fmt.Errorf("%s 最小值不是有效整数：%w", name, err)
	}
	maximum, err := strconv.Atoi(parts[1])
	if err != nil {
		return intRange{}, fmt.Errorf("%s 最大值不是有效整数：%w", name, err)
	}
	if minimum > maximum {
		return intRange{}, fmt.Errorf("%s 最小值不能大于最大值", name)
	}
	return intRange{Min: minimum, Max: maximum}, nil
}

func splitRange(name, value string) ([2]string, error) {
	parts := strings.Split(value, "..")
	if len(parts) == 1 {
		trimmed := strings.TrimSpace(parts[0])
		if trimmed == "" {
			return [2]string{}, fmt.Errorf("%s 不能为空", name)
		}
		return [2]string{trimmed, trimmed}, nil
	}
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return [2]string{}, fmt.Errorf("%s 必须是单值或 min..max 范围", name)
	}
	return [2]string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}, nil
}

func defaultMaxProcs() int {
	return runtime.GOMAXPROCS(0)
}
