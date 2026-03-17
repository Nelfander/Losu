# 🐺 LOSU (Log Observer & Summary Unit)

**LOSU** is a high-performance, AI-native observability tool designed to turn chaotic log streams into actionable engineering insights. It doesn't just tail logs; it understands the "why" behind the "what."

---

## 🧠 The Intelligence Layer
Unlike standard tailing tools, LOSU uses a **three-tier analysis engine**:

1.  **Pattern Fingerprinting**: Dynamically groups millions of unique log lines (e.g., `db_1`, `db_2`) into logical "Winner" patterns using fuzzy grouping logic.
2.  **Visual Delta**: A real-time 60-second "Sparkline" graph that tracks Error-Per-Second (EPS) spikes to detect anomalies instantly.
3.  **AI Observer (Ollama/Llama 3)**: A background "SRE" entity that analyzes top patterns and provides human-readable root-cause analysis and suggested mitigation steps.

## 🚀 Key Features
* **AI-Driven Root Cause**: Local LLM integration (Llama 3/Phi-3) for private, zero-cost, and offline log interpretation.
* **Multi-Channel Alerting**:
    * **Desktop**: Native OS notifications via `beeep`.
    * **Mobile**: Instant push notifications to your phone via `ntfy.sh` (zero-account setup required).
    * **Smart Rate Limiting**: Intelligent 20-second cooldown per error pattern to prevent "Alert Fatigue."
* **High-Concurrency Pipeline**: 
    * **Non-Blocking UI**: Dedicated goroutines for Data Processing, UI Rendering, and AI Analysis.
    * **Memory Bounded**: Fixed-buffer channels and backpressure strategies prevent RAM spikes during massive log storms.
* **Developer Dashboard**: ANSI-powered TUI featuring real-time stats, error distributions, and a dedicated "AI Wisdom" panel.

## 🛠 Tech Stack
* **Language**: Go (Golang)
* **TUI Framework**: `tview` & `tcell`
* **AI Engine**: Ollama (Local API)
* **Alerting**: `beeep` (Desktop) & `ntfy` (Mobile/HTTP)
* **Concurrency**: Context-aware Worker Pools, Mutex-protected Snapshots, and Atomic Counters.

## 📦 Installation & Setup

### 1. Prerequisites
Install [Ollama](https://ollama.com) and pull the high-performance Llama 3 model:
```bash
ollama pull llama3
```

## Run the app 
# Clone the repository
git clone [https://github.com/nelfander/losu.git](https://github.com/nelfander/losu.git)
cd losu

# Run with default INFO filter
go run main.go

# Run and wipe previous session stats
go run main.go -reset

### Mobile Alerts Setup
1. Download the **ntfy** app (iOS/Android).
2. Click **"Subscribe to topic"** and enter a unique, private name (e.g., `losu-monitor-7722`).
3. In `main.go`, ensure the `NtfyTopic` matches your chosen name:
   ```go
   notifier.NtfyTopic = "losu-monitor-7722"
4. Instant push notifications will now bypass your desktop and hit your pocket for all ERROR level events.

##🧪 Testing with Chaos
LOSU includes a built-in Chaos Generator to simulate production-grade failures, including high-memory spikes, database timeouts, and security anomalies:

# In a separate terminal (Change time.Sleep depending on how chaotic you want it! It can handle 1k logs/sec)
go run internal/generator/generator.go

## 🛠 <b>Development History</b>
<details><summary>(Click to expand)</summary>

<details>
<summary><b>March 17, 2026: AI-Native Observability & Distributed Alerting Systems</b> (Click to expand)</summary>

#### Phase 1: Cognitive Analysis Layer (Ollama Integration)
* **Local LLM Orchestration**: Integrated a dedicated `explainer` package to interface with the Ollama API, utilizing the Llama 3 (4.7 GB) model for zero-latency, private log interpretation.
* **Contextual Prompt Engineering**: Developed a specialized "SRE Role" prompt that forces the AI to output structured "Situation Reports" focused on Root Cause and Actionable Mitigation.
* **Short-Term Memory (Sliding Window)**: Engineered a "Destructive Read" buffer (`RecentMessages`) in the Aggregator. This ensures the AI analyzes only the "Delta" (the last 30 seconds of activity) rather than repeating historical session data.

#### Phase 2: Distributed Notification Architecture
* **Cross-Platform Alerting**: Implemented a multi-channel notification engine using `beeep` for local desktop OS alerts and `ntfy.sh` for remote mobile push notifications.
* **Intelligent Rate Limiting**: Developed a per-pattern "Cooldown" mechanism (20-second limiter) using a Mutex-protected map of timestamps. This prevents alert fatigue and desktop "spam" during high-frequency error spikes.
* **Thread-Safe Snapshotting**: Optimized the handoff between the high-speed log ingestion and the slower AI analysis loop using dedicated `Snapshot` clones to prevent memory racing.

#### Phase 3: UI/UX Structural Optimization
* **Flexible Layout Refinement**: Re-engineered the TUI Flex layout to prioritize AI insights, expanding the `AIView` real estate while maintaining a focused 15-line "Rolling History" for real-time logs.
* **Asynchronous State Updates**: Leveraged `App.QueueUpdateDraw` to ensure the AI "Thinking" states and final reports render smoothly without locking the main terminal UI thread.
* **Real-Time Insight Synchronization**: Aligned the AI analysis window with the Sparkline peaks, providing a direct visual link between throughput spikes and the AI's "Situation Report."

</details>

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

## 📜 License
This project is licensed under the MIT License. Feel free to use, modify, and distribute it in your own projects or as a base for your own observability tools!

---

## 🏗 Development Roadmap
- [x] **Phase 1**: High-Concurrency Pipeline & FS-Watcher
- [x] **Phase 2**: Pattern Recognition & Fuzzy Message Grouping
- [x] **Phase 3**: AI Observer Integration (Ollama/Llama 3)
- [x] **Phase 4**: Multi-Channel Alerting (Desktop & Mobile)
- [ ] **Phase 5**: Support for JSON-structured logs & Custom Regex
- [ ] **Phase 6**: Prometheus Metrics Export & Grafana Integration
- [ ] **Phase 7**: Historical Log Searching & Persistence