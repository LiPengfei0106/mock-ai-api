package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIDocumentIncludesAllAPIRoutes(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	response := httptest.NewRecorder()
	testServer().handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q，期望 application/json", contentType)
	}

	var document struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("OpenAPI 文档不是有效 JSON：%v", err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("OpenAPI 版本 = %q，期望 3.1.0", document.OpenAPI)
	}

	expectedPaths := []string{
		"/healthz", "/v1/models", "/v1/models/{model}",
		"/v1/chat/completions", "/v1/completions", "/v1/embeddings", "/v1/responses",
		"/v1/messages", "/v1/messages/count_tokens",
		"/openai/deployments/{deployment}/chat/completions",
		"/openai/deployments/{deployment}/completions",
		"/openai/deployments/{deployment}/embeddings",
		"/v1beta/models", "/v1beta/models/{model}",
		"/api/tags", "/api/show", "/api/chat", "/api/generate", "/api/embed", "/api/embeddings",
	}
	for _, version := range []string{"v1", "v1beta"} {
		for _, action := range []string{"generateContent", "streamGenerateContent", "countTokens", "embedContent", "batchEmbedContents"} {
			expectedPaths = append(expectedPaths, "/"+version+"/models/{model}:"+action)
		}
	}
	for _, path := range expectedPaths {
		if _, ok := document.Paths[path]; !ok {
			t.Errorf("OpenAPI 文档缺少路径 %s", path)
		}
	}
}

func TestDocsPageUsesPinnedScalarAndOpenAPIEndpoint(t *testing.T) {
	handler := testServer().handler()

	request := httptest.NewRequest(http.MethodGet, "/docs", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d", response.Code, http.StatusOK)
	}
	for _, expected := range []string{
		"@scalar/api-reference@1.63.0",
		"data-url=\"/openapi.json\"",
		"integrity=\"sha384-",
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("测试页面缺少 %q", expected)
		}
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Error("测试页面缺少 Content-Security-Policy")
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != "/docs" {
		t.Fatalf("根路径响应 = %d %q，期望重定向到 /docs", response.Code, response.Header().Get("Location"))
	}
}
