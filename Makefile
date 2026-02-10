.PHONY: build build-linux clean

BINARY=mini-docker

# Build for current platform
build:
	go build -o build/$(BINARY) .

# Cross-compile for Linux (for testing in VM)
build-linux:
	GOOS=linux GOARCH=amd64 go build -o build/$(BINARY)-linux .

# Build for Linux ARM64
build-linux-arm:
	GOOS=linux GOARCH=arm64 go build -o build/$(BINARY)-linux-arm .

clean:
	rm -rf build/

# Format code
fmt:
	go fmt ./...

# Run vet
vet:
	go vet ./...
