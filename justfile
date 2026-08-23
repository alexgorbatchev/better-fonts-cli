# Default recipe: list available recipes
default:
    @just --list

# Build compiled binary strictly into bin/
build:
    @mkdir -p bin
    go build -o bin/better-fonts ./cmd/better-fonts

# Install binary to GOPATH bin
install:
    go install ./cmd/better-fonts

# Run better-fonts CLI with arguments
run *ARGS:
    go run ./cmd/better-fonts {{ARGS}}

# Run all unit tests with race detector
test:
    go test -race ./...

# Format Go code
fmt:
    go fmt ./...

# Check module hygiene and run vet
lint:
    go mod tidy -diff
    go vet ./...

# Run static code analysis
vet:
    go vet ./...

# Clean build artifacts and temporary files
clean:
    rm -rf bin/ .tmp/ coverage.out
