ARG GO_IMAGE=golang:1.22-alpine
ARG RUNTIME_IMAGE=alpine:3.20

FROM ${GO_IMAGE} AS build

WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY web ./web
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mock-ai-api .

FROM ${RUNTIME_IMAGE}

RUN addgroup -S app && adduser -S -G app app
COPY --from=build /out/mock-ai-api /usr/local/bin/mock-ai-api
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mock-ai-api"]
