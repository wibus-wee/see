# Lane A independent verification: spec-lane-a-tools @ a2e6bba

Verifier: independent agent, not the implementer. Evaluated tree: `a2e6bba` plus the
verifier's own probe tests (listed below; no implementation code was modified). Host:
Darwin arm64, Go 1.27.4. Method: every matrix claim re-checked against the actual test
it cites, all implementer tests re-run, mutation claims re-produced by re-mutating the
implementation, and adversarial scenarios the implementer did not test constructed and
executed against real processes.

Verdict: **needs-fix** — the core lifecycle/capability machinery is solid and most matrix
rows verify, but three defects contradict the spec or the matrix's own claims, two of them
at the trust/redaction boundary the audit commit specifically claims to have closed. Fix
list at the end.

## What was independently re-confirmed

Test suites, all green on this machine:

```
CGO_ENABLED=0 go test -race -count=1 ./internal/proc ./internal/lsp ./internal/langserver ./internal/tasks   # 4/4 pass
CGO_ENABLED=0 go test -count=1 ./internal/app ./internal/languages ./internal/search                        # pass (app 225s)
CGO_ENABLED=0 go build ./... && go tool mygo vet . && go vet ./internal/...                                  # pass
CGO_ENABLED=0 go test -race -count=3 -run TestVerify ./internal/langserver ./internal/lsp ./internal/proc ./internal/languages  # verifier probes, 3 iterations, no flakes
```

Mutation-discriminating power, re-produced by mutating the implementation myself and
confirming the cited test fails (implementation restored after each run):

| Mutation applied | Test that must fail | Result |
|---|---|---|
| epoch guard `attempt != s.attempt` removed in `exited()` | `TestExitOfAnOldServerIsIgnored` | FAILS as claimed |
| capability gate `if !c.Supports(method)` removed | `TestCapabilityProcess` | FAILS as claimed |
| `started = false` forced after `cmd.Wait()` (post-start retry) | `TestPolicyProcess` | FAILS as claimed |
| workspace-executable denial neutralized in `ValidateExecutable` | `TestWorkspaceExecutableNeverStarts` | FAILS as claimed |
| `secretEnvValue` disabled (redaction off) | `TestStartCrashReportsRedactedStderr` | FAILS as claimed |
| byte cap dropped from outbox push | `TestBlockedWriterFailsSessionOnByteOverflow` | FAILS as claimed |

The matrix's mutation claims are accurate. (Note: my first reproduction of the no-retry
and launch-denial mutations initially produced a no-op patch and a compile error
respectively — evidence that this kind of mutation testing is easy to do wrong; the
matrix's claims themselves, however, reproduce when the mutation is done correctly.)

## Per-requirement verdicts

Format: verdict — my own evidence, beyond re-running the cited test.

### SPEC-001 capability negotiation (matrix: 6 implemented / 3 partial)

| REQ | Verdict | Independent evidence |
|---|---|---|
| REQ-CAP-010 | confirmed | `operations` map built once at initialize end, immutable per session; `TestCapabilityProcess` opens two concurrent sessions with different advertisements and snapshots stay disjoint. |
| REQ-CAP-020 | confirmed | No wrapper layers exist; the dispatch gate is the only dispatch path (`c.call`); re-grepped: every feature request method routes through it (documentSymbol, foldingRange, typeHierarchy were converted in this branch). |
| REQ-CAP-030 | confirmed | Operations map covers 32 methods including per-variant entries (`semanticTokens/full` vs `/range`, `completionItem/resolve`, `documentLink/resolve`, `prepareRename` separately from `rename`). `TestSemanticTokensPartialCapability` verified present and asserts no full request is sent on range-only peers. |
| REQ-CAP-040 | partial — agrees with matrix, seam noted | `c.call` gates everything, so no bypass exists. Entry-point pre-disable varies (see D7), but that is exactly what the matrix admits. No stronger refutation. |
| REQ-CAP-050 | confirmed | `TestRemoteFailureOnPipe` independently re-read: in-process unsupported = permanent/no-effect and zero frames on the wire; remote method-not-found = unsupported/permanent but uncertain finality (it did reach the server). The unsupported-vs-execution distinction is machine-checkable, not string matching. |
| REQ-CAP-060 | partial — agrees | `TestCodeActionUnsupportedStepPreservesFile` verified: unsupported command, unsupported resource op, and unverifiable version all leave the file byte-identical. Version checks use both protocol and buffer versions (`MatchesDocumentVersion`). |
| REQ-CAP-070 | partial — agrees | Position encodings other than utf-8/utf-16 are rejected at handshake; explicit `null` also rejected (`TestInvalidCapabilityHandshake` covers 6 malformed shapes). Misadvertisement is undetectable in principle; admitted. |
| REQ-CAP-080 | confirmed | Sorted effective operations logged at initialize; verified in `initialize()`. No diagnostic UI — spec says SHOULD, not MUST. |
| REQ-CAP-090 | confirmed | `dynamicRegistration: false` advertised everywhere; server's `client/registerCapability` falls through to method-not-found. Snapshots immutable per session; reconnect re-negotiates. |

