export CGO_ENABLED := 0
BIN := bin/netscope
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build test release image run clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN) .

test:
	go vet ./...
	go test ./...

release:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; \
		[ "$$os" = windows ] && ext=.exe; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="-s -w" \
			-o dist/netscope-$$os-$$arch$$ext . || exit 1; \
	done

image:
	docker build -t netscope:latest .

run:
	docker compose run --rm netscope

clean:
	rm -rf bin dist
