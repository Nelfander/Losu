# Variables
BINARY_NAME=losu
GO_FILES=cmd/logsum/main.go
NORMAL_GEN=bin/normal/normal_gen.go
STRESS_GEN=bin/stress/stress_gen.go

.PHONY: build run clean test build-all test-normal test-stress

# Compile the main binary
build:
	go build -o $(BINARY_NAME) $(GO_FILES)

# Build everything (Main app + both generators)
build-all: build
	go build -o normal_gen $(NORMAL_GEN)
	go build -o stress_gen $(STRESS_GEN)

# Run the normal generator (Simulates steady traffic)
test-normal:
	go run $(NORMAL_GEN)

# Run the stress generator (Simulates high-velocity spikes)
test-stress:
	go run $(STRESS_GEN)

# Run the app locally
run: build
	./$(BINARY_NAME)

# Clean up all binaries and logs
clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-linux normal_gen stress_gen alerts.log test.log