---
title: Execution Specification
description: Command execution semantics - run modes, chain and pipeline execution, builtin commands, exit codes, and signal handling.
recommend_after: redirection.md
---

# Execution Specification

> **Canonical for:** command execution — run modes, chain and pipeline execution, builtin commands, exit codes, signals, and runtime error reporting. Operator SEMANTICS are canonical in operators.md, redirection in redirection.md, expansion in expansion.md. The authority map lives in the README.

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

### Shell State

A shell instance carries exactly this state:

| Field | Meaning |
|-------|---------|
| stdin, stdout, stderr | The three streams the shell reads from and writes to. They MUST be substitutable — the shell's behavior may not depend on any of them being a terminal, except where this file says "interactive" |
| interactive | Whether the shell runs in interactive mode (§Run Modes) |
| last exit status | The value behind `$?` and the prompt color (§Last Exit Code) |
| substitution executor | The component that runs command-substitution bodies (§Command Substitution Executor, expansion.md §Executor Interface) |

"Subshell" is deliberately NOT the name of the substitution executor: the term is reserved for the future `()` grouping construct.

### Run Modes

#### Interactive Mode

Active when the shell is interactive.

- Reads ONE LINE at a time through the line editor, with syntax highlighting and a colored prompt
- **Each line is a separate parse and a separate execution.** `$?` therefore reflects the previous line (expansion.md §Special Parameters)
- Empty lines are silently skipped
- Handles Ctrl+C (clear line, new prompt) and Ctrl+D (exit)

Required features. The shell prints a welcome message on start. The prompt is a green `$` after a success and a red `$` after a failure. The line under edit is highlighted as it is typed (highlighting.md).

#### Non-Interactive Mode (Whole-Input)

Active when the shell is not interactive (piped stdin, no TTY).

The shell reads **ALL of stdin to EOF**, parses the entire text as **ONE input**, and executes the resulting single chain in order. Within that input, unquoted newlines separate commands (lexer.md §3.4) and `#` comments — including a shebang line — are removed by the lexer (lexer.md §8).

Consequences — each of these is intended, specified behavior:

1. **Commands inherit the shell's stdin, which is at EOF.** The shell consumed the pipe before any command ran. A command that reads stdin therefore sees an immediate EOF. It can never steal script text:

   ```
   $ printf 'echo got:$(cat)\n' | fsh
   got:
   ```

   (POSIX shells share the input stream with commands line-by-line. This is a documented deviation.)
2. **A parse error anywhere rejects the whole input.** An unclosed quote on the last line means NOTHING executes: the shell prints the diagnostic and exits with status 1. In exchange, quoted strings and substitution bodies may span lines.
3. **`exit` stops the sequence** (§exit): the remaining commands do not run and the shell exits with the given status.
4. **`$?` sees the pre-input status.** The whole input is one parse, so every `$?` in it expands before anything runs (expansion.md §Special Parameters).

The shell's exit status is the chain's final status (or the `exit` status, or 1 after a parse error).

#### Script Execution

Script mode takes a filename. It opens that file, reads it **in full**, and executes it exactly like non-interactive whole-input mode. Two notes:

- The script file is NOT the shell's stdin. Commands inherit the stdin the shell itself was started with (e.g. the terminal), so `fsh script.sh < data.txt` lets commands in the script read `data.txt`
- The shebang line is an ordinary `#` comment (lexer.md §8). No special first-line handling exists or is needed

If the file cannot be opened: `cannot open script file <name>: <os reason>` on stderr, exit status 1.

#### Single Command Execution

The shell also accepts one input as a string. It parses that string as ONE input with the same whole-input semantics (newlines inside the string separate commands) and executes it. This is the mode a non-interactive one-shot invocation uses, which joins its arguments into the command string.

## Chain Execution

### Chain Structure

A chain is the parser's output (parser.md). It holds an ordered list of commands and an ordered list of the operators between them. The invariant is that a chain of N commands carries exactly N−1 operators.

### Chain Execution Interface

Chain execution takes a chain, the three streams, and a cancellation signal. It yields two results:

1. An exit status. This is the ordinary outcome, including every kind of command failure.
2. An internal-failure indication. This is reserved for a broken invariant, such as an absent chain, or for a failure of the I/O plumbing itself.

Ordinary command failures — command not found, redirection open failure, non-zero exits — are never chain-fatal. Each one is an exit status (see §Runtime Failures Never Abort the Chain).

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

Skip propagation means `false && a && b` runs NOTHING and yields 1, and `true || a || b` runs nothing and yields 0. Operator semantics — including the normative skip-propagation algorithm — are canonical in operators.md (§Execution Algorithm). This file defers to it.

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

