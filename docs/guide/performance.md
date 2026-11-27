# Performance and I/O

OpenSync streams media by default and keeps a bounded audio working set in memory. Persistent analysis caching is an explicit choice. Long recordings are divided into fingerprint work blocks so CPU work can be distributed across cores.

Storage scheduling and CPU scheduling address different bottlenecks. Concurrent reads can reduce throughput on a mechanical disk, while separate drives can operate independently. SSDs may benefit from concurrency. The best policy depends on measured throughput, source format, and workload.

For performance investigations, record stage times, decoder starts, bytes read, memory use, storage type, cache state, candidate coverage, and final timing results. Optimizations should preserve the evidence and output expected by callers.

An experimental Windows shared-library FFmpeg audio backend is under development. It is build-tagged and opt-in. Its initial small-file parity and decoder timing results do not establish large-project speed or default-release readiness. The public alpha uses the normal executable-based FFmpeg path for compressed media operations.

See [Benchmarks](../BENCHMARKS.md) and [Native Go migration](../NATIVE-GO-MIGRATION.md).
