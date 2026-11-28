# Lane E conformance matrix — SPEC-009 round 2

- Target revision: branch `spec-lane-e-round2` (see commit footer)
- Environment: macOS, Go 1.2x, CGO_ENABLED=0, real filesystem and real processes throughout
- Specification revision: `docs/specs/09-round2-advanced-mechanisms.md` at `f3d214c`
- Verification framework: `docs/specs/00-verification-framework.md`

Lane E's scope per dispatch: sections D, E, F, G implemented; B implemented
as the `fileobs` contract package without subsystem wiring; A and C as
interface contracts and assessments only. Each entry states what was
verified and how; "implementation evidence" is navigation, not proof.

## Section D — Checksum and end-to-end integrity

| Req ID | Status | Interpretation / flow | Behavioral evidence | Gap |
| --- | --- | --- | --- | --- |
| REQ-CHK-010 | implemented | The install pipeline distinguishes: the published checksum (expectation), the downloaded archive's full-object digest (verified pre-commit), extraction validation (structural). `fileobs` keeps digest/ETag/version as separate attributes so callers cannot mix them (REQ-META-040's other half). | `verifyChecksum` compares full archive bytes against the release checksum file before any write; `TestVerifyChecksumFormats` covers both checksum file formats, foreign hashes, missing hashes | — |
| REQ-CHK-020 | implemented | Explicit checksum verification is unconditional in the install flow: no install path skips it | `TestInstallRipgrepRejectsAnotherChecksum`: corrupted mirror → install errors mentioning checksum, parent directory empty | — |
| REQ-CHK-030 | implemented (install) / missing (local history, Lane C wiring) | Pre-commit rejection must leave the old object unchanged. For installs this holds by construction plus tests. Local history snapshots carry no verifiable integrity today. | `TestInstallRipgrepChecksumMismatchCannotReachTheToolDirectory`: existing install byte-identical after the corrupted mirror episode; fresh install leaves nothing, not even the directory; `TestInstallRipgrepInstallsReleaseBinary` second-install refusal. Local history: see integration notes §D contract | Lane C wires snapshot digests per contract |
| REQ-CHK-040 | implemented | Verification completes before any byte is published: the installer verifies the whole archive in memory before staging; `fileobs.VerifyDigest` documents the streaming variant honestly (sink bytes pass through before the digest is known — callers that need publish-after-verify must stage) | install pipeline order in `installRipgrep`; `TestInstallRipgrepRejectsAnotherChecksum` proves nothing is published on mismatch | — |
| REQ-CHK-050 | implemented (documented non-application) | Range/partial reads (hex viewer, editor paging) never claim full-object verification, so no wrong application exists. Decision recorded explicitly in integration notes §D rather than left accidental | grep confirms no checksum machinery in the partial-read paths; decision documented | — |
| REQ-CHK-060 | implemented | No streaming-copy-into-final-location exists in the install path — extraction validates before staging, staging precedes rename, so a poisoned writer state cannot arise. `VerifyDigest`'s sink contract documents the abort-equivalent semantics for future streaming users | code inspection of `installRipgrep` + `internal/fileobs/digest.go` contract | — |
| REQ-CHK-070 | implemented | A checksum file that contains no usable hash is a failure, not a skip | `TestVerifyChecksumFormats` cases "empty" and "no hash" fail verification; install therefore aborts | — |

## Section E — Structured fuzzing

Infrastructure: `docs/specs/conformance/lane-e-fuzzing.md`; targets in
`internal/search/fuzz_test.go` and `internal/regexplayground/fuzz_test.go`.

| Req ID | Status | Behavioral evidence | Gap |
| --- | --- | --- | --- |
| REQ-FUZZ-010 | implemented | All nine targets decode raw bytes into near-valid shapes (matches pointing at real lines, newline path lists, structured capture triples) — documented per target in lane-e-fuzzing.md | — |
| REQ-FUZZ-020 | implemented | Every target clamps input sizes before work; budgets derive from real-caller magnitudes (table in lane-e-fuzzing.md) | — |
| REQ-FUZZ-030 | implemented | Budgets compose multiplicatively-bounded: worst case FuzzFiles is 512 paths × limit 500 with constant work per pair; no input parameter controls iteration count independently of clamped size | — |
| REQ-FUZZ-040 | implemented | Three real defects found in the first fuzzing minutes (preview slice-out-of-range, preview window invariant, unflip out-of-bounds hits); each became a committed regression input in `internal/search/testdata/fuzz/FuzzPreview/` and is pinned forever by `go test` | — |
| REQ-FUZZ-050 | implemented | No target touches network or processes; all are pure functions of input bytes | — |
| REQ-FUZZ-060 | implemented | Oracles assert caller-visible contracts (coordinates inside text, bounded output, no-op identity), never internal structure — see target table | — |