### SPEC-002 operation policies (matrix: 7 implemented / 2 partial)

| REQ | Verdict | Independent evidence |
|---|---|---|
| REQ-POL-010 | confirmed | `WithCommand` replaces the factory while fixed argv come from `Kind`; `TestDeriveWhileProcessDispatchInFlight` re-read and re-run. |
| REQ-POL-020 | confirmed | Value receivers return copies. One seam worth knowing: `WithPolicy` creates a fresh `slots` channel, so two derived handles each get their own independent concurrency budget (never *shared* beyond intent — the safe direction). |
| REQ-POL-030 | partial — agrees | Order is documented in the `Handle` comment and fixed in code: admission → caller total ctx → retry loop → per-attempt timeout → hook → process. `TestAttemptAndTotalDeadline` distinguishes per-attempt from total budget (its zero-elapsed-time assertion depends on the injected clock, weaker than a real-time proof, but the structure is in code). |
| REQ-POL-040 | confirmed | `BeforeStart` hook + command factory are real operation/resource extension points exercised at the process boundary. |
| REQ-POL-050 | partial — agrees | `WithClock`/`WithCommand`/`WithPolicy` each preserve the others (struct copy); the matrix's honesty about not measuring clock behavior mid-process is accurate. |
| REQ-POL-060 | confirmed | Fixed argv; hooks can't rewrite argv (they only see ctx+attempt). Policies do not touch output bytes. |
| REQ-POL-070 | confirmed | Post-start failures are never replayed (mutation-verified). Cancel during retry wait terminates with cancelled/no-effect (`TestPolicyAdmissionAndCancellation` re-run). Pre-cancel returns before admission. One imperfection, no retry consequence: cancellation finality is Uncertain even when the request may never have been written — conservative, spec-compliant. |
| REQ-POL-080 | confirmed | Hook is an ordinary policy field, nil in production construction; count-controlled in tests. |
| REQ-POL-090 | confirmed | Negative values rejected (`TestRejectInvalidPolicy`); whole-policy replacement avoids scope ambiguity. Backoff 0 = immediate retry is a possible tight loop but bounded by Attempts — acceptable. |

### SPEC-004 structured errors (matrix: 8 implemented / 2 partial)

| REQ | Verdict | Independent evidence |
|---|---|---|
| REQ-ERR-010 | confirmed | kind/status are independent fields; `temporary→persistent` transition observed in `TestPolicyProcess`. |
| REQ-ERR-020 | partial — agrees | All 11 kinds exist in the enum. LSP wire codes map 4 of them; a child-process boundary exercises fewer. Honest partial. |
| REQ-ERR-030 | confirmed | Retry allowlist is exactly {unexpected, timeout, rate-limited} AND pre-start only — invalid-input/conflict/unsupported can never be replayed even when a hook misclassifies them as temporary. Verified by re-reading the `Handle.Run` loop condition. |
| REQ-ERR-040 | confirmed | `n >= Attempts` converts to persistent, terminal. |
| REQ-ERR-050 | confirmed with a display seam | Structured fields exist (operation/attempt/duration/finality). But see F4: `Error()` renders only `kind (status)`, and the UI fallback prints exactly that. The context is machine-readable yet invisible to users in the most important failure case. |
| REQ-ERR-060 | partial — matrix says implemented, see F1 | Exact env values and URL passwords are redacted (re-verified). But the redaction is exact-match-only **and window-limited**: a secret longer than the 4 KiB digest can never match and leaks wholesale. The matrix admits derived-form leaks but not this one. |
| REQ-ERR-070 | confirmed | `errors.Join(context.Canceled, safe)` keeps cancellation detectable through classification; cancelled requests never consume retry budget (cancellation checked before the retry decision). |
| REQ-ERR-080 | confirmed | `errors.Is(err, fs.ErrNotExist)` survives the wrap chain (`TestMissingProcessIsSafeToDisplay`); RPC errors keep their numeric code. |
| REQ-ERR-090 | partial — agrees | PCs captured only for unexpected kind. Trace quality unassessed — verified the matrix doesn't overclaim here. |
| REQ-ERR-100 | confirmed with a note | Probe `TestVerifySubprocessExitClassification`: a nonzero exit classifies as unexpected/temporary/uncertain. "Temporary" for a deterministic compile error is semantically wrong but conservative-legal per the spec, and harmless because post-start errors are never replayed. Worth a kind refinement someday; not a violation. |

