# Lane E fuzzing infrastructure

This document defines the fuzz harness pattern SPEC-009 §E requires, the
targets that exist today, and the interface contracts for targets owned by
other lanes that are not yet wired. REQ-FUZZ-010 through REQ-FUZZ-060 are
the requirements each target is held to.

## The harness pattern

Every fuzz target in this repository follows the same five rules. They are
not conventions; they are what makes a fuzz run evidence instead of noise.

**Inputs are structured toward near-valid shapes (REQ-FUZZ-010).** Purely
random bytes would be rejected at the first validation step and exercise
nothing. Each harness therefore decodes its raw input into shapes the real
producer can emit: `FuzzReplaceMatches` builds matches that genuinely point
at lines of the text, corrupting some line hashes so both accept and
reject paths stay live; `FuzzFiles` treats input as a newline-separated
path list. Random bytes still arrive — the mutator is free — but seeds and
the input decoding keep the walk inside the contract's neighborhood.

**Every input carries a budget (REQ-FUZZ-020, REQ-FUZZ-030).** Budgets are
chosen from what the real caller could ever produce, not from what fuzzing
finds convenient: a preview line is bounded by ripgrep's 4000-byte column
cap plus margin, a file list by what the ranking shards process. Budgets
clamp inputs *before* work begins, so two small parameters cannot multiply
into amplification — a 512-path list with a 500-limit query is the worst
case `FuzzFiles` can reach, and it is small.

**Assertions are behavioral, never mirrors of internals (REQ-FUZZ-060).**
Oracles assert what a caller can observe: coordinates stay inside the text
they index, output size stays inside its window, results never exceed
their limit, a no-op transformation changes nothing. They do not assert
how the function computed its answer.

**No network, ever (REQ-FUZZ-050).** Targets exercise parsers, index
mapping, and display math. Anything that needs a process or a socket
belongs to the behavioral test suite instead.

**Every crash becomes a permanent regression input (REQ-FUZZ-040).** Go's
fuzzer writes the failing input into `testdata/fuzz/<Target>/`; those
files are committed, so `go test` re-runs them forever as ordinary seed
corpus. Minimizing is optional — the inputs are already small — but
committing is not.

## Running

```
go test -fuzz=FuzzPreview -fuzztime=60s -run=^$ ./internal/search/
```

`-run=^$` skips unit tests; without it the seed corpus runs as unit tests
and then fuzzing begins. Any input that fails lands in
`internal/<pkg>/testdata/fuzz/<Target>/` — commit it before fixing the
bug, so the regression is pinned independently of the fix.

## Targets that exist today

| Target | Package | What it proves per input | Budgets |
| --- | --- | --- | --- |
| `FuzzPreview` | internal/search | preview coordinates stay inside the preview text; valid-UTF-8 input is never cut mid-character; output stays inside its window | line ≤ 8192 B |
| `FuzzUnflip` | internal/search | every hit index stays inside the path it highlights | path ≤ 4096 B |
| `FuzzFiles` | internal/search | ranking hits stay inside their paths; output count stays at or under the limit | ≤ 512 paths, limit ≤ 500 |
| `FuzzReplaceMatches` | internal/search | a no-op replacement changes nothing; output growth is explained by the matches; corrupted hashes are rejected, not applied | text ≤ 16 KiB, ≤ 64 matches, ≤ 512 ops bytes |
| `FuzzEvaluate` | internal/regexplayground | match/capture coordinates stay inside the sample; matches are ordered; capture values equal the sample bytes they span | pattern ≤ 1 KiB, sample ≤ 8 KiB |
| `FuzzParseDiagram` | internal/regexplayground | diagram stays within its node/depth/label caps | pattern ≤ 1 KiB |
| `FuzzCaptureHighlights` | internal/regexplayground | highlight spans are non-empty, non-overlapping, in order | ≤ 300 raw bytes, ≤ 100 captures |
| `FuzzDetectJS` | internal/regexplayground | a reported literal's span stays inside the source and contains its pattern | source ≤ 16 KiB |
| `FuzzUnsupportedJSFeatures` | internal/regexplayground | scanning is deterministic and never panics on adversarial backslash runs | pattern ≤ 4 KiB |

### Bugs the initial runs found

The first minutes of fuzzing paid for the infrastructure:

1. **`preview` sliced out of range** when ripgrep-reported coordinates
   exceeded the line or were inverted — out-of-contract input, but the
   function is downstream of an external process's output, so hardening
   was required. Regression seed committed.