Fuzz run evidence (this revision): 20s per search target, 15s per
regexplayground target, all PASS, zero new failures after the three initial
defects were fixed:

```
FuzzPreview PASS 21.4s · FuzzUnflip PASS 21.2s · FuzzFiles PASS 20.3s
FuzzReplaceMatches PASS 20.3s · FuzzEvaluate PASS 16.5s
FuzzParseDiagram PASS 16.3s · FuzzCaptureHighlights PASS 15.3s
FuzzDetectJS PASS 15.3s · FuzzUnsupportedJSFeatures PASS 16.2s
```

Interface contracts for the four targets owned by other lanes (LSP frame
parser, path normalization, save format, hex paging, edit sequences) are
specified in lane-e-fuzzing.md and unwired by design — their packages are
mid-construction in parallel lanes.

## Section F — Threat model

Document: `SECURITY-THREAT-MODEL.md` (repository root).

| Req ID | Status | Behavioral evidence | Gap |
| --- | --- | --- | --- |
| REQ-SEC-010 | implemented | The document defines system model, trust boundaries, in-scope/conditional/out-of-scope; every claim cites the enforcing code or a behavioral test | — |
| REQ-SEC-020 | implemented | See holds no credentials of its own; trust state is per-workspace-path; configured server env travels only into that server's process. No cross-context token use exists to violate | — |
| REQ-SEC-030 | implemented (See's own speech) / pending wiring (server stderr) | `TestConfiguredServerSecretsStayOutOfTheLog`: a configured server whose env and command carry token-shaped values starts, runs, and exits — the tokens never appear in the log. Server stderr is untrusted content that may echo those secrets — cross-lane decision (integration notes §F item 1): it enters the log redacted through Lane A's streaming redactor, the single redaction layer after integration. Until integration it still passes verbatim; recorded as a defect, not a boundary | Lane A redactor wired as the single layer (§G checklist item 3) |
| REQ-SEC-040 | implemented (owned surface) / partial (LSP parser) | Fuzz evidence over the parsing/slicing code Lane E owns (above); three real defects found and fixed. The LSP frame parser — the highest-value target — is specified but unwired because Lane A owns it mid-construction | Wire LSP frame fuzz target at integration (contract ready) |
| REQ-SEC-050 | partial — workspace-binary denial provisional | Threat model documents: workspace binary execution rejected via symlink-resolved path containment (`TestConfiguredServerInsideWorkspaceNeverStarts`); local history `0700`/`0600` permissions; install staging uses same-directory temp + rename; trust gating of tasks/LSP verified by inspection of the gates. Independent verification refuted the denial's completeness: on case-insensitive volumes a case-variant path spelling evades the lexical comparison end-to-end (the verifier's D1). The fix mechanism exists in Lane A's branch (`diskSpelling`); the threat model marks the invariant provisional and the integration checklist (§G item 2) carries extending it to every executable resolver | disk-spelling comparison across `languages.FindConfigured` and `search.FindRipgrep` at integration |
| REQ-SEC-060 | implemented (documented conditional) | Built-in bounds enumerated in the threat model (scan caps, column caps, download caps, log ring, playground budgets); beyond-capacity exhaustion is documented conditional | — |
| REQ-SEC-070 | partial | The document itself mandates updating with new surfaces (its intro), and this delivery is its first instance. Enforcement is process, not mechanism — no CI gate exists | — |

## Section G — List/scan stream semantics

