package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const maxRequestBodySize = 1 << 20

type eventStream struct {
	response http.ResponseWriter
	flusher  http.Flusher
}

func newEventStream(response http.ResponseWriter) (*eventStream, bool) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		return nil, false
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	return &eventStream{response: response, flusher: flusher}, true
}

func (stream *eventStream) send(value any) bool {
	return stream.sendEvent("", value)
}

func (stream *eventStream) sendEvent(event string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if event != "" {
		if _, err = stream.response.Write([]byte("event: " + event + "\n")); err != nil {
			return false
		}
	}
	if _, err = stream.response.Write([]byte("data: ")); err != nil {
		return false
	}
	if _, err = stream.response.Write(data); err != nil {
		return false
	}
	if _, err = stream.response.Write([]byte("\n\n")); err != nil {
		return false
	}
	stream.flusher.Flush()
	return true
}

func (stream *eventStream) done() {
	_, _ = stream.response.Write([]byte("data: [DONE]\n\n"))
	stream.flusher.Flush()
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("请求体包含无效的尾随内容：%w", err)
	}
	return errors.New("请求体只能包含一个 JSON 值")
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeAPIError(response http.ResponseWriter, status int, errorType, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errorType,
			"param":   nil,
			"code":    nil,
		},
	})
}

func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("request panic", "error", recovered, "method", request.Method, "path", request.URL.Path)
				writeAPIError(response, http.StatusInternalServerError, "server_error", "服务内部错误")
			}
		}()
		next.ServeHTTP(response, request)
	})
}
