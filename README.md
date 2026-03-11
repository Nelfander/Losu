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
<summary><b>March 11, 2026: Architectural Refinement & Real-Time Throughput Visualization</b> (Click to expand)</summary>

#### Phase 1: Data Model Standardization
* **Unified Model Package**: Established `internal/model` as the neutral ground for shared data structures, resolving circular dependency issues between the UI and Aggregator packages.
* **MessageStat Structural Upgrade**: Transitioned from simple frequency maps to a robust `MessageStat` struct. This allows the system to pair severity levels (ERROR/WARN) with message frequency, enabling accurate color-coding in high-frequency data views.
* **Stable Sort Implementation**: Engineered a multi-tier sorting algorithm (Frequency -> Alphabetical) to eliminate "flicker" and element swapping in the Top 10 panel during high-speed ingestion.

#### Phase 2: High-Resolution TUI Dynamics
* **Advanced 2x2 Stats Grid**: Optimized the Stats Breakdown panel with a fixed-width grid layout, improving scannability for total log counts and severity distribution.
* **Intelligent Truncation Logic**: Implemented a custom text-trimming engine to ensure long log messages do not break the 2-column UI layout, maintaining perfect alignment across varying terminal widths.
* **Context-Aware Color Mapping**: Developed a dynamic switch-case color engine that maps internal log levels to TUI color tags, providing immediate visual differentiation between Warnings and Errors.

#### Phase 3: Temporal Velocity Analysis (Sparkline)
* **Real-Time Throughput Engine**: Integrated a per-second "Heartbeat" tracker using background goroutines and tickers to capture log velocity without blocking the main parser.
* **Multi-Line Sparkline Generator**: Engineered a high-resolution, multi-line vertical graph using Unicode block elements (`█`, `▄`). This provides a 60-second historical window of system activity.
* **Dynamic Peak & Status Monitoring**: Added real-time "Peak EPS" tracking and automated status labeling (IDLE/NORMAL/HIGH LOAD), transforming the graph from a simple line into an actionable diagnostic tool.

</details>

<details>
<summary><b>March 10, 2026: TUI Evolution & High-Frequency I/O Synchronization</b> (Click to expand)</summary>

#### Phase 1: Professional Terminal User Interface (TUI)
* **tview Framework Integration**: Migrated from raw ANSI escape sequences to a robust, cell-buffered TUI using `github.com/rivo/tview`. This resolved all terminal flickering and "double-image" artifacts during high-speed updates.
* **Component-Based Layout**: Engineered a dynamic Flex-box layout featuring a dedicated **Stats Panel** for real-time metrics and a **Log History Panel** with color-coded severity tags (`[red]`, `[yellow]`, etc.).
* **Thread-Safe UI Updates**: Implemented `dash.App.QueueUpdateDraw` to synchronize background log processing with the UI main loop, ensuring the dashboard remains responsive even at 1,000+ events per second.

#### Phase 2: High-Performance Data Ingestion 
* **Event-Loss Mitigation**: Refactored the `Tailer` component to utilize a "Greedy Read" strategy. The engine now drains the entire file buffer until `io.EOF` upon receiving a single watcher signal, preventing data stagnation when the OS drops filesystem events.
* **Buffered Watcher Channels**: Hardened the `fsnotify` implementation with buffered channels to prevent the file watcher from blocking during burst-write scenarios.
* **Resource Fairness & Throttling**: Introduced a strategic `time.Sleep` "breather" in the Aggregator logic. This prevents CPU starvation, allowing the Go scheduler to prioritize UI rendering frames without sacrificing log parsing throughput.

#### Phase 3: Filesystem Write-Through Optimization
* **Manual Buffer Flushing**: Identified and resolved a critical "Live-Update" blackout caused by OS-level file buffering. Integrated `f.Sync()` into the generator logic to force immediate filesystem notifications.
* **Global Input Capture**: Added application-level keyboard listeners (Input Capture) to support graceful exits ('q' or 'Esc'), moving the project closer to a production-ready CLI tool.
* **Stress Test Validation**: Successfully validated the end-to-end pipeline at a **1ms (1,000 logs/sec)** frequency, maintaining a perfectly stable, real-time dashboard.

</details>

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