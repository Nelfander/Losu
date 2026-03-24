# 🐺 LOSU (Log Observer & Summary Unit)

**LOSU** is a high-performance, zero-dependency log tailing + intelligent analysis tool built in Go.  
It turns noisy log streams into actionable SRE intelligence — delivered straight to your terminal, desktop, **and pocket**.

Whether you're debugging at 2 a.m. or sipping coffee, LOSU gives you real-time visibility, smart pattern grouping, AI-powered root-cause suggestions (optional), and mobile-first heartbeat reports — **all offline-capable and private**.

## ✨ Highlights

- Single static Go binary — **no runtime dependencies**
- Real-time TUI dashboard with sparkline EPS graphs
- Intelligent log pattern fingerprinting & clustering
- Optional **local AI root-cause analysis** (Ollama + Llama 3 / Phi-3)
- Mobile heartbeat summaries via **ntfy.sh**
- Desktop notifications via **beeep**
- Smart alert rate-limiting to prevent fatigue
- Memory-safe, high-concurrency pipeline

## 🚀 Key Features

### Core Monitoring
- **Dynamic pattern grouping** — turns millions of near-identical log lines into clean, readable clusters
- Real-time **60-second sparkline** showing Errors-Per-Second (EPS) anomalies
- **Error-first prioritization** — most frequent ERROR always surfaces as "Top Issue"
- Zero-data / low-activity awareness with clear delta counters (Errors, Warnings, Info)

### Executive Heartbeat (SRE Reporting)
LOSU doesn't just tail — it **summarizes system health** and pushes concise status reports to your phone.

- Configurable reporting window (`LOSU_REPORT_WINDOW`)
- **Error-first "Top Issue"** promotion
- Counts + delta over the window
- Beautiful, emoji-enhanced mobile notifications via **ntfy.sh**

### 🧠 Optional AI Layer ("Pluggable Brain")
If a local Ollama instance is available:

- Dedicated "SRE Take" section in heartbeat & TUI
- Root-cause hypothesis + concrete mitigation suggestions  
  (e.g. "Redis connection pool exhaustion — flush + increase max connections")
- Fully local, private, zero-cost, offline

When AI is unavailable → falls back gracefully to statistical summaries.

### Alerting
- **Desktop** — native OS notifications (`beeep`)
- **Mobile** — instant push via `ntfy.sh` (no account needed)
- **Smart cooldown** — 20-second per-pattern rate limit

## 🛠 Tech Stack

- **Language**: Go (Golang)
- **TUI**: `tview` + `tcell`
- **AI**: Ollama (local HTTP API)
- **Notifications**: `beeep` (desktop), `ntfy` (mobile/HTTP)
- **Concurrency**: context-aware worker pools, atomic counters, mutex snapshots, bounded channels

## ⚙️ How It Works

Losu operates as a high-throughput pipeline designed to bridge the gap between "noisy" raw logs and "actionable" AI insights.

1. **Structured Ingestion**: The `Tailer` package utilizes OS-level signals to follow log files, passing data to a `Regex Parser` that extracts Timestamps, Levels, and Messages.
2. **State Aggregation**: The `Aggregator` maintains a thread-safe global state, calculating Errors Per Second (EPS) and clustering similar log patterns via cryptographic-style fingerprinting.
3. **Asynchronous Analysis**: A dedicated `Observer` routine periodically snapshots the aggregator state and prompts a local **Ollama** instance to perform root-cause analysis without blocking the UI.
4. **Reactive TUI**: Built with `tview`, the interface provides a real-time dashboard with interactive search filtering, mouse support, and a dynamic sparkline graph for throughput visualization.

## 📦 Installation, Setup and Testing!
<details><summary><b>The Docker Way!</b>(Click to expand)</summary>

Follow these steps to get the monitor, the AI, and the log generator running in sync.