2. **`preview` lost its window's invariant** on long lines: the windowing
   math could slide the match out of the window entirely, and
   rune-aligning the window start on non-UTF-8 bytes could move the start
   past the match, producing a negative lead. The windowing was rewritten
   around one invariant — the window must contain the match, alignment
   never crosses it. Regression seeds committed.
3. **`unflip` emitted out-of-bounds hit indexes** for out-of-contract
   input. These indexes are where the UI paints highlights, so invalid
   hits are now dropped. Regression seed committed.

All three fixes are in `internal/search/search.go` and `internal/search/files.go`.

## Interface contracts for targets owned by other lanes

These are not implemented here because their packages are owned by other
lanes and their shapes are still moving. Each entry is the contract a
future fuzz target must satisfy once the owning lane's code settles; the
integration phase wires them.

### LSP JSON-RPC frame parser — `internal/lsp` (owner: Lane A)

- **Input shape**: a byte stream of concatenated or fragmented frames —
  `Content-Length` headers with valid, near-valid, and garbage values,
  bodies shorter/longer than declared, bodies split across reads. Model
  reads as a stream (a `[]byte` fed in random chunk sizes), not a slice,
  because the parser's contract is about stream boundaries.
- **Budgets**: total input ≤ 64 KiB; chunk count ≤ 256; declared
  Content-Length values clamped to ≤ 1 MiB regardless of what the input
  claims — this is REQ-FUZZ-030's amplification test: a header claiming
  2 GB must not allocate 2 GB.
- **Invariants**: no panic; no read that blocks after input is exhausted
  (run the parser in a goroutine with a timeout and fail on deadlock);
  every parsed message's byte count never exceeds its declared length;
  memory stays proportional to input, not to declared lengths.
- **Acceptance tie-in**: SPEC-009's "malformed LSP frame 不 panic、不无限
  分配" behavioral acceptance is exactly this target plus a memory
  measurement.

### Path normalization / confined join — `internal/workspace` (owner: Lane B)

- **Input shape**: `(root, relative)` pairs where relative is built from
  segments drawn from a small alphabet plus `.`, `..`, separators, and
  unicode; occasionally absolute paths and symlink-ish segments.
- **Budgets**: ≤ 64 segments, segment ≤ 256 bytes.
- **Invariants**: the normalized result of a confined join never escapes
  root (`workspace.Within(root, result)` or an error — never a silent
  escape); normalization is idempotent (`norm(norm(x)) == norm(x)`); no
  panic on any byte sequence including invalid UTF-8 and NUL.
- **Acceptance tie-in**: "随机 path 输入不会逃逸 root".

### Save/recovery file format — `internal/localhistory` (owner: Lane C)

- **Input shape**: bytes shaped as the store's JSON records — valid
  records with mutated fields, truncated records, records with
  extra/absent keys, wrong types. Decode raw fuzz input into structured
  mutations rather than feeding pure noise at the JSON parser.
- **Budgets**: record ≤ 1 MiB, ≤ 32 snapshots.
- **Invariants**: no panic; a record that fails validation never produces
  a restorable snapshot; a valid round-trip (`write` then `read`) is
  byte-identical in content fields; path mismatch in the record is always
  rejected.
- **Note**: this target doubles as the integrity-checking evidence for
  REQ-CHK-030 once Lane C wires content digests (see
  lane-e-integration-notes.md §D). Today the store rejects structurally
  invalid JSON but cannot detect semantically-corrupted valid JSON.

### Hex paging / range calculations — `internal/hexview` (owner: Lane C)

- **Input shape**: `(fileSize, offset, length, windowSize)` tuples biased
  toward boundary values (0, ±1 around powers of two, maxint-adjacent
  values clamped into range).
- **Invariants**: computed ranges never overflow, never exceed the file,
  and a page request inside the file never errors; empty-file edges never
  panic.

### Document version/edit sequence — editor buffers (owner: Lane A)

- **Input shape**: a sequence of edit operations (insert/delete at
  positions decoded from fuzz bytes) applied to a buffer, interleaved
  with version reads.
- **Invariants**: positions after each edit stay consistent with the
  edit's semantics; undo of the exact sequence returns to the original
  text; no edit at a stale position corrupts the buffer silently (the
  contract either rejects or re-targets — but observably one of the two).
- **Note**: this is the edit-side analogue of REQ-COND semantics; wire it
  once Lane A's save pipeline settles, because the interesting oracles are
  about what "stale" means there.