The substitution executor carries the shell's stdin, its stderr, and the cancellation signal. It takes one command body as a string and yields three results: the captured output, an exit status, and an error indication.

It is the component the expansion layer calls (expansion.md §Executor Interface). Behavior:

1. **Recursive parse**: the body is parsed with the SAME executor, so substitutions nested inside the body expand through recursion (expansion.md §Recursive Execution)
2. **Same process**: the body's chain runs in-process, through the usual chain executor. A buffer captures its stdout. Its stderr goes to the shell's stderr, and its stdin is the shell's stdin
3. **Return values**: captured stdout and the chain's final status. The CALLER (the expansion layer) discards the status (expansion.md §Failure Semantics)
4. **Errors**: the error indication is set only when the body fails to PARSE (surfaced as `parse error: command substitution error: <err>`) or on internal failure. A runtime command failure in the body prints to stderr and sets NO error indication. The substitution then yields whatever stdout the buffer holds, and the outer line continues
5. **State mutations are visible**: builtins and assignments in the body mutate the shell (§Builtin Commands). An `exit` in the body stops only the body's own sequence (§exit)

Benefits of the same-process model: access to the same environment, no shell-spawning overhead, consistent behavior with the main shell.

## Pipeline Execution

Commands connected by `|` are grouped and executed as one pipeline segment:

```
cmd1 | cmd2 | cmd3 && cmd4 | cmd5
└───────────────┘    └──────────┘
   Segment 1           Segment 2
```

Pipeline execution takes the segment's commands plus the three streams the segment inherits.

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

Closing the read end in step 5 is REQUIRED. A consumer can exit without draining its input. The producer's next write then fails, which is the in-process equivalent of EPIPE and SIGPIPE. That write failure **terminates the producer silently**. It is not an error, it prints no message, and it cannot change the pipeline's status. The rightmost command has no stdout pipe. It can therefore never be the command terminated this way.

```bash
yes | head -1        # prints y, exits immediately, status 0 - MUST NOT hang
seq 1 100000 | head -2   # prints 1 2, exits, status 0
echo x | pwd         # pwd never reads stdin - MUST NOT hang
```

The same rule covers a pipeline command whose stdin is redirected from a file. Its incoming pipe stays unconnected. Its read end closes at wiring time, so the upstream producer terminates rather than blocks (redirection.md §7.4).

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

### Concurrent Writes to Stderr

The pipeline's commands run at the same time and share one stderr. That stderr MUST serialize each write, so no two error messages interleave.

## Command Execution

### Command Structure

A command is the parser's per-command record (parser.md). It holds the expanded argument words. It also holds the redirection fields: an input file, an output file, and an error file. Each output field carries a flag that says whether the file is appended to or truncated.

### Command Execution Interface

Command execution takes one command, the three streams, and the cancellation signal. It yields an exit status and an internal-failure indication.

### Execution Flow

```
1. Validate: the command exists and has at least one argument word
2. Open redirections in order: InputFile, OutputFile, ErrorFile
   (redirection.md §5.2). On open failure: print the canonical message
   (redirection.md §9) to the command's stderr, return status 1 —
   the command does NOT execute
3. Track opened files, so step 5 can close every one of them
4. Standalone assignment? → perform it (§Standalone Assignment) Builtin?              → run it in-process (§Builtin Commands) Otherwise             → run it as an external command (§External Command Execution)
5. Close the tracked files and return the exit status
```

### External Command Execution

The shell resolves the command name through `$PATH`, starts it with the command's three streams, and waits for it. The outcome maps to a status by the FIRST matching rule in this order:

| Order | Outcome | Message to stderr | Status |
|-------|---------|-------------------|--------|
| 1 | The child ran and exited normally | (none from the shell) | The child's own status, 0–255 |
| 2 | Execution was cancelled (§Signals and Cancellation) | (none) | 130 |
| 3 | The command name resolves to nothing | `<name>: command not found` | 127 |
| 4 | The name resolves but cannot be executed — permission denied, a directory, or an unrecognized executable format | `<name>: <os reason>` | 126 |
| 5 | The child was killed by signal N | (none from the shell) | 128+N |
| 6 | The child's write failed because its consumer had exited (§Early Exit Terminates Producers) | (none) | 0 |
| 7 | Any other start-up or plumbing failure | `<name>: <os reason>` | 1 |

Two orderings in that table are normative, not incidental:

- The spawn failures (rows 3 and 4) are classified BEFORE any exit-status reading. A shell that cannot start a command has no exit status to read.
- The signal case (row 5) is classified BEFORE the normal-exit case (row 1). A child killed by signal N reports 128+N. It never reports the placeholder value a wait-status reader may give for a signal death.

`<os reason>` is the bare operating-system error text — `permission denied`, `is a directory`, `exec format error`. It carries no wrapper and no path prefix.

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

