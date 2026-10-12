.PHONY: help build run test race fmt vet check tidy clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "%-10s %s\n", $$1, $$2}'

build: ## Build ./bin/bc
	@mkdir -p bin
	go build -o bin/bc ./cmd/bc

run: build ## Build and run bc
	./bin/bc

test: ## Run all tests
	go test ./...

race: ## Run all tests with the race detector
	go test -race ./...

fmt: ## Format all Go source files
	gofmt -w $$(find . -type f -name '*.go' -not -path './.git/*')

vet: ## Run go vet
	go vet ./...

check: fmt vet test ## Format, vet, and test

tidy: ## Tidy Go module dependencies
	go mod tidy

clean: ## Remove build output
	rm -rf bin
