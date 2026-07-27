package main

import (
	"embed"
	"net/http"
)

//go:embed web/index.html web/openapi.json
var webAssets embed.FS

var (
	scalarPage  = mustReadWebAsset("web/index.html")
	openAPISpec = mustReadWebAsset("web/openapi.json")
)

func mustReadWebAsset(name string) []byte {
	content, err := webAssets.ReadFile(name)
	if err != nil {
		panic("failed to read embedded web asset: " + err.Error())
	}
	return content
}

func handleDocsRedirect(response http.ResponseWriter, request *http.Request) {
	http.Redirect(response, request, "/docs", http.StatusTemporaryRedirect)
}

func handleDocs(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; img-src data: https:; font-src data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(scalarPage)
}

func handleOpenAPI(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "public, max-age=300")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(openAPISpec)
}