| Req ID | Status | Interpretation / flow | Behavioral evidence | Gap |
| --- | --- | --- | --- | --- |
| REQ-LIST-010 | partial | The scan is always recursive within the workspace root — there is no directory-mode listing API, so trailing-slash semantics do not exist to violate. This narrowing is now explicit in the `ListFiles` contract comment | contract documentation on `ListFiles` | If a directory-mode query is ever added, this REQ becomes live |
| REQ-LIST-020 | implemented | Stream errors terminate the scan and propagate as errors, never as partial success: scanner stderr/exit failure propagates; the walk fallback discards partial results on error (fixed this lane — it previously returned them) | `internal/search/process_test.go` scanner-failure cases; `walkFiles` fix with `nil` return on error; end-to-end: `TestQuickOpenRetriesAfterAScanError` | — |
| REQ-LIST-030 | implemented | Memory bounded by the cap (200k entries for quick open) plus one scanner line; cancellation through the workspace context stops the scan and is reported as the context error, never as truncation or an empty result | `TestQuickOpenScanCancellationLeavesNoPartialList`: a scanner producing infinite output is canceled mid-stream; no files retained, scan ends | — |
| REQ-LIST-040 | implemented (not applicable, fail-closed) | Filesystem scans carry no versioning; no version/deleted-marker query capability is advertised, so there is nothing to silently degrade. `fileobs` carries the Tri-state model for when a versioned backend appears | type system: no such parameter exists | — |
| REQ-LIST-050 | partial | The backend/client filtering split is now documented on `ListFiles`: gitignore filtering is ripgrep's (backend), `skipDir` is the walk's client-side filter and only applies there. Honest for what exists; the watcher-merge semantics (incremental vs full scan) belong to Lane B and are not yet contractual | contract documentation | Lane B watcher merge semantics |
| REQ-LIST-060 | implemented (documented) | Duplicates are contractual bugs, not input to dedupe: neither `rg --files` nor the walk can visit a file twice; ordering is explicitly unspecified; the contract comment says so, and no caller deduplicates (verified by grep) | `ListFiles` contract comment | — |

### The defect this section's work exposed and fixed

Driving the REQ-LIST-020/030 behavioral tests through the real quick-open
panel surfaced a hot-loop defect: a scan failure left `files` nil, the
completion invalidated the ranking, the re-rank called `ensureFiles`, and
the scan restarted — forever, once per frame. Any scanner failure mode
(permissions, resource exhaustion) would have burned a process per frame
indefinitely. The fix makes scan failure sticky (`q.err` blocks re-scan)
with two deliberate retry points: opening the panel (the user's natural
retry) and workspace invalidations. All invalidation sites updated;
`TestQuickOpenRetriesAfterAScanError` pins: frames pass with zero
additional scans while the error stands; reopening retries exactly once;
a healed scanner yields its files through the same path.

