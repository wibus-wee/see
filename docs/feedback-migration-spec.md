# Feedback migration spec — attention semantics

Internal working spec for the toast migration task. Delete or fold into docs when done.

## Background

See's user feedback today flows almost entirely through `a.note(msg, isErr)` → `a.notice`/`a.noticeError` → a single text slot in the toolbar pill (`internal/app/toolbar.go` ~line 301). Properties of that channel:

- No expiry: success confirmations ("Saved x", "Copied x.") stay until overwritten — stale events occupy the only feedback slot.
- Single slot, overwrite semantics: a later trivial event (a copy confirmation) silently destroys an earlier error (a save failure) with no trace.
- Errors carry no action: save failure is red text, no Retry. Compare LSP errors, which have a Retry button in the same pill.
- State and events share the slot: `a.watchErr` ("not watching for changes", a persistent state) competes with notices, and a notice masks it.

Meanwhile mygo (`github.com/wibus-wee/mygo`, see `ui/toast.go` in the module cache) has a complete toast system that See uses in exactly one place (`internal/app/counterparts.go`):

- `c.Toast(msg)` — 4s auto-dismiss, hover/keyboard-focus pauses the timer, `RoleStatus` + screen-reader `Announce` built in, bottom-center overlay, pointer pass-through.
- `c.ToastAction(msg, label, fn)` — 8s + action button (e.g. Undo).
- `c.AddToast(ui.Toast{...})` — full control: `ID` dedups/refreshes instead of stacking, `Timeout: -1` = until closed, `Type` for styling.
- Up to 3 visible; older ones wait hidden **with their timers still running** — so persistent errors must NOT rely on toasts; use banners for those.
- IMPORTANT: `c.Toast`/`AddToast` is only callable during view build. Feedback produced in business logic (often in `a.post.Post` callbacks) must be recorded in App state and converted to toast calls during the view pass — exactly the pattern `counterpartFeedback` in `counterparts.go` already uses.

## Target design (attention semantics)

Classify every piece of feedback by two questions: does the user need to DO something about it? where is its causal locus?

1. **Success confirmations / pure info events** (Saved, Copied, Committed, "no definition found", "already open"…) → toast, auto-dismiss. These need nothing from the user; self-erasure is the point. Dedup by ID so repeats refresh instead of stacking.
2. **Errors** → persistent until acknowledged or resolved. Banner in the window (the existing `a.banner` mechanism, WarnBg/Err-colored), with an action where recovery exists (Retry for save failure, etc.). An error must never be auto-dismissed and never silently overwritten by a success event.
3. **Persistent states** (watchErr, LSP server state) → own dedicated slot in the toolbar pill, no longer competing with events.
4. **Destructive-action confirm** (trash) → UNCHANGED, stays confirm banner. Rationale: mygo has no undelete/put-back API (verified: no Untrash/PutBack in shell*.go), so a real undo is impossible; a confirm is the honest cost. Do not fake undo.

## Requirements

### R1. Feedback mechanism

Build a small feedback layer in `internal/app` (new file, e.g. `feedback.go`):

- App state records pending feedback items: kind (success/info vs error), message, optional action (label + func), dedup ID.
- During the view pass, drain the queue into `c.AddToast` calls (success/info) or into a single error banner slot.
- Errors: one visible at a time in a banner; the banner must have a dismiss control and, where the call site provides one, a recovery action. If multiple errors are pending, latest wins for display but do not lose the fact errors occurred — simplest correct approach: replace (the user acts on the newest first). Document the choice in a comment.
- Success/info toasts: dedup by message identity so hammering the same action doesn't stack 3 copies.
- Keep `a.note` as the call-site API shape if convenient (it's at ~149 call sites in 36 files), but split it into semantically honest methods, e.g. `a.flash(msg)` for success/info events and `a.fail(msg, ...)` for errors — the point is call sites must declare intent, and the mechanism routes accordingly. Mechanical mass-edit is fine.

### R2. Classify all call sites

There are ~149 `a.note(...)` calls in ~36 non-test files under `internal/app/`. Rules:

- `isErr=false` → success/info event → toast. (Includes empty-result messages like "No definition found", "No language server configured".)
- `isErr=true` → error → persistent banner. Additionally, upgrade these specific high-frequency errors with a recovery action:
  - Save failure in `editing.go` (`MsgCouldNotSave`) → Retry action re-attempting the save.
  - The file-changed-on-disk case already has its own banner flow — check for double-reporting and dedupe.
  - LSP/server install errors keep their existing Retry patterns; don't double up.
- `MsgSaveOrCloseTheFileSUnsavedEdits` (git operations blocked by dirty files): this is a blocking precondition the user can resolve — keep as error, no action needed (the resolution is user-side).

### R3. State slot separation

`a.watchErr` ("not watching for changes") must not compete with event feedback. Give it its own independent display in the toolbar pill (it already partially has one — make sure events can't mask it and it can't mask events). Same for LSP state which already has its own branches — verify the pill switch no longer has events and states fighting over one slot.

### R4. Trash stays confirm

Do not change the trash/delete flow. Do not implement fake undo.

### R5. Settings inline feedback

`shellCommandMessage` in settings.go mirrors `a.notice` for install/uninstall of the shell command — this is correct inline feedback and must keep working after the refactor (it reads `a.notice` directly today). Update the plumbing so the settings card still shows the result inline; the toast/banner may additionally fire, that's fine.

### R6. Locales

New UI strings (e.g. dismiss label if the banner system lacks one, Retry where added) go into `locales/en.json` and `locales/zh-Hans.json` following the existing catalog pattern (check `catalog_gen.go` workflow — it appears generated; find the generator and use it).

### R7. Tests

- Many existing tests assert on `a.notice`/`noticeError` (grep: blame_test, config_note_test, e2e_test, formatting_test, git_partial_test, git_repositories_test, git_test, gitlog_actions_test, navigation_test, peek_test and more). Update them to assert on the new state.
- Add behavior-level tests for the new invariants:
  - a success event does not erase/overwrite a pending error
  - a pending error survives subsequent success events
  - success feedback is drained to toasts during the view pass (test at the state/drain level, not pixels)
  - error banner carries the recovery action when the call site provided one
- Follow existing test harness patterns in `harness_test.go`/`notify_test.go`.

### R8. Verification (DoD)

- `go build ./...` clean.
- `go test ./internal/app/ -count=1` green (full package — it's big, run it; note the baseline was green before the change).
- `go vet ./...` if the repo passes it at baseline (check first; don't introduce new failures).
- Zero remaining uses of the old overwrite-prone single-slot pattern for errors; success confirmations no longer persist.
- Committed in small logical commits on the current branch `toast-attention-design` (do NOT push, do NOT open a PR — the orchestrator handles that).

## Non-goals

- No redesign of the visual style of banners/toasts beyond what the semantics require.
- No changes to OS notifications (`notify.go`) — that layer is already correct.
- No undo-for-trash.
- Don't touch the docs/ tree otherwise; don't update README.