### SPEC-005 external tool lifecycle (matrix: 9 implemented / 2 partial)

| REQ | Verdict | Independent evidence |
|---|---|---|
| REQ-EXT-010 | confirmed with a UI seam | State machine has all six states; transitions verified. But Degraded renders as "Ready" in the toolbar (the switch has no Degraded case) — see F6. |
| REQ-EXT-020 | partial — agrees, but the matrix understates the gap | Launch boundary classifies permission-denied correctly (audit test re-run). However the custom-server finder still conflates denial with absence — see F3, which the matrix's "trust denials now read as denials" claim does not cover. |
| REQ-EXT-030 | partial — matrix says implemented, I disagree | Workspace-executable denial verified including the outside-symlink-pointing-inside case. But the check is lexical-after-symlink-resolution, and `filepath.EvalSymlinks` on macOS does not normalize case: a case-variant path into a case-insensitive workspace evades it. Probe-confirmed — see F2. |
| REQ-EXT-040 | confirmed | Handshake completes before Ready; optional capability absence doesn't block Ready (`TestServerWithoutDefinitionIsReady`). |
| REQ-EXT-050 | confirmed | Replay verified at the process level: after crash, the new session receives the **latest** content, versions restart at 1 per session (per-session monotonicity is what the protocol requires). Obsolete publications dropped by session+version check. Verifier probe `TestVerifyCloseBeforeOverflowDoesNotReplay` confirms a doc closed before an overflow is not resurrected. |
| REQ-EXT-060 | confirmed | Verifier probes with a real escapee process: `TestVerifyPendingRequestTerminatesWithEscapeeHoldingPipes` — the leader dies, a setsid grandchild holds stdout open forever, and the pending hover still terminates (in well under the 6 s bound) because the owned read end is closed after the 500 ms drain. This is the strongest evidence in the lane. |
| REQ-EXT-070 | confirmed | Two restarts then durable Failed, verified at process level. Budget-exhaustion surfacing probe: after exhaustion `Get` returns immediately with a persistent classified error — but see F4 for what the user reads. Also F5: a manual Retry does not reset the crash counter. |
| REQ-EXT-080 | confirmed | `$/cancelRequest` is best-effort queued even when the pipe is stalled (`TestCancelWithStalledPipe` uses real OS pipes); cancellation never waits for the peer to drain. |
| REQ-EXT-090 | confirmed with a design note | Verifier probe `TestVerifyQueuePressureNoDeadlock`: 4 producers × 256 KiB documents against a blocked writer — session fails exactly as specified, blocked `Sync` callers terminate with `session ended`, `StopWait` completes. One real-design concern (not a spec violation): `maxServing` breach kills the whole connection where `maxPending` breach fails one request; uneven severity for the same class of overflow. |
| REQ-EXT-100 | partial — agrees | Verifier probes `TestVerifyShutdownEscalationTiming`/`TestVerifyShutdownUnresponsiveServer`: exit-ignoring and shutdown-ignoring servers are both killed at ~300 ms when the timeout is 300 ms, process exited, no hang. Windows gap admitted. |
| REQ-EXT-110 | partial — matrix says implemented, see F1 | Exact-secret redaction verified by mutation. Flood probe `TestVerifyStderrFloodDoesNotStallReaping` confirms bounded digest + bounded reaping. But oversize-secret leak unadmitted; also the digest is server-controlled text injected into UI-visible error strings with control characters intact — low severity in this UI (plain text, no terminal), noted. |

## Defects found by this verification

### F1 — Oversize secrets leak wholesale through the stderr digest (medium)

`proc.Tail` keeps the last 4 KiB and redacts exact env values afterwards. A secret longer
than the buffer can never appear whole inside the window, so `strings.ReplaceAll` never
matches it and up to 4 KiB of it is exposed in plaintext. PEM bundles, cert chains and
some cloud session tokens in env exceed 4 KiB.

