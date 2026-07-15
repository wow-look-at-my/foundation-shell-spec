---
title: Execution Specification
description: Command execution semantics - run modes, chain and pipeline execution, builtin commands, exit codes, and signal handling.
recommend_after: redirection.md
---

# Execution Specification

> **Canonical for:** command execution — run modes, chain and pipeline execution, builtin commands, exit codes, signals, and runtime error reporting. Operator SEMANTICS are canonical in operators.md, redirection in redirection.md, expansion in expansion.md. The authority map lives in the README.

Implementation: the foundation-shell repository; this specification is authoritative.

## Overview

Foundation Shell's execution system consists of three layers:

1. **Shell Layer** — run modes (interactive, non-interactive, script, single command), I/O management, the last-status register
2. **Chain Layer** — pipeline grouping and operator evaluation, command substitution executor
3. **Command Layer** — individual command execution: redirection setup, builtin dispatch, external processes

## Architecture

```
User Input
     │
     ▼
┌────────────┐
│   Shell    │  → run modes, lastExitCode, signal handling
└─────┬──────┘
      │
      ▼
┌────────────┐
│   Parser   │  → Chain structure (expansion happens here)
└─────┬──────┘
      │
      ▼
┌────────────┐
│   Chain    │  → pipeline grouping, operator evaluation
│  Executor  │
└─────┬──────┘
      │
      ▼
┌────────────┐
│  Command   │  → redirections, builtin or external execution
│  Executor  │
└────────────┘
```

## Shell Layer

### Shell Structure

```go
type Shell struct {
    stdin         io.Reader
    stdout        io.Writer
    stderr        io.Writer
    isInteractive bool
    lastExitCode  int
    executor      expander.SubstitutionExecutor
}
```

(`SubstitutionExecutor` is the renamed `SubshellExecutor`; "subshell" is reserved for future `()` grouping.)

### Creation

```go
// Standard I/O, specify interactivity
func New(interactive bool) *Shell

// Custom I/O streams (for testing)
func NewWithIO(stdin io.Reader, stdout, stderr io.Writer, interactive bool) *Shell
```

### Run Modes

#### Interactive Mode

Active when `isInteractive == true`.

- Reads ONE LINE at a time via readline, with syntax highlighting and a colored prompt
- **Each line is a separate parse and a separate execution.** `$?` therefore reflects the previous line (expansion.md §Special Parameters)
- Empty lines are silently skipped
- Handles Ctrl+C (clear line, new prompt) and Ctrl+D (exit)

```go
func (s *Shell) runInteractive(ctx context.Context) int
```

Features: welcome message on start; green `$` prompt after success, red after failure; real-time syntax highlighting via the Painter interface.

#### Non-Interactive Mode (Whole-Input)

Active when `isInteractive == false` (piped stdin, no TTY).

```go
func (s *Shell) runNonInteractive(ctx context.Context) int
```

The shell reads **ALL of stdin to EOF**, parses the entire text as **ONE input**, and executes the resulting single chain in order. Within that input, unquoted newlines separate commands (lexer.md §3.4) and `#` comments — including a shebang line — are removed by the lexer (lexer.md §8).

Consequences — each of these is intended, specified behavior:

1. **Commands inherit the shell's stdin, which is at EOF.** The shell has already consumed the pipe, so a command that reads stdin sees immediate EOF — it can never steal script text:

   ```
   $ printf 'echo got:$(cat)\n' | fsh
   got:
   ```

   (POSIX shells share the input stream with commands line-by-line; this is a documented deviation.)
2. **A parse error anywhere rejects the whole input.** An unclosed quote on the last line means NOTHING executes: the shell prints the diagnostic and exits with status 1. In exchange, quoted strings and substitution bodies may span lines.
3. **`exit` stops the sequence** (§exit): the remaining commands do not run and the shell exits with the given status.
4. **`$?` sees the pre-input status.** The whole input is one parse, so every `$?` in it expands before anything runs (expansion.md §Special Parameters).