Independent verification found the second retry point incomplete for the
quick open panel (the verifier's D3): invalidation cleared the error and
the file list, but the panel's ranking was keyed on the query, which had
not changed — so nothing re-entered `ensureFiles` and the panel sat on
its error while the filesystem changed underneath it. (The tree filter
did retry: its rebuild path reaches `ensureFiles` unconditionally.) The
respin invalidates the open panel's ranking for file-consuming queries
at the same invalidation sites, so the re-rank reaches the retry.
`TestQuickOpenInvalidationRetriesScan` pins: an empty event batch retries
nothing, one invalidation retries exactly once, a healed scanner's files
arrive with the panel never closed and the query never retyped — and the
test fails without the ranking invalidation.

## Section B — FileObservation (contract package only, per dispatch)

| Req ID | Status | Evidence | Gap |
| --- | --- | --- | --- |
| REQ-META-010 | implemented (package) | `Observation` fields are private; construction/enrichment copy; `internal/fileobs/fileobs_test.go` proves enrichment never mutates the original | Wiring belongs to other lanes per contract |
| REQ-META-020 | implemented (package) | `Tri` three-state; `WithVersion` accepts `TriUnknown` and never fabricates false | — |
| REQ-META-030 | implemented (package) | ETag/version stored verbatim; digest byte-exact; comparison is byte equality | — |
| REQ-META-040 | implemented (package) | `Check` refuses cross-source comparison with `ErrIncomparableIdentity`; shared attributes must all agree; no-attribute pairs are incomparable, never matching. Since the respin `Check` is fail-closed on asymmetric strength: an attribute the expectation carries but the current observation does not measure is incomparable, never a silently dropped term of the condition — verified against the wiring trap the integration contract originally prescribed (same-length content replacement with restored mtime must fail loudly, and does) | — |
| REQ-META-050 | missing (deferred) | Copy/rename metadata preservation semantics are not yet defined anywhere; the contract notes (integration notes §B) frame the decision | Deliberate — decision belongs with the operations' owners |
| REQ-META-060 | implemented (package) | Presence bits separate absent from empty; tests cover empty-ETag vs no-ETag distinction | — |

## Section C — Conditional operations (interface only, per dispatch)

| Req ID | Status | Evidence | Gap |
| --- | --- | --- | --- |
| REQ-COND-030 | implemented (primitive) | `ErrConditionMismatch` is its own error category, distinct from not-found/permission, tested | Save-path wiring per contract §C |
| REQ-COND-010/020/040/050/060/070/080 | partial (contract written) | Integration notes §C specify target, evaluation point (commit-time), honest degradation (rename-window race disclosed per 070), batch independence | Implementation belongs to Lane C's atomic-write pipeline; contract is ready |
| existing behavior note | — | `saveDoc` today implements a content-equality condition with a check-then-write race window; `search/replace.go` implements staleness-checked replacement (per-line SHA-256 verified before write, `ErrStaleMatches`) — both documented in the notes as the migration baseline | — |

## Section A — Three-stage API (assessment only, per dispatch)

No REQs marked implemented — deliberately not built. Assessment in
integration notes §A: `search.Options` already demonstrates the
single-validation-boundary shape; the spec's own P2 guidance ("不必先抽象")
applies; conversion trigger is the appearance of a second caller with
divergent needs, most plausibly git operations after Lane A's pipeline
settles.

## Coverage summary

- Section D MUST requirements: 7/7 implemented for the install surface; the
  local-history half of CHK-030 is a Lane C wiring item with a complete
  contract.
- Section E MUST requirements: 6/6 implemented with run evidence.
- Section F MUST requirements: SEC-010/020 implemented; SEC-030
  implemented for See's own speech with the server-stderr half pending
  the integration wiring of Lane A's redactor; SEC-040 partial pending
  the LSP fuzz wiring at integration; SEC-050 partial — the
  workspace-binary invariant is provisional until disk-spelling
  comparison covers every resolver at integration; SEC-060
  documented-conditional; SEC-070 partial-process. Each gap is marked at
  the claim in the threat model rather than only here.
- Section G MUST requirements: 4/6 implemented, 2 partial (LIST-010 and
  LIST-050 — both narrowed-by-design with the narrowing documented).
- Section B: contract package implemented and tested; subsystem wiring is
  the integration phase's work, specified per subsystem.
- Not acceptable as evidence: none — every "implemented" above cites a
  behavioral test through a real entry point (real scanner processes, real
  install servers, real app harness) or a fuzz run, not unit-level
  plumbing.

## Deviations requiring reproduction steps

- **Workspace-binary denial is evadable by case-variant path spellings on
  case-insensitive volumes** (independent verification's D1): configure a
  server whose command path is a case-rotated spelling of a binary inside
  a mixed-case workspace directory on default macOS APFS; the lexical
  rejection does not fire where the exact spelling is rejected.
  Reproduction exists as `TestVerifyCaseVariantWorkspaceBinaryResolution`
  on the verification branch. Deliberately not fixed in this lane: the
  mechanism (`diskSpelling`) is Lane A's, shipped on Lane A's branch, and
  re-implementing it here would collide at merge. The threat model marks
  the invariant provisional and integration notes §G item 2 carries the
  coverage check.
- **Server stderr enters the log unredacted** (cross-lane contradiction
  C1, resolved in favor of Lane A's position): run a configured server
  that echoes its environment to stderr; token values from
  `LSPConfig.Env` reach the log panel verbatim on this branch. Wiring
  Lane A's redactor is integration work; the decision and the single-layer
  rule are recorded in integration notes §F item 1.

Everything else in Lane E's scope stands verified: the three fuzz-found
defects, the scan hot-loop defect, and the quick-open retry point are
fixed in this lane with regression evidence committed. Remaining open
items are integration-phase wiring with contracts written, tracked in
lane-e-integration-notes.md.