### 1. Clone the repository
git clone [https://github.com/nelfander/losu.git](https://github.com/nelfander/losu.git)
& cd losu

### 2. Spin up the Infrastructure
This starts the UI container and the AI engine in the background.
```bash
docker-compose up -d
```

### 3. Prepare the AI (One-Time Setup)
Run this to download the Llama3 model into your local Docker volume. You only need to do this once:
```bash
docker-compose exec ollama ollama run llama3
```

### 4. Launch the Monitor (UI)
To enter the interactive dashboard (with search and scroll support), run:
```bash
docker exec -it losu-losu-1 ./losu
```

### 5. Start the Log Generator
In a new <b>terminal window</b>, start the stream of simulated logs:
```bash
# For Stress test (1k logs/sec)
docker-compose exec -d losu ./stress_gen ./logs/test.log
```
```bash
# For Normal test (Warn: 10% chance | Error: 3% chance)
docker-compose exec -d losu ./normal_gen ./logs/test.log
```


### 🛠️ Useful Commands

| Action | Command |
| :--- | :--- |
| **Stop Logs** | `docker-compose exec losu pkill stress_gen` |
| **View Raw Logs** | `tail -f ./logs/test.log` |
| **Shutdown All** | `docker-compose down` |
| **Reset Stats** | `docker exec -it losu-losu-1 ./losu -reset` |


</details>

---

<details><summary><b>The GO Way!</b>(Click to expand)</summary>

### 1. Clone the repository
git clone [https://github.com/nelfander/losu.git](https://github.com/nelfander/losu.git)
& cd losu

### 2. Prerequisites
Install [Ollama](https://ollama.com) and pull the high-performance Llama 3 model:
```bash
ollama pull llama3
```

### 3. Configuration
Create a `.env` file in the root directory(Check .env.example):
| Environment Variable | Description | Default |
| :--- | :--- | :--- |
| `LOSU_LOG_PATH` | Path to the log file to monitor. | `test.log` |
| `LOSU_MIN_LEVEL` | Minimum severity (DEBUG/INFO/WARN/ERROR). | `INFO` |
| `LOSU_NTFY_TOPIC` | Unique ntfy.sh topic for phone alerts. | `losu-monitor-default` |
| `LOSU_AI_MODEL` | The Ollama model for analysis. | `llama3` |

### 4. Mobile Alerts Setup
1. Download the **ntfy** app (iOS/Android).
2. Click **"Subscribe to topic"** and enter a unique, private name (e.g., `losu-monitor-5437`).
3. In `.env`, ensure the `LOSU_NTFY_TOPIC` matches your chosen name:
   ```go
   NTFY_TOPIC=losu-monitor-5437
4. Instant push notifications will now bypass your desktop and hit your pocket for all ERROR level events.


### 5. ▹Run the app 
<details><summary><b>Normal GO way!</b>(Click to expand)</summary>
-Run with default INFO filter

```bash
go run cmd/logsum/main.go
```

-Run and wipe previous session stats

```bash
go run cmd/logsum/main.go -reset
```

### 6a. 🧪 Testing with Chaos. (Populates test.log!)
-LOSU includes a built-in Chaos Generator to simulate production-grade failures, including high-memory spikes, database timeouts, and security anomalies:

-In a separate terminal (Change time.Sleep depending on how chaotic you want it! It can handle 1k logs/sec). 
```bash
go run bin/stress/stress_gen.go
```

### 6b. 🧪 Testing without Chaos. (Populates test.log!)
-In a separate terminal
```bash
go run bin/normal/normal_gen.go
```
</details>

<details><summary><b>Makefile way!</b>(Click to expand)</summary>
You can use the provided **Makefile** for easy execution:

```bash
# Run the monitor
make run
```

### 6. 🧪 Testing 
-<b>Normal steady traffic</b>
```bash
# Run the steady test
make test-normal
```
-<b>High-velocity "Chaos" mode</b>
```bash
# Run the stress test
make test-stress
```
</details>

</details>

---

## 🛠 <b>Development History</b>
<details><summary>(Click to expand)</summary>

<details>
<summary><b>March 24, 2026: High-Performance UI Virtualization & Terminal Sync</b> (Click to expand)</summary>

#### Phase 1: High-Frequency Viewport Architecture
* **Virtual Buffer Implementation**: Engineered a "Hard Trim" logic for the primary log viewport, capping visible lines at 1,500 while maintaining a 50,000-line RAM cache. This prevents terminal memory exhaustion and ensures $O(1)$ rendering complexity regardless of total log volume.
* **Atomic Buffer Reset**: Implemented a `LogView.Clear()` strategy during high-volume bursts. By flushing the GPU text cache and `tcell` internal state during log spikes, the system eliminates the "Symbol Wall" artifacting caused by ANSI escape sequence fragmentation.
* **Fprint Stream Optimization**: Shifted from heavy `SetText` string-joining to direct `fmt.Fprint` streaming. This allows the UI to append new data to the terminal's sub-buffer without triggering a full layout re-calculation of the existing 350k+ lines.

#### Phase 2: Input-Sync & Resource De-escalation
* **$O(1)$ Scroll Geometry**: Refactored the `SetMouseCapture` logic to eliminate expensive `strings.Count` operations on multi-megabyte buffers. By utilizing the `FilteredLogs` slice length for coordinate mapping, mouse-driven scrolling now incurs near-zero CPU overhead during 400k EPS spikes.
* **Priority-Based Scheduling**: Implemented an "Engine-First" rendering priority. During extreme data surges, the UI intelligently introduces micro-latency (throttling) to protect the integrity of the terminal's IO pipe, ensuring the background Processor never loses its place in the log stream.
* **State Persistence Logic**: Enhanced the `FilteredLogs` cache to support a sliding-window architecture. This ensures that even when the UI "trims" the screen for performance, the internal search-indexed data remains accurate for statistical reporting.

#### Phase 3: Stability Hardening & UI Polish
* **Mouse Tracking Synchronization**: Resolved a critical "Desync" bug where terminal mouse-reporting codes were interpreted as raw text. Hardened the event-loop to prioritize TUI control sequences over raw log data during high-throughput windows.
* **Dynamic Capacity Scaling**: Integrated a 5,000-line internal `tview` buffer limit alongside a 50,000-line `FilteredLogs` slice. This multi-tiered memory approach provides a "smooth-scroll" experience for the user while keeping the "hot" rendering path lean.
* **Search Filter Optimization**: Optimized the `Update` loop to only process "Delta" logs (new arrivals) rather than re-filtering the entire history, drastically reducing the per-frame computational tax.

</details>

<details>
<summary><b>March 23, 2026: Proactive SRE Heartbeat & Priority Analytics</b> (Click to expand)</summary>

#### Phase 1: Temporal Heartbeat Architecture
* **Dynamic Reporting Engine**: Engineered a secondary time-windowed aggregator that tracks system health over configurable intervals (e.g., 60m). Implemented via `time.Ticker` in a dedicated background goroutine, it ensures periodic status updates are dispatched regardless of real-time UI activity.
* **Environment-Driven Scheduling**: Integrated `LOSU_REPORT_WINDOW` into the `.env` configuration with robust `strconv` parsing and safety defaults. This allows users to scale the reporting "pulse" from high-frequency 1-minute bursts to 24-hour executive summaries.
* **AI-Optional Failover**: Architected the reporting logic to be "AI-First, but not AI-Dependent." If the Ollama bridge is offline, the system gracefully falls back to a high-fidelity raw data summary, ensuring 100% uptime for critical notifications.

#### Phase 2: Priority-Weighted Data Aggregation
* **Error-First Intelligence**: Refactored the `FlushHourlyStats` logic to implement a "Criticality Search." The system now prioritizes **ERROR** patterns as the "Top Issue" for the hour, ensuring that high-volume warnings do not mask low-frequency but catastrophic failure states.
* **Aggregator Struct Evolution**: Enhanced the internal `TopMessages` map to store complex anonymous structs `{Count int; Level string}`. This move from simple frequency tracking to level-aware tracking provides the AI and the user with verified severity context for every summarized message.
* **Stat Preservation**: Integrated the new hourly counters into the `Update` loop, maintaining separate buckets for real-time sparkline telemetry and windowed reporting to prevent data contamination between the two systems.

#### Phase 3: Executive AI Reporting & Mobile Integration
* **Multi-Modal Prompt Engineering**: Developed the `AnalyzeHeartbeat` method within the AI Explainer. This utilizes a structured "[REPORT FORMAT]" prompt that forces Llama3 to provide a consistent 3-point brief (Status, Analysis, Action), eliminating "lazy" or inconsistent LLM responses.
* **Mobile Notification Hardening**: Expanded the `alerts.Alerter` package with a generic `PushNotification` method. This enables the transmission of custom-formatted markdown reports and summaries to **ntfy.sh**, complete with priority tags and status emojis for instant mobile observability.
* **Resource Conflict Resolution**: Optimized the HTTP client timeouts (20s) for heartbeat analysis to prevent "Cold Start" LLM latency from blocking the primary telemetry workers or the UI thread.

</details>

<details>
<summary><b>March 22, 2026: Containerized Orchestration & Persistent AI Bridge</b> (Click to expand)</summary>

#### Phase 1: Dockerized Environment Architecture
* **Multi-Stage Build Optimization**: Engineered a high-efficiency `Dockerfile` utilizing `golang:alpine` for compilation and a minimal `alpine:latest` final stage. This decoupled the build environment from the runtime, reducing the final image footprint while ensuring all binaries (`losu`, `stress_gen`) are pre-compiled for Linux.
* **Persistent Volume Mapping**: Implemented a bidirectional "Data Bridge" between the host `${LOG_PATH_HOST}` and the container `/app/logs`. This ensures that logs generated on the host (or by the internal generator) persist across container restarts and remain accessible for real-time analysis.
* **Service Orchestration**: Configured `docker-compose.yaml` to manage the lifecycle of both the `losu` observer and the `ollama` AI engine, utilizing `depends_on` to enforce a logical startup sequence.

#### Phase 2: Virtualized TTY & Terminal Fluidity
* **Interactive TTY Pass-through**: Resolved "Frozen UI" issues by configuring `tty: true` and `stdin_open: true` within the Docker stack. This allows the Go-based TUI to capture raw terminal escapes and mouse events, enabling full search and scroll functionality inside a containerized shell.
* **Xterm-256Color Integration**: Injected `TERM=xterm-256color` into the container environment, ensuring the `tview` styles, color-coded log levels (INFO/WARN/ERROR), and Unicode sparklines render with 100% fidelity.
* **Process Management**: Integrated `pkill` logic within the containerized workflow, allowing for the hot-swapping of log generators (`stress_gen`) without interrupting the primary UI telemetry loop.

#### Phase 3: AI Engine & Network Hardening
* **Internal DNS Resolution**: Refactored the `LOSU_OLLAMA_HOST` networking from `localhost` to a Docker-internal service alias (`http://ollama:11434`). This enables the Go backend to communicate with the Llama3 model across the virtual bridge without exposing unnecessary ports to the host.
* **Persistent Model Caching**: Configured a dedicated `ollama_data` volume to store the ~5GB Llama3 weights. This prevents costly re-downloads and ensures the "AI Brain" is available immediately upon subsequent stack launches.
* **Environment Variable Injection**: Centralized the configuration into a `.env` file, allowing for seamless path adjustments (`LOG_PATH_HOST`) without modifying the core source code or Docker infrastructure.

</details>

<details>
<summary><b>March 20, 2026: High-Velocity Telemetry & Delta-Cache UI Architecture</b> (Click to expand)</summary>

#### Phase 1: High-Performance Log Virtualization
* **Delta-Cache Log Engine**: Engineered a "Sliding Window" caching system in the `Dashboard` using `LastHistoryLen` and `FilteredLogs`. This decoupled the UI from the raw 50k+ log buffer, reducing $O(n)$ re-scans to $O(1)$ incremental updates, achieving 0-latency at 50,000 logs.
* **Virtual Scroll Integration**: Implemented a mathematical scroll-capture logic that calculates percentage-based jumps through the log cache. This allows for fluid, draggable navigation through massive datasets without string-counting overhead.
* **Smart Memory Capping**: Integrated an automated cache-trimming mechanism that caps the UI's internal `FilteredLogs` at 50,000 entries, preventing memory exhaustion during indefinite stress-testing.



#### Phase 2: Signal Intelligence & Alert Hardening
* **Global Alert Throttle**: Refactored the `Alerter` logic from "Message-Based" to "Pattern-Based" cooldowns. By utilizing a `GLOBAL_ERROR_COOLDOWN` key, we prevented network stack overflows and "NTFY" provider bans during high-frequency error spikes.
* **Network Stack Protection**: Decoupled the notification trigger from the primary processing loop using a thread-safe `sync.Mutex` and `time.Since` validation, ensuring only high-value alerts reach the mobile device.
* **Visual Telemetry Graphing**: Enhanced the `GraphView` with a scaled `getSparkline` generator. This translates raw "Errors Per Second" into 10-line high Unicode blocks, providing a 60-second visual "Pulse" of system health.



#### Phase 3: UI Fluidity & UX Refinement
* **Non-Blocking Search Concurrency**: Optimized the `SearchFilter` to trigger a background cache rebuild only on change. This allows the user to filter 50k logs in real-time without locking the main `tview` application thread.
* **Draggable Scrollbar Logic**: Developed a custom `SetMouseCapture` handler for the `LogView`, enabling a "True Scrollbar" experience where the user can click and drag the right-most edge of the terminal to navigate history.
* **Dynamic Status Labeling**: Implemented an EPS-aware `getStatusLabel` function that provides color-coded, blinking "Critical" states when throughput exceeds 20.0 errors per second.

</details>

<details>
<summary><b>March 18, 2026: Interactive UX Framework & State-Driven Focus Management</b> (Click to expand)</summary>

#### Phase 1: Interactive Input & Focus Engine
* **Thread-Safe Event Interception**: Implemented a global `SetInputCapture` layer using `tcell` to manage system-level shortcuts (Ctrl+C) while preventing input "swallowing" during high-frequency log updates.
* **Mouse-Driven Navigation**: Enabled `EnableMouse(true)` and reconfigured primitive selectability, allowing users to transition from "Passive Monitoring" to "Active Investigation" via direct UI clicks.
* **Low-Latency Search Filtering**: Engineered a real-time `SearchFilter` mechanism that dynamically masks the `LogView` buffer. This allows for instant "Grep-like" functionality directly within the TUI without interrupting background ingestion.

#### Phase 2: Observability Metadata & Heartbeat Tracking
* **Temporal State Tracking**: Augmented the `Aggregator` and `Snapshot` models to track `LastErrorTime` and `LastWarnTime`. This provides a high-fidelity "Heartbeat" for system health regardless of current throughput.
* **Dynamic Header Injection**: Developed a real-time metadata header in the `AIView` and `TopErrors` panels, providing instant visual confirmation of the last critical event timestamp using Go's `time.IsZero` validation.
* **Input Quality of Life (QoL)**: Integrated a "Clear-on-Escape" `SetDoneFunc` for the search panel, allowing for rapid reset of the visual state and filter parameters.

#### Phase 3: UI Stability & Deadlock Prevention
* **Concurrency Guardrails**: Resolved a "Spider-Man" deadlock condition by decoupling manual `App.Draw()` calls from input change events, leaning on the main ticker loop to handle screen paints safely.
* **Layout Hierarchy Refinement**: Restructured the `rightSide` Flex row to prioritize the search interface, ensuring the input field remains visible and accessible during high-load error spikes.
* **Type-Aware Filtering**: Optimized the search logic to target a composite string of `Level + Message`, enabling users to filter by severity (e.g., typing "Warn") or specific error keywords interchangeably.

</details>

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

---

## 🏗 Development Roadmap
- [x] **Phase 1**: High-Concurrency Pipeline & FS-Watcher
- [x] **Phase 2**: Pattern Recognition & Fuzzy Message Grouping
- [x] **Phase 3**: AI Observer Integration (Ollama/Llama 3)
- [x] **Phase 4**: Multi-Channel Alerting (Desktop & Mobile)
- [ ] **Phase 5**: Support for JSON-structured logs & Custom Regex
- [ ] **Phase 6**: Prometheus Metrics Export & Grafana Integration
- [ ] **Phase 7**: Historical Log Searching & Persistence

---

## Problems & How I Solved Them
<details>
<summary>(Click to expand)</summary>
### 🚧 Challenge: The "Symbol Wall" (Terminal Buffer Desync)
**Problem:** As log throughput exceeded 10,000 EPS (Events Per Second), the UI would periodically "crash" into a wall of raw ANSI escape codes and terminal coordinate symbols (e.g., `[555;72;20M`). This was caused by a race condition between the high-speed `io.Writer` and the Terminal's rendering engine. When the stdout pipe saturated, the terminal would drop "closing tags" for colors, causing it to interpret log data as raw control sequences.

**Solution: UI Virtualization & Atomic Buffer Management**
To reach stable performance at **400,000+ EPS**, the rendering logic was completely overhauled:
* **Decoupled History from Viewport:** Instead of the UI holding the entire log history, a "Virtual Window" of 1,500 lines is maintained.
* **Atomic Resets:** Implemented `LogView.Clear()` during high-volume spikes to flush the GPU text cache and reset the `tcell` internal state, preventing buffer fragmentation.
* **$O(1)$ Event Capture:** Migrated mouse-tracking logic from active string-scanning (`strings.Count`) to cached slice indexing. This eliminated the CPU-bound "read-back" lag that previously triggered terminal desync.
* **Throttled Delta Updates:** The UI now prioritizes the background processing engine. If the log firehose exceeds the terminal's refresh rate, the UI intelligently skips frames to maintain system stability without losing data in the underlying telemetry.
</details>
---
## Pipeline Flow Diagram
<details>
<summary>(Click to expand)</summary>


```text
+───────────────────────────────────────┐
                  │          External Log Source          │
                  │      (Application / System Logs)      │
                  └──────────────────┬────────────────────┘
                                     │
                        (fsnotify / poll events)
                                     ▼
                ┌───────────────────────────────────────────┐
                │             Watcher & Tailer              │
                │     (internal/watcher + internal/tailer)  │
                │ - Non-blocking file stream                │
                │ - Context-aware shutdown                  │
                └──────────────────┬────────────────────────┘
                                   │
                         (RawLog Channel: string)
                                   ▼
                ┌───────────────────────────────────────────┐
                │          Worker Pool / Parser             │
                │      (internal/pipeline + parser)         │
                │ - Concurrent Regex Extraction             │
                │ - Type Conversion (Timestamp/Level)        │
                └──────────────────┬────────────────────────┘
                                   │
                        (LogEvent Channel: struct)
                                   ▼
                ┌───────────────────────────────────────────┐
                │           State Aggregator                │
                │        (internal/aggregator)              │
                │ - Circular Buffer (50k limit)             │
                │ - Pattern Clustering & Metrics            │
                │ - EPS (Errors Per Second) Calculation     │
                └───────┬──────────┬──────────┬─────────────┘
                        │          │          │
         ┌──────────────┘          │          └──────────────┐
         ▼                         ▼                         ▼
+────────────────+      +──────────────────+      +──────────────────+
│ Alert Service  │      │ AI Observer      │      │  TUI Dashboard   │
│ (internal/alerts)     │ (internal/ai)    │      │  (internal/ui)   │
├────────────────┤      ├──────────────────┤      ├──────────────────┤
│- Global Mutex  │      │- Delta Snapshot  │      │- Delta Caching   │
│- 20s Cooldown  │      │- Ollama/API LLM  │      │- Virtual Scroll  │
│- ntfy.sh Phone │      │- Pattern Summary │      │- Mouse Capture   │
└────────────────┘      └──────────────────┘      └──────────────────┘
         │                         │                         │
         ▼                         ▼                         ▼
   [Mobile Alert]           [Heuristic Report]         [Real-time TUI]
```
</details>

---

## 📜 License
This project is licensed under the MIT License. Feel free to use, modify, and distribute it in your own projects or as a base for your own observability tools!