Reproduction: `internal/proc/verify_adversarial_test.go` → `TestVerifyOversizeSecretLeaks`
(5 KB env secret written to stderr appears verbatim in the digest). The matrix admits
*derived-form* leaks (base64/case — also confirmed, see `TestVerifyRedactionDerivedFormsLeak`)
but not this one: a verbatim leak of the exact secret.

Mitigating factors: env secrets > 4 KiB are uncommon; exposure requires the server to
print them. Fix direction: streaming redaction before the window trim, or document the
bound. Also confirmed boundary-straddle is neutralized by the whole-lines policy
(`TestVerifyTailBoundaryStraddleNeutralized`) — the design is one fix away from sound.

### F2 — Case-variant paths evade the workspace-executable trust check on case-insensitive filesystems (medium)

`ValidateExecutable` compares `filepath.Rel` lexically after `EvalSymlinks`. On macOS
`EvalSymlinks` resolves `/tmp`→`/private/tmp` but does **not** normalize case (verified
directly: `EvalSymlinks("/tmp/x/mixedcaseroot/tool")` = `/private/tmp/x/mixedcaseroot/tool`
while the real directory is `MixedCaseRoot`; `os.SameFile` confirms they are the same
file). Result: `/ws/tool` is denied, `/WS/tool` — the same bytes on disk — executes.

Reproduction: `internal/proc/verify_trust_test.go` → `TestVerifyCaseVariantEvadesTrustCheck`.
Attack realism: a copied config/env path with wrong case, or a malicious workspace that
convinces the user to configure `tools/rg`-with-typo. This is precisely the class of bug
the trust boundary exists against, and REQ-EXT-030's "never implicitly execute" claim in
the matrix does not mention it. Fix direction: ancestor walk with `os.SameFile`, or
case-normalized comparison keyed off the volume's case sensitivity.

### F3 — Custom-server trust denial still reads as "not installed" (low-medium, contradicts the audit claim)

`languages.FindConfigured` maps a workspace-resident configured binary to
`"myserver is not installed"`. The a2e6bba commit message says "the settings and find
surfaces render trust-boundary denials as denials rather than raw error chains or
'not installed'" — true for rg in settings/find, but the configured-server path (added in
this same lane's scope) was missed. A user who points a custom server at a workspace
binary is told to install software they already have.

Reproduction: `internal/languages/verify_adversarial_test.go` →
`TestVerifyFindConfiguredConflatesDenial`. This is the clearest bloodline seam: the audit
patched the surfaces it looked at and the matrix row claims the class fixed, but one
producer of the same error class kept the old behavior.

### F4 — Budget-exhausted server surfaces as bare `unexpected (persistent)` (low-medium)

After the restart budget is spent, `failSession` reclassifies the exit error: kind stays
`unexpected`, status becomes `persistent`, and `Operation`/`cause` are not rendered by
`operr.Error.Error()` (by design — the cause may contain server text). Every UI surface
that falls back to `err.Error()` then shows literally `unexpected (persistent)`: no
server name, no restart-budget explanation, no hint that the Retry button exists. Verified
end-to-end: `TestVerifyBudgetExhaustionSurfacing` logs the exact string; the toolbar's
`shortErr` has no persistent case, the notice fallback prints the raw text. It is visible
(not silent death), but not actionable — REQ-ERR intent is "UI can give actionable hints".

Fix direction: give the persistent terminal failure a dedicated user-facing message in
`lspFailure`/`shortErr` (e.g. "gopls crashed repeatedly — retry"), which requires no
change to the redaction-safe `Error()` design.

### F5 — Manual Retry does not reset the crash counter (low)

`s.retries` is only reset by `stableUptime`. After durable failure, a user hitting Retry
gets one new session — and if that session crashes within five minutes, the server is
durably failed again with no further automatic restart, because the counter still reads
`maxRestarts`. Arguably conservative-by-design; arguably punishes the user for the pre-fix
crashes of a binary they just reinstalled. Not spec-mandated either way; flagged as a
behavior review item.

### F6 — Degraded state is sticky and invisible (low)

A single transient delivery error moves the server to `Degraded` with no path back to
`Ready` short of a restart, and the toolbar switch has no `Degraded` case — it renders as
"Ready". The state machine gains a state the user never sees. Either recover on the next
successful sync or surface it; currently it's write-only state.

### F7 — App entry points discover unsupported-on-use rather than pre-disabling (low, admitted)