The shell's exit status is the chain's final status (or the `exit` status, or 1 after a parse error).

#### Script Execution

```go
func (s *Shell) RunScript(ctx context.Context, filename string) int
```

Opens `filename`, reads it **in full**, and executes it exactly like non-interactive whole-input mode. Two notes:

- The script file is NOT the shell's stdin. Commands inherit the stdin the shell itself was started with (e.g. the terminal), so `fsh script.sh < data.txt` lets commands in the script read `data.txt`
- The shebang line is an ordinary `#` comment (lexer.md §8); no special first-line handling exists or is needed

If the file cannot be opened: `cannot open script file <name>: <os reason>` on stderr, exit status 1.

#### Single Command Execution

```go
func (s *Shell) RunCommand(ctx context.Context, cmdStr string) int
```

Parses `cmdStr` as ONE input with the same whole-input semantics (newlines inside the string separate commands) and executes it. This is the entry point used by `fsh-exec`, which joins its arguments into the command string.

## Chain Execution

### Chain Structure

```go
type Chain struct {
    Commands  []*CommandSpec
    Operators []token.TokenType
}
```

Invariant: `len(Operators) == len(Commands) - 1` (parser.md).

### Chain Execution Functions

```go
// Default I/O
func Execute(ctx context.Context, chain *parser.Chain) (exitCode int, err error)

// Custom I/O
func ExecuteWithIO(ctx context.Context, chain *parser.Chain,
    stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error)
```

The `error` return is reserved for INTERNAL failures (nil chain, I/O plumbing). Ordinary command failures — command not found, redirection open failure, non-zero exits — are never chain-fatal errors; they are exit statuses (see §Runtime Failures Never Abort the Chain).

### Execution Algorithm

```
1. Group consecutive |-connected commands into pipeline segments
2. Evaluate segments left to right, applying &&, ||, ; between them
   with full skip propagation: a skipped segment PRESERVES the current
   status, and the operator following a skipped segment is evaluated
   against that same preserved status
3. The chain's status is the status of the last segment actually
   executed (or the preserved status, if trailing segments were skipped)
```

Skip propagation means `false && a && b` runs NOTHING and yields 1, and `true || a || b` runs nothing and yields 0. Operator semantics — including the normative skip-propagation algorithm — are canonical in operators.md (§Execution Algorithm); this file defers to it.

### Runtime Failures Never Abort the Chain

A command that cannot run at all is an ORDINARY failure, indistinguishable — for control flow — from a command that ran and exited non-zero:

| Failure | Status | Message (to the command's stderr) |
|---------|--------|-----------------------------------|
| Command not found | 127 | `<name>: command not found` |
| Found but not executable | 126 | `<name>: <os reason>` |
| Redirection target fails to open | 1 | `cannot open input file <name>: <os reason>` / `cannot open output file <name>: <os reason>` (redirection.md §9) |

The message is printed, the status feeds operator evaluation, and the chain CONTINUES:

```bash
nosuchcmd || echo fallback    # prints fallback; status 0
nosuchcmd ; echo next         # prints next
cat < /missing || echo recovered   # prints recovered; status 0
```

### Command Substitution Executor

```go
type Executor struct {
    ctx    context.Context
    stdin  io.Reader
    stderr io.Writer
}

func NewExecutor(ctx context.Context, stdin io.Reader, stderr io.Writer) *Executor

func (e *Executor) Execute(command string) (output string, exitCode int, err error)
```

`Executor` implements `expander.SubstitutionExecutor` (expansion.md §Executor Interface). Behavior:

1. **Recursive parse**: the body is parsed with `parser.ParseWithExecutor(command, e)` — the SAME executor — so substitutions nested inside the body expand through recursion (expansion.md §Recursive Execution)
2. **Same process**: the body's chain executes in-process with the normal chain executor. stdout is captured to a buffer; stderr passes through to the shell's stderr; stdin is the shell's stdin
3. **Return values**: captured stdout and the chain's final status. The CALLER (the expansion layer) discards the status (expansion.md §Failure Semantics)
4. **Errors**: `err` is non-nil only when the body fails to PARSE (surfaced as `parse error: command substitution error: <err>`) or on internal failure. Runtime command failures inside the body print to stderr and do NOT produce `err` — the substitution yields whatever stdout was captured and the outer line continues
5. **State mutations are visible**: builtins and assignments in the body mutate the shell (§Builtin Commands); `exit` in the body stops only the body's sequence (§exit)

Benefits of the same-process model: access to the same environment, no shell-spawning overhead, consistent behavior with the main shell.

## Pipeline Execution

Commands connected by `|` are grouped and executed as one pipeline segment:

```
cmd1 | cmd2 | cmd3 && cmd4 | cmd5
└───────────────┘    └──────────┘
   Segment 1           Segment 2
```

```go
func executePipelineWithIO(ctx context.Context, commands []*parser.CommandSpec,
    defaultStdin io.Reader, defaultStdout, defaultStderr io.Writer) (int, error)
```

### Algorithm

```
1. Create N-1 pipes for N commands
2. Wrap stderr in a synchronized writer (concurrent access)
3. Launch all commands concurrently
4. Wire I/O:
   - First command:  defaultStdin  → pipe[0]
   - Middle command: pipe[i-1]     → pipe[i]
   - Last command:   pipe[N-2]     → defaultStdout
5. When a command finishes (or fails to start), close BOTH of its pipe
   ends: the write end of its stdout pipe (downstream sees EOF) AND the
   read end of its stdin pipe (upstream writes start failing)
6. Wait for all commands
7. Return the exit status of the LAST (rightmost) command
```

### Early Exit Terminates Producers (Normative)

Closing the read end in step 5 is REQUIRED. When a consumer exits without draining its input, the producer's next write fails (the in-process equivalent of EPIPE/SIGPIPE); that write failure **terminates the producer silently** — it is not an error, produces no message, and cannot affect the pipeline's status (the rightmost command has no stdout pipe, so it can never be the one terminated this way).

```bash
yes | head -1        # prints y, exits immediately, status 0 - MUST NOT hang
seq 1 100000 | head -2   # prints 1 2, exits, status 0
echo x | pwd         # pwd never reads stdin - MUST NOT hang
```

The same rule covers a pipeline command whose stdin is redirected from a file: its incoming pipe is not connected, and its read end is closed at wiring time so the upstream producer terminates instead of blocking (redirection.md §7.4).

### Spawn Failures Inside Pipelines

A command that fails to spawn ANYWHERE in a pipeline is reported, not swallowed, and does not abort or wedge the pipeline:

1. Its message prints to the (synchronized) stderr — `<name>: command not found` etc.
2. Its position reports status 127/126/1 as usual
3. Its pipe ends are closed immediately (step 5), so neighbors see EOF / stop writing and the pipeline runs to completion
4. The exit status is still the rightmost command's

```bash
nosuchcmd | cat       # stderr: nosuchcmd: command not found; status 0 (cat)
echo hi | nosuchcmd   # stderr: nosuchcmd: command not found; status 127
```

### Builtins in Pipelines

Builtins execute IN-PROCESS even as pipeline members (§Builtin Commands). Their side effects mutate the parent shell — a documented deviation from POSIX shells, which run pipeline members in subshells:

```bash
cd / | cat ; pwd      # prints /   (POSIX shells print the original directory)
```

### Thread Safety

Stderr is wrapped in a synchronized writer to prevent interleaved error output from concurrent pipeline commands:

```go
type syncWriter struct {
    mu sync.Mutex
    w  io.Writer
}
```

## Command Execution

### CommandSpec Structure

```go
type CommandSpec struct {
    Args         []string
    InputFile    string
    OutputFile   string
    ErrorFile    string
    AppendOutput bool
    AppendError  bool
}
```

### Execute Function

```go
func Execute(ctx context.Context, spec *parser.CommandSpec,
    stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error)
```

### Execution Flow

```
1. Validate: spec != nil && len(Args) > 0
2. Open redirections in order: InputFile, OutputFile, ErrorFile
   (redirection.md §5.2). On open failure: print the canonical message
   (redirection.md §9) to the command's stderr, return status 1 —
   the command does NOT execute
3. Track opened files for cleanup (deferred close)
4. Standalone assignment? → perform it (§Standalone Assignment)
   Builtin?              → ExecuteBuiltin (in-process)
   Otherwise             → executeExternal
5. Return the exit status
```

### External Command Execution

```go
func executeExternal(ctx context.Context, name string, args []string,
    stdin io.Reader, stdout, stderr io.Writer) (int, error) {

    cmd := exec.CommandContext(ctx, name, args...)
    cmd.Stdin = stdin
    cmd.Stdout = stdout
    cmd.Stderr = stderr

    err := cmd.Run()
    if err == nil {
        return 0, nil
    }

    // Interrupt / cancellation (§Signals and Cancellation)
    if ctx.Err() != nil {
        return 130, nil
    }

    // Spawn failures - classified BEFORE the exit-status cases
    if errors.Is(err, exec.ErrNotFound) {
        fmt.Fprintf(stderr, "%s: command not found\n", name)
        return 127, nil
    }
    if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ENOEXEC) ||
        errors.Is(err, syscall.EISDIR) {
        fmt.Fprintf(stderr, "%s: %s\n", name, osReason(err))
        return 126, nil
    }

    // Ran and exited non-zero, or was killed by a signal
    var exitErr *exec.ExitError
    if errors.As(err, &exitErr) {
        if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
            return 128 + int(ws.Signal()), nil
        }
        return exitErr.ExitCode(), nil
    }

    // Producer terminated because its consumer exited (§Early Exit):
    // success-equivalent, silent
    if errors.Is(err, io.ErrClosedPipe) {
        return 0, nil
    }

    // Any other failure: generic
    fmt.Fprintf(stderr, "%s: %s\n", name, osReason(err))
    return 1, nil
}
```

`osReason(err)` is the bare OS error text (`permission denied`, `is a directory`, `exec format error`) — unwrapped, with no `fork/exec <path>:` prefix. Go's `ExitError.ExitCode()` returns −1 for signal deaths, which is why the `Signaled()` branch MUST come first: a child killed by signal N reports 128+N, never −1 or 255.

## Builtin Commands

Foundation Shell has exactly SIX builtin commands, plus one command FORM (standalone assignment):

| Name | Purpose | Typical status |
|------|---------|----------------|
| `cd` | Change the working directory | 0 / 1 |
| `pwd` | Print the working directory | 0 |
| `exit` | Terminate the shell or the current sequence | (the given code) |
| `clear` | Clear the terminal screen | 0 |
| `help` | List builtins and shell syntax | 0 |
| `export` | Set environment variables / print the environment | 0 / 1 |
| `NAME=VALUE` | Standalone assignment (recognized by shape, not name) | 0 |

Anything else — including `echo`, `true`, `false`, `test` — is an external command resolved via `$PATH`.

### Detection

```go
func IsBuiltin(name string) bool
```

True exactly for the six names above. Standalone assignment is not name-based (§Standalone Assignment).

### Execution API

```go
func ExecuteBuiltin(name string, args []string,
    stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error)
```

Builtins return their exit status directly. The `err` return is reserved for the `exit` sentinel and internal failures. Ordinary builtin failures (missing directory, invalid export name) are NOT errors: the builtin prints its own message to stderr and returns a non-zero status, and the chain continues per operator logic.

### Common Properties

1. **In-process, always.** Builtins execute in the shell's own process — including inside pipelines and command substitutions — and their side effects (directory changes, environment changes) mutate the parent shell. This is a deliberate deviation from POSIX (`cd / | cat ; pwd` prints `/`)
2. **Redirections apply** (redirection.md §12.3): `pwd > f` writes the directory to `f`
3. **No builtin reads stdin**
4. **Messages are prefix-free**: builtin errors print as `<builtin>: <reason>` with no `execution error:` or shell-name wrapper

### cd

```
cd [dir]
```

| Case | Behavior |
|------|----------|
| No argument | Change to the home directory. If it cannot be determined (HOME unset/empty): `cd: could not determine home directory` to stderr, status 1 |
| One argument | Change to that directory (the argument has already been through the full expansion pipeline, so `cd ~/src` and `cd $DIR` work) |
| Extra arguments | Ignored (only the first is used) |
| Failure | `cd: <path>: <os reason>` to stderr (e.g. `cd: /nope: no such file or directory`), status 1 |
| Success | Status 0, no output |

`cd` does NOT maintain the `PWD` environment variable — `PWD` keeps whatever value the environment had at shell startup. Use `pwd` (or `$(pwd)`) for the current directory. Symlinks are resolved by the OS (physical behavior; there is no `-L`/`-P`).

### pwd

```
pwd
```

Prints the current working directory followed by a newline to stdout; status 0. Arguments are ignored. If the working directory cannot be determined: `pwd: <os reason>` to stderr, status 1.

### exit

```
exit [code]
```

| Case | Behavior |
|------|----------|
| No argument | Exit with the shell's last recorded status — the same value `$?` expands to (§Last Exit Code) |
| Numeric argument | Exit with that value modulo 256, non-negative (`exit 0` → 0, `exit 256` → 0, `exit -1` → 255, `exit 300` → 44) |
| Non-numeric argument | `exit: <arg>: numeric argument required` to stderr, status 2, and the shell does NOT exit |
| Extra arguments | Ignored (only the first is examined) |

**Sentinel semantics.** `exit` NEVER terminates the process directly (no `os.Exit` inside the builtin — that would bypass output capture, deferred file closes, and readline teardown). It returns its code together with a sentinel error:

```go
type ErrExit struct{ Code int }

func (e ErrExit) Error() string {
    return fmt.Sprintf("exit %d", e.Code)
}
```

Each layer decides what the sentinel means:

| Where `exit` runs | Effect |
|-------------------|--------|
| Top level (a segment executed directly by the chain executor) | The chain STOPS — no further segments run — and the shell terminates with the code after normal cleanup. In whole-input mode this ends the script/input |
| Inside a multi-command pipeline segment | Sets only that command's exit status within the pipeline; the shell survives. `exit 7 \| cat ; echo after` prints `after`, final status 0 (rightmost `cat`) |
| Inside a command substitution | Stops only the substitution's own sequence; output captured so far is used; the shell survives. `echo before $(exit 7) ; echo after` prints `before` (empty substitution) and `after` |

The usage-error case (status 2) does not raise the sentinel — the shell keeps running.

### clear

```
clear
```

Writes the ANSI clear-screen sequence `\033[2J\033[H` (clear screen, cursor home) to stdout; status 0. Arguments are ignored.

### help

```
help
```

Prints a command overview to stdout; status 0. The exact text and coloring are not normative, but the content MUST:

- List every builtin (`cd`, `pwd`, `exit`, `clear`, `help`, `export`) and standalone assignment
- List the real chain operators: `|`, `&&`, `||`, `;`
- List the redirections: `< file`, `> file`, `>> file`, `2> file`, `2>> file`
- NOT advertise unimplemented features — in particular there is no `&` background operator (lexer.md §3.3.2)

### export

```
export                       # print the environment
export NAME=VALUE [NAME2=VALUE2 ...]
export NAME [NAME2 ...]      # no-op names
```

| Case | Behavior |
|------|----------|
| No arguments | Print the entire environment to stdout, sorted by name, one `NAME=value` per line; status 0 |
| `NAME=VALUE` | Set the environment variable. The FIRST `=` splits name from value; the value may be empty (`export A=` sets `A` to the empty string) and may itself contain `=` |
| `NAME` (valid name, no `=`) | No-op, success — every variable is already an environment variable (§All Variables Are Environment Variables) |
| Invalid name | `export: invalid name: <arg>` to stderr; processing CONTINUES with the remaining arguments |

A name is valid iff it matches `[A-Za-z_][A-Za-z0-9_]*` (expansion.md §Variable Expansion). `export =x`, `export 1X=y`, and `export A-B=z` are invalid-name errors.

The final status is the status of the LAST failing argument (1), or 0 if every argument succeeded:

```bash
export A=1 1BAD=2 B=3    # sets A and B; stderr: export: invalid name: 1BAD=2
                         # status 1
```

Arguments reach `export` fully expanded: `export A=$B` assigns `B`'s value, and `export A="x y"` assigns `x y`.

### Standalone Assignment

A command is a standalone assignment when ALL of the following hold:

1. The command has exactly ONE word (redirections do not count as words)
2. That word's token was fully unquoted (`WasQuoted == false`)
3. The word's EXPANDED value matches `NAME=VALUE`, where `NAME` is a valid variable name and `VALUE` (everything after the first `=`) may be empty

Effect: set the environment variable; status 0; no output.

```bash
A=hello          # sets A=hello, status 0
A=               # sets A to the empty string
A=$B             # sets A to B's value (expansion ran first)
A=B=C            # sets A to "B=C" (first = splits)
'A=B'            # NOT an assignment (quoted): runs a command named A=B -> 127
A="B"            # NOT an assignment (whole-token WasQuoted - see below)
A=x cmd          # NOT an assignment (two words): runs a command named A=x -> 127
1X=y             # NOT an assignment (invalid name): runs a command named 1X=y -> 127
```

Notes — all deliberate:

- **Whole-token quoting granularity**: the `WasQuoted` flag covers the whole token (expansion.md §Tilde Expansion documents the same granularity), so `A="hello world"` is NOT an assignment. To set a value containing spaces (or any quoted value), use `export A="hello world"` — `export` matches on the expanded argument and has no unquoted requirement
- **Recognition uses the expanded value**: an unquoted `$X` whose value is `A=B` IS an assignment (POSIX shells recognize assignments before expansion; documented deviation)
- **Prefix assignments are NOT supported**: `VAR=x cmd` does not set `VAR` for `cmd`; it runs a command literally named `VAR=x` (typically 127). Per-command environment prefixes are a possible FUTURE feature
- **Redirections on an assignment** are opened and closed with their usual side effects (creation/truncation); the assignment itself produces no output

### All Variables Are Environment Variables

Foundation Shell does not distinguish shell variables from exported variables. Every variable set by `export` or standalone assignment is immediately an environment variable, inherited by every subsequently started child process. This is a deliberate simplification over POSIX (where `var=x` alone is not inherited until exported).

## Exit Codes

### Standard Exit Codes (Normative)

This table is not advisory — the executor MUST produce these values:

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Generic failure — including redirection open failures and empty-target/`&`-target parse-rejected lines (redirection.md §9) |
| 2 | Builtin usage error (e.g. `exit foo`) |
| 126 | Command found but not executable (permission denied, is a directory, exec format error) |
| 127 | Command not found |
| 128+N | Command killed by signal N (e.g. 137 = SIGKILL, 143 = SIGTERM) |
| 130 | Interrupt — SIGINT (128+2), including context cancellation |

Exit codes are in the range 0–255. External commands' own exit statuses (1–255) pass through unchanged.

### Exit Code Flow

```
Command → Exit Status → Operator Logic (operators.md) → Shell.lastExitCode → prompt color, $?
```

### Last Exit Code

```go
type Shell struct {
    lastExitCode int
}
```

`lastExitCode` is THE value behind `$?` (expansion.md §Special Parameters) and the prompt color. It starts at 0 and is updated after each command line:

| Event | New value |
|-------|-----------|
| Command line completes | The chain's final status |
| `exit` at top level | The exit code (the shell then terminates) |
| Parse error | 1 |
| Command interrupted | 130 (or 128+N for other signals) |
| Command substitution completes | UNCHANGED — substitution statuses are discarded (expansion.md) |

## Signals and Cancellation

### While a Child Runs

While a foreground child process is running, SIGINT and SIGQUIT delivered to the shell are **forwarded to the child**; the shell itself does not terminate.

- The child dies (or handles the signal); a signal death reports 128+N — Ctrl+C is 130
- The shell records the status and continues: in interactive mode it shows a new prompt; operator logic treats 130 like any failure (`&&` skips, `||` runs)
- This holds for EVERY command in the session, not just the first — the shell's signal disposition must not decay after a command completes

### At the Prompt (Interactive)

| Input | Behavior |
|-------|----------|
| Empty line | Skip, show new prompt |
| Ctrl+C | Clear the current line, show new prompt (shell does not exit) |
| Ctrl+D | Exit the shell |
| Valid command | Parse and execute |

### Context Cancellation

All execution functions accept a `context.Context`. Cancellation is treated as an interrupt: the running child is killed and the command reports **130**. `exec.CommandContext` provides the kill; the executor maps the result to 130 (§External Command Execution).

## Error Handling

### Parse Errors

```go
cmdChain, err := parser.ParseWithExecutor(line, s.executor)
if err != nil {
    if errors.Is(err, parser.ErrEmptyInput) {
        return true // silent skip (interactive)
    }
    result := syntax.Analyze(line)
    if !result.Valid {
        fmt.Fprint(s.stderr, syntax.FormatDiagnostics(line, result.Errors))
    } else {
        fmt.Fprintf(s.stderr, "parse error: %v\n", err)
    }
    s.lastExitCode = 1
}
```

Caret diagnostics come from the analyzer when it detects the problem (diagnostics.md); otherwise the parser's canonical string (diagnostics.md §5.2) prints with the `parse error: ` prefix. In whole-input mode a parse error rejects the entire input (§Non-Interactive Mode).

### Runtime Failures

Runtime failures print **at the failing command, to that command's stderr, with NO wrapper prefix** — there is no `execution error:` prefix on any per-command failure:

| Failure | Message |
|---------|---------|
| Command not found | `<name>: command not found` |
| Not executable | `<name>: <os reason>` |
| Redirection open failure | `cannot open input file <name>: <os reason>` / `cannot open output file <name>: <os reason>` (canonical in redirection.md §9) |
| Builtin failure | `<builtin>: <reason>` (§Builtin Commands) |

All failures then flow through operator logic as exit statuses (§Runtime Failures Never Abort the Chain).

### Internal Errors

A non-nil `error` from `ExecuteWithIO` indicates an internal failure (broken invariant, I/O plumbing), not a command failure. The shell reports it as `execution error: <err>` — in normal operation this path is never taken.

### Defined Errors

```go
var ErrEmptyCommand = errors.New("empty command") // parser-level (diagnostics.md §5.2)

type ErrExit struct{ Code int } // exit sentinel (§exit)
```

## Prompt System

### Prompt Colors

```go
const (
    promptSuccess = "\033[32m" // Green
    promptFailure = "\033[31m" // Red
    promptReset   = "\033[0m"
)
```

### Prompt Generation

```go
func (s *Shell) getPrompt() string {
    if s.lastExitCode == 0 {
        return promptSuccess + "$ " + promptReset
    }
    return promptFailure + "$ " + promptReset
}
```

## Interactive Features

### Readline Configuration

```go
cfg := &readline.Config{
    Prompt:          s.getPrompt(),
    InterruptPrompt: "^C",
    EOFPrompt:       "exit",
    Painter:         painter, // syntax highlighting
    Stdout:          s.stdout,
    Stderr:          s.stderr,
}
```

### Syntax Highlighting Painter

```go
type syntaxPainter struct {
    highlighter *syntax.Highlighter
}

func (p *syntaxPainter) Paint(line []rune, pos int) []rune {
    return []rune(p.highlighter.Highlight(string(line)))
}
```

## Examples

### Simple Command

```
Input: ls -la
Flow:  Shell → Parser → Chain(1 cmd) → Command → executeExternal(ls, [-la])
```

### Pipeline

```
Input: cat file | grep pattern | wc -l
Flow:  Shell → Parser → Chain(3 cmds, 2 pipes)
       → executePipelineWithIO:
         goroutine 1: cat file      (stdin → pipe1)
         goroutine 2: grep pattern  (pipe1 → pipe2)
         goroutine 3: wc -l         (pipe2 → stdout)
       Status: wc's
```

### Skip Propagation

```
Input: false && echo a && echo b
Flow:
1. false → status 1
2. && sees 1 → SKIP echo a, status stays 1
3. && sees 1 → SKIP echo b, status stays 1
Output: (none). Final status: 1
```

### Failure Recovery

```
Input: nosuchcmd || echo fallback
Flow:
1. Spawn fails → stderr "nosuchcmd: command not found", status 127
2. || sees 127 → run echo fallback → status 0
Output: fallback. Final status: 0
```

### Mixed Operators

```
Input: cmd1 | cmd2 && cmd3 || cmd4 ; cmd5
Flow:
1. Execute pipeline cmd1 | cmd2 → status1 (from cmd2)
2. If status1 == 0: execute cmd3 → status2
   Else: status2 = status1, skip cmd3
3. If status2 != 0: execute cmd4 → status3
   Else: status3 = status2, skip cmd4
4. Execute cmd5 (unconditional) → final status
```

### Command Substitution

```
Input: echo $(whoami)
Flow:
1. Parser expands the token: Executor.Execute("whoami")
2. Recursive parse + in-process execution; stdout captured: "user\n"
3. Trailing newline trimmed → token becomes "user"; status discarded
4. echo runs with argument "user"
```

### Whole-Input Script

```
Script file:
    #!/usr/bin/env fsh
    export GREETING=hello
    echo $GREETING ; false
    echo done

Flow:
1. RunScript reads the file in full; lexer drops the shebang comment,
   newlines become separators
2. ONE chain executes: export → echo hello → false → echo done
3. Shell exits with echo's status (0)
```

## Testing Considerations

### Shell Testing

- Use `NewWithIO` with buffers for controlled I/O
- Use `os.Pipe()` for stdin when testing interactive mode (readline requires a ReadCloser)
- Whole-input mode: multi-line stdin executes as one parse; a late parse error must prevent ALL execution; `$(cat)` must see EOF

### Chain Testing

- Skip propagation: `false && a && b` (nothing runs, status 1), `true || a || b` (nothing runs, status 0), `false && a || c` (c runs)
- Failure recovery: `nosuchcmd || fallback`, `nosuchcmd ; next`, `cat < /missing || recovered`
- Early-exit pipelines: `yes | head -1` terminates; producer failure is silent

### Command Testing

- Exit codes: 127 (not found), 126 (not executable), 128+N (signal death), 130 (interrupt), pass-through 1–255
- Builtins: each builtin's status and message table; `exit` sentinel at top level / in pipelines / in substitutions; `export` continue-on-invalid; assignment recognition incl. quoted and multi-word non-assignments
- Redirection failures: message format, status 1, chain continues

### Signal Testing

- SIGINT during the SECOND command of a session must still reach the child (no disposition decay)
- Shell survives SIGINT while a child runs; records 130
- Context cancellation reports 130

## Resource Management

### File Handle Cleanup

```go
var filesToClose []*os.File
defer func() {
    for _, f := range filesToClose {
        f.Close()
    }
}()
```

All redirection file handles close after the command completes, regardless of success, failure, or interrupt (redirection.md §12.2).

### Pipe Cleanup

Each pipeline goroutine closes BOTH of its pipe ends when its command finishes — the stdout write end (EOF downstream) and the stdin read end (terminates upstream producers, §Early Exit Terminates Producers).

### Readline Cleanup

```go
rl, err := readline.NewEx(cfg)
defer rl.Close()
```

The `exit` sentinel (never `os.Exit`) guarantees these cleanups run (§exit).