A command word is a builtin exactly when it equals one of the names listed above. Standalone assignment is not name-based (§Standalone Assignment).

### Execution Interface

Builtin execution takes the name, the argument words, and the streams the command runs on. It yields the builtin's exit status directly. Its error indication is reserved for the `exit` sentinel and for internal failures. Ordinary builtin failures, such as a missing directory or an invalid export name, are NOT errors. The builtin prints its own message to stderr and returns a non-zero status. The chain then continues under the usual operator logic.

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

`cd` does NOT maintain the `PWD` environment variable — `PWD` keeps whatever value the environment had at shell startup. Use `pwd` (or `$(pwd)`) for the current directory. The operating system resolves symlinks (physical behavior, with no `-L` or `-P` option).

### pwd

```
pwd
```

Prints the current working directory followed by a newline to stdout, with status 0. Arguments are ignored. If the working directory cannot be determined: `pwd: <os reason>` to stderr, status 1.

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

**Sentinel semantics.** `exit` NEVER terminates the process from inside the builtin. A direct process exit there bypasses output capture, the closing of redirection files, and the teardown of the line editor. The builtin returns its code together with an `exit` sentinel instead. Every enclosing layer can see that sentinel and act on it.

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

Writes the ANSI clear-screen sequence `\033[2J\033[H` (clear screen, cursor home) to stdout, with status 0. Arguments are ignored.

### help

```
help
```

Prints a command overview to stdout, with status 0. The text and the coloring are not normative. The content MUST:

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
2. No part of that word's token was single-quoted (`WasSingleQuoted == false`)
3. The word's EXPANDED value matches `NAME=VALUE`, where `NAME` is a valid variable name and `VALUE` (everything after the first `=`) may be empty

Effect: the environment variable is set. Status 0, with no output.

```bash
A=hello          # sets A=hello, status 0
                 # ...but $A LATER IN THE SAME INPUT is a parse error:
                 # the input already expanded (parser.md §Assignment Then
                 # Use Is Guarded)
A=               # sets A to the empty string
A=$B             # sets A to B's value (expansion ran first)
A=B=C            # sets A to "B=C" (first = splits)
A="B"            # sets A=B (double quotes do not block assignment)
"A=B"            # sets A=B (double quotes do not block assignment)
A="hello world"  # sets A to "hello world" (quotes make it one word)
'A=B'            # NOT an assignment (single-quoted): runs a command named A=B -> 127
A='B'            # NOT an assignment (whole-token WasSingleQuoted - see below)
A=x cmd          # NOT an assignment (two words): runs a command named A=x -> 127
1X=y             # NOT an assignment (invalid name): runs a command named 1X=y -> 127
```

Notes — all deliberate:

- **Whole-token granularity**: the `WasSingleQuoted` flag covers the whole token (lexer.md §4.4.2). A single-quoted part ANYWHERE in the word — `'A=B'`, `A='B'`, `'A'=B` — disqualifies the whole word from assignment recognition
- **Recognition uses the expanded value**: an unquoted `$X` whose value is `A=B` IS an assignment. POSIX shells recognize assignments before expansion, so this is a documented deviation
- **Prefix assignments are NOT supported**: `VAR=x cmd` does not set `VAR` for `cmd`. It runs a command literally named `VAR=x`, typically 127. Per-command environment prefixes are a possible FUTURE feature
- **Redirections on an assignment** are opened and closed with their usual side effects, such as creation and truncation. The assignment itself prints nothing

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

The shell keeps one last-exit-status register (§Shell State). That register is THE value behind `$?` (expansion.md §Special Parameters) and the prompt color. It starts at 0 and is updated after each command line:

| Event | New value |
|-------|-----------|
| Command line completes | The chain's final status |
| `exit` at top level | The exit code (the shell then terminates) |
| Parse error | 1 |
| Command interrupted | 130 (or 128+N for other signals) |
| Command substitution completes | UNCHANGED — substitution statuses are discarded (expansion.md) |

## Signals and Cancellation

### While a Child Runs

While a foreground child runs, SIGINT and SIGQUIT delivered to the shell are **forwarded to the child**. The shell itself does not terminate.

- The child dies, or it handles the signal. A signal death reports 128+N, and Ctrl+C is 130
- The shell records the status and continues. In interactive mode it shows a new prompt. Operator logic treats 130 like any other failure (`&&` skips, `||` runs)
- This holds for EVERY command in the session, not just the first — the shell's signal disposition must not decay after a command completes

### At the Prompt (Interactive)

| Input | Behavior |
|-------|----------|
| Empty line | Skip, show new prompt |
| Ctrl+C | Clear the current line, show new prompt (shell does not exit) |
| Ctrl+D | Exit the shell |
| Valid command | Parse and execute |