The matrix admits this for CAP-040. Verified examples: completion/signature-help requests
fire and get locally rejected (no wire frame — the gate works), but the user gesture
exists and produces a note rather than a disabled menu item. Command execution checks
the command whitelist at execution time. Since the wire gate is load-bearing and
mutation-verified, this is UX inconsistency, not a correctness hole.

## Cross-lane interface check (Lane D dependency)

Lane D's report says: "进程/终端输出的截断展示归 Lane A 的 internal/tasks" — verified,
not dropped between lanes: `tasks.Output.Snapshot()` exposes `truncated`, `taskView`
renders the localized `tasksTruncated` line (en + zh-Hans both present), and verifier
probe `TestVerifyTaskTruncationIsVisible` renders the panel against overflowed output and
sees the notice. Caveat: this covers the Go build/test task panel only; there is no other
terminal in the app on this branch, so nothing is uncovered.

## Bloodline-seam assessment (1813559 vs a2e6bba)

The two-author structure is visible in exactly the places the audit touched:

1. Error classification is *mostly* one system (`operr` everywhere), but the audit added
   surface-level string patching (`shortErr`, settings/navigator special cases) instead of
   teaching the error type to render — which is why F4 exists and why F3 was missed in a
   producer the audit didn't touch. Two layers classify the same events.
2. Trust checking exists in three independent implementations: `search.FindRipgrep`
   (lexical only), the `languages` finders (lexical + one resolved variant), and
   `proc.ValidateExecutable` (the authoritative one). They currently agree on the tested
   paths; F2 shows the shared lexical weakness. Consolidation is the structural fix.
3. The a2e6bba additions themselves (stderr digest, byte cap, permission classification)
   are well-tested and their mutation claims all reproduce — the audit did good work; the
   gaps are at the boundary of what it examined, not inside it.

## Engineering hygiene

- `git diff specs-opendal-dispatch..HEAD` contains no unrelated production code. The
  deletion of `docs/specs/09-round2-advanced-mechanisms.md` in that diff is an artifact of
  the comparison base: Lane A branched from `bbccc32`, and SPEC-009 was added to
  `specs-opendal-dispatch` (`f3d214c`) afterwards — not deleted by the lane.
- Commit messages accurately describe their content; the a2e6bba claims were each verified
  above, with the F3 exception.
- Matrix's own caveats (`verify.sh` Python-fixture failure, Windows coverage, tsrs never
  run for real) verified as honestly stated; I did not re-verify Windows either.

## Verdict: needs-fix

Core machinery — session isolation, capability gating, ordered sync, bounded queues,
escalating shutdown, retry discipline — verifies cleanly under adversarial probing,
including against real process-group-escaping descendants. The required fixes are small
and localized:

**Must fix before merge:**

1. **F3**: custom-server denial must not read "not installed" — return the typed
   `proc.ErrUntrustedExecutable` from `FindConfigured` (or classify like `findRipgrep`).
   Direct contradiction of REQ-EXT-020 and of the a2e6bba commit's own claim.
2. **F1 or its documentation**: either implement streaming/redaction-before-trim so
   oversize secrets cannot leak verbatim, or write the 4 KiB limitation into the matrix
   next to the derived-form admission. The spec MUST-level redaction requirement currently
   has an unadmitted hole.

**Should fix (can merge without, but recommended in the same wave):**

3. **F2**: case-insensitive trust evasion — platform-dependent fix; at minimum add the
   `os.SameFile` ancestor check on darwin/windows.
4. **F4**: user-facing persistent-exhaustion message.
5. **F5/F6/F7**: behavior-review items, no evidence they're violations.

## Verifier-added tests (all committed on this branch, none modify implementation code)

| File | Tests |
|---|---|
| `internal/proc/verify_adversarial_test.go` | derived-form leak pinning, straddle neutralization, oversize leak, case-variant control, short-secret threshold |
| `internal/proc/verify_trust_test.go` | case-variant trust evasion |
| `internal/lsp/verify_adversarial_unix_test.go` | escapee survives group kill while shutdown completes; pending request terminates with escapee-held pipes; shutdown escalation timing (exit-ignoring, shutdown-ignoring); stderr flood reaping |
| `internal/langserver/verify_adversarial_test.go` | queue pressure no-deadlock; budget-exhaustion surfacing; close-before-overflow not replayed |
| `internal/languages/verify_adversarial_test.go` | FindConfigured denial misclassification |
| `internal/tasks/verify_adversarial_test.go` | subprocess exit classification pinning |
| `internal/app/verify_adversarial_test.go` | task truncation UI visibility |
