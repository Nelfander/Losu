BINARY_NAME=losu
GO_FILES=cmd/logsum/main.go
NORMAL_GEN=bin/generators/normal/main.go
JSON_GEN=bin/generators/json/json_gen.go

.PHONY: build run run-web run-both run-reset clean test test-normal test-json

# Compile the main binary
build:
	go build -o $(BINARY_NAME) $(GO_FILES)

# Run the app (TUI only)
run: build
	./$(BINARY_NAME) --ui=tui

# Run with web dashboard only
run-web: build
	./$(BINARY_NAME) --ui=web

# Run with both TUI and web dashboard
run-both: build
	./$(BINARY_NAME) --ui=both

# Wipe stats and start fresh
run-reset: build
	./$(BINARY_NAME) --reset

# Run tests
test:
	go test ./... -race

# Run the normal logfmt generator (simulates steady production traffic)
test-normal:
	go run $(NORMAL_GEN)

# Run the JSON generator (simulates Docker container log format)
test-json:
	go run $(JSON_GEN)

# Clean up binaries and logs
clean:
	rm -f $(BINARY_NAME) alerts.log
	rm -f logs/test.log logs/test2.log