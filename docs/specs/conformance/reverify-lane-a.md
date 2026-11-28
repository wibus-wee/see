# Lane A re-verification after fixes: spec-lane-a-tools @ e86dda2

Re-verifier: independent agent, not the implementer and not the first verifier.
Evaluated tree: `e86dda2` (fixes `a2e6bba..e86dda2` on top of the tree the first
verifier judged needs-fix in `verify-lane-a.md`, cb4ac34). Host: Darwin arm64, Go
1.27.4. Method: narrow re-verification — every required fix re-proven with
independently constructed adversarial inputs, the load-bearing nature of each fix
reproduced by mutating the implementation myself (mutations restored after each
run), the regression suites re-run in full, and the matrix re-read for honesty.
Probe tests added by this round are committed alongside this report
(`internal/proc/reverify_redact_test.go`, `internal/proc/reverify_trust_test.go`,
`internal/langserver/reverify_didopen_test.go`); no implementation code was
modified, `git diff` of tracked implementation files is empty.

## Verdict: merge-ready

All three must-fix defects are confirmed fixed with adversarial evidence, all
three behavior-review items are resolved as the matrix claims, the regression
suites are green including race stability runs, and the matrix's remaining-limit
admissions were spot-checked as truthful. Three minor residuals are listed at the
end; none contradicts a spec MUST or a matrix claim, but two are documentation
gaps worth one matrix sentence each at integration time.

## Fix commits (a2e6bba..e86dda2)

| Commit | Content |
|---|---|
| 578004e | streaming redaction before the window trim; case-variant workspace executable denial |
| 7a7764d | crash budget reset on manual Retry; Degraded recovery |
| a22424b | workspace denials classified end to end; terminal failure explained |
| 387f7a1 | adopts the first verifier's regression probes — diffed against cb4ac34, test functions byte-identical, only provenance comments changed |
| c3b4da6 | matrix update — claims re-verified row by row below |
| e86dda2 | crash fixtures exit after the handshake — re-read; the added `readInitialize` genuinely makes the crash hit a live session rather than racing initialize |

## F1 — oversize secret leak: confirmed-fixed

Redaction now runs as bytes arrive (`Tail.redact`), before the window trim, with
a holdback of `len(longest secret) − 1` bytes so a secret split across pipe reads
cannot be emitted before it completes. Reviewed the holdback arithmetic: any
pending fragment of a not-yet-complete secret is raw-input bytes (never
post-redaction text, since replacement only shrinks), so its length is bounded by
`len(secret) − 1 ≤ holdback` — the flush-at-`String()` then either redacts the
completed secret or emits a strict prefix that never was one. The reasoning holds.

Independent adversarial probes (`reverify_redact_test.go`, all pass under `-race`):

- 5004-byte secret attacked at five positions: stream start, stream end without
  newline, mid-flood, cut landing mid-secret with the secret ending exactly at
  end of stream, straddling the 4 KiB cut. No 64-byte fragment anywhere.
- Split-write attacks beyond the implementer's sweep: 8-byte chunks, prime-sized
  97-byte chunks (aliasing-resistant), alternating 1/300-byte chunks, and an
  independent sweep of every split point inside a 44-byte secret.
- Mixed-length secrets: a 5 KiB env secret inflating the holdback does not starve
  ordinary output and does not protect or endanger a coexisting short secret.
- URL-password secret (`GOPROXY` userinfo) split in 3-byte writes.
- Secret arriving after a 200-line flood still redacts.

Mutation evidence, reproduced independently:

| Mutation | Expected failure | Result |
|---|---|---|
| genuine pre-fix `redact.go` checked out from a2e6bba (trim-then-redact) | oversize secret leaks verbatim | FAILS in both the implementer's `TestTailRedactsOversizeSecret` (all three write schedules) and my `TestReverifyOversizeSecretAtEveryPosition` — verbatim fragments observed |
| holdback forced to 0 in the fixed implementation | split secrets leak | FAILS `TestTailRedactsSecretSplitAcrossWrites` at split byte 1 and my adversarial schedules — the exact class the streaming design exists against |

End-to-end: implementer's `TestStartCrashRedactsOversizeStderrSecret` (real
process printing a 5 KiB secret byte-by-byte) passes `-race -count=15` in my
stability run, no flakes.

Residual shapes pinned, both consistent with the exact-match design:

- **Derived forms still leak — admission confirmed truthful**: base64-encoded and
  case-flipped copies of a secret pass through verbatim while the exact form is
  redacted (`TestReverifyDerivedFormsStillLeakAsAdmitted`). The matrix admits
  exactly this class and nothing more; the admission is honest, not overstated.
- **Stream-end strict prefix**: a process dying mid-print leaks up to
  `len(secret) − 1` bytes of a secret prefix verbatim (pinned:
  `TestReverifyPartialSecretAtEndOfStreamPinsResidual` — a 37-of-38-byte prefix
  appears whole). This is inherent to exact-match streaming redaction — the bytes
  never were the secret — but it is a *larger* residual than "derived forms"
  suggests; the matrix does not mention it. Documentation gap, see residuals.

## F2 — case-variant trust evasion: confirmed-fixed

Containment is now decided in the disk's own spelling, probed per component with
`os.SameFile` as the decider (`internal/proc/diskspelling.go`).

Independent probes in the report's original scenario shape (workspace `/tmp/…/ws`,
tool inside it), all pass on this case-insensitive APFS volume:

- `/WS/tool` and uppercased-parent variants of the workspace path: **denied**.
- Deep multi-level case variants (`workspace/subdir/bin/SRV` for
  `WorkSpace/SubDir/Bin/srv`): **denied**.
- Case-variant root paired with the exact executable: **denied** (no inversion).
- Unicode normalization: NFD spelling of an NFC workspace (`wo\u0308rkspace` vs
  `w\u00f6rkspace`): **denied**.
- Negative controls against overcorrection: the exact-spelling denial still
  fires, an outside executable passes, and a sibling `ws2/tool` — sharing only a
  string prefix — passes, including its own case variant `WS2/tool`.

Mutation evidence: neutralizing `diskSpelling` to identity makes every evasion
probe above fail with the original signature (`evaded the trust check: <nil>`),
including the implementer's `TestValidateExecutableDeniesCaseVariant`. The check
is load-bearing, and the wiring is genuine end to end: `lsp.Start` validates
`cmd.Path` against `opts.RootPath` before spawn, and the finders route through
the same `proc.ValidateExecutable`.

Lane B convergence: compared against `origin/spec-lane-b-paths`
`workspace.DiskSpelling` — the shapes are identical as claimed (per-component
directory reads, fold-equal candidate accepted only on `os.SameFile`, missing
components keep their spelling, drive-relative Windows bail). One divergence to
resolve at integration: Lane B folds with full Unicode case folding
(`cases.Fold`), Lane A with simple `strings.ToLower`; spellings that only
full-folding equates (Turkish İ, uppercase ẞ) are corrected by Lane B but not
Lane A. The convergence note in `diskspelling.go` (delete this copy in favor of
the workspace one) is the right disposition — Lane B's is the stronger key for a
trust boundary. Not a merge blocker for the ASCII/cased-ASCII attack class the
verifier demonstrated.

## F3 — finder denial misclassification: confirmed-fixed

Read every finder plus the end-to-end rendering chain; cross-checked by an
independent sub-verifier whose mutation-free test runs all pass:

- `validateCandidate` (languages.go:25) is the single shared classification
  point, applying the launch boundary's own `proc.ValidateExecutable`.
- `FindConfigured` returns the typed `ErrUntrustedExecutable` denial directly for
  a workspace-resident configured command; only non-denial failures produce
  "not installed". The original defect is closed.
- typescript/clangd/cmake/gopls: denial surfaces when the candidate is explicit
  ($SEE_* env, managed install, configured command); a PATH-discovered copy
  inside the workspace falls back to "not installed", which is the matrix's
  documented design (the user never pointed at it) and practically unreachable
  anyway since `exec.LookPath` does not yield workspace-relative paths.
- End to end: settings row renders `Denied` with "never run" detail
  (`TestSettingsRowMarksDeniedServers`), the failure notice reads "Denied:
  executables inside this workspace are never run"
  (`TestLspFailureExplainsTrustDenial`), `shortErr` maps the typed error rather
  than string-matching.

## F4 — budget-exhaustion surfacing: confirmed-fixed

