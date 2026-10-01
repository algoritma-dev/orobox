# Design: `--agent` output mode

**Date:** 2026-09-17
**Status:** approved, not yet implemented

## Problem

Orobox is increasingly driven by LLM agents rather than by a human at a terminal. Everything the
CLI prints for a human — banners, spinners, progress lines, per-tool headers, success
confirmations, and the QA tools' full human-readable output — is input tokens the agent pays for
and then discards.

`orobox qa` is the worst case. It streams every enabled tool's native output straight to the
terminal: PHPStan's table, PHP-CS-Fixer's per-file progress, ESLint's grouped listing. A run over a
mid-sized bundle produces thousands of lines, of which the agent needs the findings and nothing
else.

## Goal

A global `--agent` flag that reduces every command's output to the minimum an automated caller
needs: the payload, the errors, and the exit code. Nothing else.

`orobox qa --agent` is the feature's centre of gravity. Everything else is the consistency that
makes the flag worth having.

## Contract

### Activation

`--agent` is a persistent flag on the root command:

```go
rootCmd.PersistentFlags().Bool("agent", false, "minimal output for automated callers: payload and errors only")
```

Flag only. It is deliberately **not** bound to viper: binding is what exposes a setting to
`AutomaticEnv` under the `ORO_` prefix, and agent mode must never switch on because of an inherited
environment. There is no auto-detection from a non-TTY stdout either — that would change the output
of every existing CI pipeline and of the e2e harness.

### Precedence

`--debug` wins over `--agent` when both are given. `--debug` exists to show everything; silently
discarding it would be a worse surprise than ignoring the newer flag. The `--agent` help text says
so.

### Streams

- **stdout** carries the payload only: QA findings, test failures, and the verbatim child output of
  the passthrough commands.
- **stderr** carries errors, one line each, prefixed `error: `, with ANSI colour disabled.
- Nothing else is printed. No banners, no spinners, no success confirmations.

A clean `orobox qa --agent` writes zero bytes and exits 0.

### Exit codes

Unchanged from today's behaviour. An agent reads the exit code, not the text.

### Prompts

`--agent` implies non-interactive. The `utils.Ask*` functions return their default without reading
stdin. A required value with no default produces one stderr line naming what to set, and exit 1:

```
error: bundle name required; pass --name or set bundle_name in .orobox.yaml
```

### Interaction with `--report`

Orthogonal. `orobox qa --report=gitlab --agent` still writes the GitLab artifact; it just prints no
summary.

## Architecture

### `internal/output`

A new package owning the mode and the writers:

```go
package output

func SetAgent(bool)      // called once from rootCmd.PersistentPreRun
func Agent() bool
func Err(msg string)     // one line, "error: " prefix, stderr, no colour
func Payload() io.Writer // os.Stdout
```

It is a new package rather than a variable in `internal/utils` because `utils` is imported by
`internal/docker`, `internal/pipeline`, `internal/certificates` and `cmd`. A mode flag living there
would make `utils` both the gate and the thing being gated. `internal/output` has no dependencies of
its own, so nothing can cycle through it.

### Gating the existing print helpers

`internal/utils/ui.go` is the single choke point for almost all of Orobox's output. The helpers
consult `output.Agent()`:

| Helper | Call sites | Agent-mode behaviour |
| --- | --- | --- |
| `PrintSuccess` | 52 | no-op |
| `PrintError` | 100 | one line via `output.Err` |
| `PrintWarning` | 69 | no-op |
| `PrintInfo` | 55 | no-op |
| `PrintTitle` | 14 | no-op |
| `StartLoader` / `StopLoader` | 25 | no-op |

No call site changes. `utils` imports `output`; there is no cycle.

Two helpers were added alongside them during implementation: `PrintPlain` and `PrintPlainf`, which
write a line verbatim — no glyph, no colour — through the same gate. They exist for the blocks of
URLs, credentials and `.env` snippets the commands print as a unit, where routing through
`PrintInfo` would have prepended `ℹ ` to every line and changed what a human sees today. See the
raw-print section below.

### The raw `fmt.Print*` sites

83 calls bypass the helpers. They fall into three buckets:

**Chrome (~55 calls)** — convert to the matching `utils.Print*` helper, keeping the text identical.
Most become `PrintPlain`/`PrintPlainf` rather than `PrintInfo`: these are multi-line blocks of URLs
and credentials, and a per-line `ℹ ` would change output every existing user sees.

| File | Calls |
| --- | --- |
| `cmd/up.go` | 24 |
| `cmd/deploy.go` | 14 |
| `cmd/test_init.go` | 5 |
| `internal/pipeline/run.go` | 4 |
| `internal/certificates/certs.go` | 3 |
| `cmd/run.go` | 3 |
| `cmd/ci_init.go` | 2 |
| `cmd/init.go` | 1 |
| `internal/docker/customimage.go` | 1 |

**Prompt rendering (15 calls, `internal/utils/ui.go`)** — left alone. The non-interactive rule means
those functions return before they print.

**Error passthrough (11 calls, `internal/docker/compose.go`)** — the `fmt.Print(stderrStr)` dumps in
the silent-runner failure paths. Kept, because that is the real diagnostic an agent needs, but
routed to stderr in agent mode.

The split between chrome and payload is a judgement call made file by file. A wrong call leaks a
banner or swallows a diagnostic. The golden-output tests below are the net, not care.

### Docker

`RunComposeCommandSilently` already passes `--progress quiet` outside debug mode; agent mode takes
the same branch. `RunComposeCommand` streams child stdout straight through, which is correct for the
passthrough commands and is what `qa` and `test` override for themselves.

## `orobox qa --agent`

### Running the tools

`--agent` forces `Report: ReportGitLab` internally even when `--report` was not given. `Mode` is
untouched: `ModeFix` locally, as today, so the tools still fix what they can.
`qatools.Tools` already treats mode and report as orthogonal, so this combination needs no change
there.

`runQaOnCompose` does not, and this is the one place the change is not additive. It derives the mode
from the format:

```go
mode := qatools.ModeFix
if format != qatools.ReportNone {
    mode = qatools.ModeCheck
}
```

That line exists because a caller who asks for a report wants to be told what the tree looks like,
not to have the tree rewritten under it. An agent-mode run that merely *uses* the report machinery
is not such a caller. So the condition has to distinguish a format the user asked for from one agent
mode forced, by passing the user's own `--report` value alongside the effective format rather than
re-deriving intent from the format. The `staged != nil` override that follows it is unaffected: a
staged run stays check-only in agent mode for the same reason it does today.

The run is captured rather than streamed — `docker.RunComposeCommandWithOutput` instead of
`RunComposeCommand`. `qatools.ReportScript` echoes `--- Running <tool> ---` before each tool, and the
JS tools print their human output to stdout while writing their JSON to the file named by an
environment variable (see `internal/report/testdata/NOTES.md`). Captured bytes are discarded on
success and written to stderr when the engine itself fails.

The per-tool JSON goes to the existing raw report directory, the same one `--report` uses. When
`--report` was not asked for, the directory is removed after the merge.

### Reading the reports

```go
type Finding struct {
    Tool     string
    Path     string
    Line     int
    Severity string
    Message  string
}

func Findings(reports []ToolReport, prefix PathPrefix) ([]Finding, error)
```

`MergeCodeQuality` cannot serve this: it collapses every tool's issues into one list and keeps only
per-tool counts, so tool attribution is gone by the time it returns. `Findings` keeps it. The two
share `toolIssues` and `rewriteIssuePath`, and neither replaces the other — `MergeCodeQuality` still
serves `--report`.

### Output

```
fixed 7 files (php-cs-fixer 5, rector 2)
src/Entity/Order.php:42 error phpstan Access to undefined property App\Entity\Order::$foo
src/Form/Type/OrderType.php:11 major eslint semi Missing semicolon.
... 38 more (--report=gitlab for the full list)
```

- One finding per line, `path:line severity tool message`.
- Sorted by path, then by line. Stable across runs.
- Message newlines collapse to single spaces, so a finding is always exactly one line.
- Severity is the tool's own CodeClimate value (`blocker`, `critical`, `major`, `minor`, `info`),
  unmapped.
- At most 50 finding lines, then the `... N more` line.
- A clean tree produces no output at all.

### Fixed versus remaining

In fix mode the tools do not all report the same thing. Some list what they rewrote; others list
what is left. A per-tool table decides:

| Tool | Fix-mode report | Treatment |
| --- | --- | --- |
| `php-cs-fixer` | files it rewrote | counted in the `fixed` line |
| `rector` | changes it applied | counted in the `fixed` line |
| `phpstan` | what remains | listed |
| `eslint` | what remains after `--fix` | listed |
| `stylelint`, `stylelint-css` | what remains after `--fix` | listed |
| `twig-cs-fixer` | **unverified** | listed, until verified |

`twig-cs-fixer --fix --report=gitlab` has not been checked against the real tool. The default for an
unclassified tool is to list its findings, so a wrong assumption produces noise rather than silence.
The fix-mode fixture capture below settles it.

### A tool that could not run

`qatools.ReportScript` already records each tool's exit code in its own status file. A non-zero
status with an empty report is a tool that failed to run, not a clean tool:

```
error: phpstan could not run (exit 255)
```

on stderr, exit 1. Today that case is invisible in fix mode.

### Exit code

1 if any listed finding remains, or if any tool failed to run. 0 when only fixes happened. This
matches today's behaviour: `php-cs-fixer` and `rector` already exit 0 after applying fixes.

### Other engines and modes

The Dagger engine uses the same formatter over `result.QAReportDir` with
`qaPathPrefix(engineDagger)`, exactly as the report path does today.

`--generate-baseline --agent` writes the baseline file, prints nothing, and exits 0.

## `orobox test --agent`

Same shape as `qa`: `--log-junit` is forced on internally, PHPUnit's stdout is captured rather than
streamed, and only failures are printed.

```go
type TestFailure struct {
    Suite   string
    Test    string
    Kind    string // "failure" or "error"
    Message string
}

func FailuresFromJUnit(doc []byte) ([]TestFailure, error)
```

This is new because `MergeJUnit` keeps each suite's body as opaque `innerxml` — it deliberately
models no children at all, so nothing in the codebase today can read a failure out of a run.
`MergeJUnit` is untouched.

```
Oro\Bundle\XBundle\Tests\Unit\OrderTest::testTotal failure Failed asserting that 2 matches 3
7 passed, 1 failed, 0 errors
```

The same 50-line cap applies. A clean run prints the summary line only.

PHPUnit writes a failure's location twice: as `file` and `line` attributes on `<testcase>`, and
again as prose at the end of the failure body. The attributes are what the implementation reads —
they are written for every case, and reading them needs no parsing of a human-readable message.
The trailing prose is stripped from the message, since the fields already carry it.

## Per-command behaviour

| Behaviour | Commands |
| --- | --- |
| Formatted payload | `qa`, `test` |
| Child output verbatim | `logs`, `shell`, `console`, `run`, `db` |
| Silent, exit code only | `up`, `down`, `clean`, `xdebug`, `init`, `qa-init`, `test-init`, `ci-init`, `deploy-init`, `deploy`, `self-update`, `create`, `internal-gen-docker` |

`db` is verbatim because `db dump` and `db restore` produce real output a caller consumes.

## Testing

**Unit.** `report.Findings` and `report.FailuresFromJUnit` against the six captured fixtures in
`internal/report/testdata/`. Those fixtures were captured in check mode, so a fix-mode capture of
`php-cs-fixer`, `rector` and `twig-cs-fixer` is added alongside them and `NOTES.md` is extended the
way it already documents this kind of surprise. This is what resolves the `twig-cs-fixer` row above.

**Golden output.** Table tests that run each command's formatter with agent mode on and assert
byte-exact stdout and stderr. This is the net under the chrome-versus-payload split across the 83
raw print sites: a leaked banner fails a test.

**End-to-end.** The `e2e/` harness gains an agent-mode QA run against the existing broken-file probe:
exit 1, stdout matching the line grammar, and zero bytes against a clean tree.

## Documentation

`docs/configuration.md` and the QA documentation gain an `--agent` section.

## Out of scope

Deliberately excluded, so a later reader does not mistake these for oversights:

- A JSON or JSONL output mode. Compact text was chosen for token cost.
- An `ORO_AGENT` environment variable or any config-file key. Flag only.
- A `--quiet` alias.
- Per-tool verbosity flags.
- Auto-detection from a non-TTY stdout.
