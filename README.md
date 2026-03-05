# LogSum: High-Performance Log Streamer

## Architecture
[File] -> (fsnotify) -> [Tailer] -> (chan RawLog) -> [Parser] -> (chan LogEvent) -> [Aggregator] -> [UI]

## Core Guarantees
1. **Zero Leaks**: Every goroutine is tracked with a `context.Context`.
2. **Memory Bounded**: All channels have a buffer limit of 100. If the UI freezes, the whole pipeline pauses rather than exploding RAM.
3. **Graceful Exit**: On `Ctrl+C`, the program flushes the remaining logs and closes files before exiting.

## Backpressure Strategy
We use **blocking sends** on bounded channels. If the Parser is slow, the Tailer waits. This ensures we never process more than the CPU can handle.