The user-facing string is `"<server> crashed repeatedly — restart it with
Retry"` (locale `msgServerCrashedRepeatedly`, en + zh-Hans both present), with
the server name prefixed at the toolbar (toolbar.go:326) and the notice
(app.go `lspFailure`). `shortErr` special-cases `Unexpected/Persistent` so no
surface falls back to the bare `unexpected (persistent)`; the implementer's
control test pins that `operr.Error()` still renders the bare form, i.e. the
fix is at the presentation layer exactly as the verifier recommended, without
compromising the redaction-safe error design. The Retry button is rendered
adjacent, so the hint is actionable.

## F5 — manual Retry resets the crash budget: confirmed-fixed, mutation-verified twice

`Server.Retry()` zeroes `s.retries` when the state is `Failed`
(server.go:374-382), before resetting to Off. Mutation-verified independently:
with the reset line neutralized, `TestRetryResetsTheRestartBudget` fails at
review_fixes_test.go:68 (observed twice — once in the sub-verifier's deliberate
experiment, once in this verifier's own race run accidentally compiled during
that mutation window, an unplanned but exact reproduction of the mutation
signature). File restored, `git diff` empty, green re-run confirmed. The
reset-on-Failed-only shape is correct: mid-life retry-budget mechanics are
unchanged, and the test proves the post-Retry session earns the full automatic
budget exactly once.

## F6 — Degraded recovery and visibility: confirmed-fixed

`completeSync` clears Degraded on the next successful delivery
(server.go:489-493); since changes are full-text, a landed change does mean the
server's copy is current — for documents that were *opened* successfully. The
toolbar renders Degraded as its own state with its own label and explanation
(toolbar.go:278,334-335; locale `msgDegraded`/`msgDegradedExplanation`), no
longer as Ready, and clears after recovery
(`TestToolbarRendersServerFailureAndDegraded`). Both halves of the defect close.

## Regression evidence

```
go build ./...                                                             pass
go vet ./internal/... && go tool mygo vet .                                pass
CGO_ENABLED=0 go test -count=1 ./...                                       pass (whole tree, EXIT=0)
CGO_ENABLED=0 go test -race -count=1 ./internal/proc ./internal/lsp ./internal/langserver ./internal/tasks   pass
CGO_ENABLED=0 go test -race -count=5 ./internal/langserver                 pass
CGO_ENABLED=0 go test -race -count=10 -run 'TestVerify|TestRedact|TestStartCrash|TestTail' (4 pkgs)          pass
CGO_ENABLED=0 go test -race -count=15 -run 'TestStartCrash' ./internal/lsp pass — the crash-fixture flake fix (e86dda2) holds
```

One contamination incident, resolved: an early race run of
`internal/langserver` failed `TestRetryResetsTheRestartBudget` — traced to the
test binary having compiled while the sub-verifier's deliberate F5 mutation was
in place (mtime evidence plus an identical failure signature). The clean re-run
after restoration passes, and the incident independently confirms the mutation
discrimination of the test.

## Residuals (not blocking; documented here, two worth a matrix sentence)

1. **didOpen-failure recovery is protocol-non-strict and undocumented.** The
   fixer's handoff acknowledged this; the code and matrix do not. Pinned
   empirically (`TestReverifyFailedOpenHealsByProtocolViolatingChange`): when
   the initial `didOpen` fails, the document is registered locally anyway; a
   same-version re-sync neither resends nor succeeds; the next edit sends a
   bare `didChange` for a document the server never saw — the server is
   protocol-entitled to reject it, in which case the server stays visibly
   Degraded until restart (at least visible now, per F6). The REQ-EXT-010
   rationale "a landed change means the server's copy is current" is exact for
   change-failures but silent on this case. One sentence in the matrix would
   make it honest. Pre-existing shape, not a regression of this round.
2. **Stream-end partial-secret residual** (F1 section): up to `len(secret) − 1`
   verbatim bytes when a process dies mid-print. Inherent to exact-match
   redaction, but the matrix's remaining-limit text admits only derived forms.
   One sentence would close the gap.
3. **Fold-strength divergence from Lane B** (F2 section): simple vs full case
   folding; correct end state is Lane B's, already noted in the code for
   integration.

## Matrix honesty re-check

The c3b4da6 matrix update was re-read against the evidence above. Every claim
this round re-verified as accurate: the fix descriptions match what the tests
actually prove, the mutation claims reproduce, the probe-adoption claim is
byte-accurate, and the derived-form admission is truthful (spot-checked, not
merely believed). The two gaps above are omissions of things the matrix never
claims, not misstatements of things it does.
