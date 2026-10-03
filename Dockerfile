FROM golang:1.26-alpine AS base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM base AS test
ENV CGO_ENABLED=0
RUN go vet ./... && go test ./...

FROM base AS build
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/netscope .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates iproute2
COPY --from=build /out/netscope /usr/local/bin/netscope
ENTRYPOINT ["netscope"]
