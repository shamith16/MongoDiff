BINARY_NAME=mongodiff
BUILD_DIR=bin

.PHONY: build test vet clean build-all

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/mongodiff

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf $(BUILD_DIR)

build-all:
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/mongodiff
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/mongodiff
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/mongodiff
