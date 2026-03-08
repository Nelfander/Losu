# LogSum: High-Performance Log Streamer

A high-performance, concurrent log tailing and analytics engine built in Go.

## Architecture
[File] -> (fsnotify) -> [Tailer] -> (chan RawLog) -> [Parser] -> (chan LogEvent) -> [Aggregator] -> [UI]

## Core Guarantees
1. **Zero Leaks**: Every goroutine is tracked with a `context.Context`.
2. **Memory Bounded**: All channels have a buffer limit of 100. If the UI freezes, the whole pipeline pauses rather than exploding RAM.
3. **Graceful Exit**: On `Ctrl+C`, the program flushes the remaining logs and closes files before exiting.

## Backpressure Strategy
We use **blocking sends** on bounded channels. If the Parser is slow, the Tailer waits. This ensures we never process more than the CPU can handle.


## 🛠 Features (Phase 1 Complete)
- **Concurrent Pipeline**: Multi-worker architecture using Go channels and sync primitives.
- **FS-Watcher**: Real-time file system monitoring for zero-latency log detection.
- **Resilient Parsing**: Custom-built logic to handle Windows UTF-16, BOM (Byte Order Marks), and Null-bytes.
- **Live Dashboard**: ANSI-powered terminal UI for real-time error/info aggregation.

## 🚀 Quick Start
```bash
# Run with default INFO filter
go run cmd/logsum/main.go

# Run with custom filter level
go run cmd/logsum/main.go --level=ERROR
```


## 🛠 <b>Development History</b>
<details><summary>(Click to expand)</summary>

<details>
<summary><b>March 8, 2026: Concurrent Log Processing Engine & Windows Resilience</b> (Click to expand)</summary>

#### Phase 1: High-Concurrency Data Pipeline
* **Producer-Consumer Architecture**: Engineered a multi-threaded pipeline using buffered channels (`rawLineChan`, `eventChan`) to decouple log tailing from parsing logic.
* **Worker Pool Orchestration**: Implemented a scalable worker pool in `internal/pipeline`, utilizing `sync.WaitGroup` for deterministic lifecycle management and `context.Context` for graceful shutdown propagation.
* **Thread-Safe State Management**: Developed an `Aggregator` component using `sync.RWMutex` to allow concurrent log analysis without data races, supporting real-time statistical snapshots.

#### Phase 2: Cross-Platform Encoding & Regex Resilience
* **UTF-16/Null-Byte Sanitization**: Resolved a critical "Invisible Data" bug caused by Windows PowerShell's 16-bit encoding by implementing a surgical null-byte (`\x00`) removal layer in the parser.
* **BOM (Byte Order Mark) Handling**: Hardened the parsing logic to detect and strip `\ufeff` signatures, ensuring 100% Regex match rates regardless of the source file's byte order mark.
* **Universal Pattern Matching**: Refactored the `RegexParser` to utilize non-greedy matches (`.*?`), allowing the engine to successfully extract Timestamps, Levels, and Messages from non-standard system logs.

#### Phase 3: Real-Time Observability & Filtering
* **ANSI-Driven Live Dashboard**: Built a high-performance Terminal UI (TUI) in `internal/ui` using ANSI escape codes for screen-buffer management, providing a live-updating "Scoreboard" of log metrics.
* **Log Severity Filtering**: Integrated a dynamic severity gate (DEBUG, INFO, WARN, ERROR) via Go `flag` package, allowing developers to filter out high-volume noise in real-time.
* **Graceful Shutdown Logic**: Finalized the main loop with a clean "Drain" sequence: closing input pipes, waiting for worker completion, and rendering a "Final Report" of all processed data.

</details>
</details>