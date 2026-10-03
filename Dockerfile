FROM golang:1.26-alpine AS base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM base AS test
ENV CGO_ENABLED=0
RUN go vet ./... && go test ./...

FROM base AS security
ENV CGO_ENABLED=0
RUN go install github.com/securego/gosec/v2/cmd/gosec@v2.29.0 \
 && go install golang.org/x/vuln/cmd/govulncheck@v1.8.0 \
 && go install honnef.co/go/tools/cmd/staticcheck@v0.8.1 \
 && go install github.com/zricethezav/gitleaks/v8@v8.30.1
# Run every scanner even if an earlier one fails, so one CI run shows all findings.
RUN rc=0; \
    echo "== gitleaks";    gitleaks dir --no-banner --redact . || rc=1; \
    echo "== staticcheck"; staticcheck ./... || rc=1; \
    echo "== gosec";       gosec -quiet ./... || rc=1; \
    echo "== govulncheck"; govulncheck ./... || rc=1; \
    exit $rc

FROM base AS build
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/netscope .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates iproute2
COPY --from=build /out/netscope /usr/local/bin/netscope
ENTRYPOINT ["netscope"]