### Cancellation

Every execution entry point accepts a cancellation signal. Cancellation is treated as an interrupt. The running child is killed and the command reports **130** (§External Command Execution).

## Error Handling

### Parse Errors

When a parse fails, the shell reports it by the FIRST matching rule:

| Case | Report | Last status |
|------|--------|-------------|
| The input is empty | Nothing. In interactive mode the shell shows a new prompt | Unchanged |
| The analyzer also rejects the input | The caret diagnostic for the analyzer's errors (diagnostics.md) | 1 |
| The analyzer accepts the input, but the parser rejects it | The parser's canonical string (diagnostics.md §5.2), prefixed with `parse error: ` | 1 |

The middle case is the normal one. The analyzer detects every quote-state and structural problem the parser rejects on (highlighting.md §7.1). In whole-input mode a parse error rejects the entire input (§Non-Interactive Mode).

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

An internal-failure indication from chain execution means a broken invariant or a failure of the I/O plumbing. It never means a command failed. The shell reports it as `execution error: <err>`. In normal operation this path is never taken.

### Named Error Conditions

Two failures are named because other files refer to them:

| Name | Meaning |
|------|---------|
| empty command | A command with no words at all, rejected by the parser (diagnostics.md §5.2) |
| `exit` sentinel | The value `exit` returns instead of ending the process (§exit) |

## Prompt System

The interactive prompt is `$ ` followed by one space, wrapped in a color escape:

| Last exit status | Prompt |
|------------------|--------|
| 0 | `\033[32m$ \033[0m` (green) |
| Non-zero | `\033[31m$ \033[0m` (red) |

The reset escape at the end is required. Without it, the color leaks into the text the user types.

## Interactive Features

The interactive line editor MUST provide:

- The prompt above, recomputed before each line, so its color tracks the last status
- Live syntax highlighting of the line being edited. The highlighter takes the raw line and returns the same text with color escapes applied (highlighting.md)
- `^C` echoed when the user interrupts a line, and `exit` echoed when the user ends input with Ctrl+D
- Output on the shell's own stdout and stderr, not on a separate channel

## Examples

### Simple Command

```
Input: ls -la Flow:  Shell → Parser → Chain(1 cmd) → Command → external execution of ls with [-la]
```

### Pipeline

```
Input: cat file | grep pattern | wc -l
Flow:  Shell → Parser → Chain(3 cmds, 2 pipes)
       → one pipeline segment, all members running at once: cat file      (stdin → pipe1) grep pattern  (pipe1 → pipe2) wc -l         (pipe2 → stdout)
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
1. Parser expands the token: the substitution executor runs "whoami"
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
1. Script mode reads the file in full. The lexer drops the shebang comment, and newlines become separators
2. ONE chain executes: export → echo hello → false → echo done
3. Shell exits with echo's status (0)
```

## Conformance Cases

These are the behaviors a conformance suite MUST cover. They are stated as required outcomes, not as a test design.

### Run Modes

- Substituted streams: the shell behaves identically when its streams are not a terminal (§Shell State)
- Whole-input mode: multi-line stdin executes as one parse, a late parse error prevents ALL execution, and `$(cat)` sees EOF

### Chain Behavior

- Skip propagation: `false && a && b` (nothing runs, status 1), `true || a || b` (nothing runs, status 0), `false && a || c` (c runs)
- Failure recovery: `nosuchcmd || fallback`, `nosuchcmd ; next`, `cat < /missing || recovered`
- Early-exit pipelines: `yes | head -1` terminates, and the producer failure stays silent

### Command Behavior

- Exit codes: 127 (not found), 126 (not executable), 128+N (signal death), 130 (interrupt), pass-through 1–255
- Builtins: each builtin's status and message table
- The `exit` sentinel at top level, in a pipeline, and in a substitution
- The way `export` continues past an invalid name
- Assignment recognition, including the quoted and multi-word non-assignments
- Redirection failures: message format, status 1, chain continues

### Signal Behavior

- SIGINT during the SECOND command of a session must still reach the child (no disposition decay)
- The shell survives SIGINT while a child runs, and records 130
- Cancellation reports 130

## Resource Management

### File Handles

Every redirection file opened for a command closes after that command completes. This holds on success, on failure, and on interrupt alike (redirection.md §12.2).

### Pipes

Each pipeline member closes BOTH of its pipe ends when its command finishes. Closing the stdout write end gives the downstream member EOF. Closing the stdin read end terminates upstream producers (§Early Exit Terminates Producers).

### The Line Editor

The interactive line editor is torn down before the shell exits. The terminal is left in its original mode.

Every cleanup above depends on `exit` raising its sentinel rather than ending the process on the spot (§exit).
