---
title: I/O Redirection Specification
description: Input/output redirection operators, file descriptor handling, and redirection ordering.
recommend_after: operators.md
---

# Foundation Shell I/O Redirection Specification

> **Canonical for:** I/O redirection — operators, targets and their expansion, file semantics, redirection error handling and messages. Redirection *tokenization* is specified in lexer.md §3.3; the expansion pipeline in expansion.md. The authority map lives in the README.

## 1. Overview

I/O redirection allows commands to read input from files instead of stdin and write output to files instead of stdout or stderr. Foundation Shell supports standard UNIX-style redirection operators for input, output, and error streams.

Redirection is processed at parse time and applied at execution time. Each command in a chain maintains its own independent set of redirections.

### 1.1 Design Principles

1. **Simplicity**: Support the most common redirection patterns from POSIX shells
2. **Predictability**: Redirections are applied in a consistent, well-defined order
3. **Compatibility**: Syntax matches traditional shell conventions

### 1.2 Scope

This specification covers:
- File descriptor redirection (stdin, stdout, stderr)
- File creation and truncation semantics
- Interaction with pipes
- Expansion in redirection targets
- Error handling

This specification does NOT cover (unsupported features):
- Here-documents (`<<`) and here-strings (`<<<`) — attempts are rejected with a dedicated parse error (§9.6); a FUTURE feature (§15)
- File descriptor duplication (`2>&1`, `>&2`) — attempts are rejected with a dedicated parse error (§9.5); a FUTURE feature (§15)
- Process substitution (`<(...)`, `>(...)`)
- `/dev/null` or other special files (handled by the OS, not the shell)

---

## 2. Standard File Descriptors

Foundation Shell recognizes three standard file descriptors:

| File Descriptor | Name   | Default Binding | Description                        |
|-----------------|--------|-----------------|-----------------------------------|
| 0               | stdin  | Terminal input  | Standard input stream             |
| 1               | stdout | Terminal output | Standard output stream            |
| 2               | stderr | Terminal output | Standard error stream             |

### 2.1 File Descriptor 0: stdin

- **Purpose**: Provides input data to commands
- **Default**: Connected to terminal input in interactive mode, or piped input in non-interactive mode
- **Redirection**: Uses the `<` operator

### 2.2 File Descriptor 1: stdout

- **Purpose**: Primary output stream for command results
- **Default**: Connected to terminal output
- **Redirection**: Uses the `>` or `>>` operators

### 2.3 File Descriptor 2: stderr

- **Purpose**: Error messages and diagnostic output
- **Default**: Connected to terminal output (same destination as stdout by default)
- **Redirection**: Uses the `2>` or `2>>` operators

---

## 3. Redirection Operators

### 3.1 Input Redirection: `< file`

**Syntax:**
```
command < filename
```

**Behavior:**
1. Opens `filename` for reading
2. Replaces stdin (fd 0) with the opened file
3. Command reads from the file instead of terminal/pipe input

**Example:**
```bash
cat < input.txt
wc -l < data.csv
```

**Details:**
- The file is opened read-only
- The file must exist and be readable
- File is opened before command execution begins
- File is closed after command completes

### 3.2 Output Redirection (Truncate): `> file`

**Syntax:**
```
command > filename
```

**Behavior:**
1. Opens or creates `filename` for writing
2. **Truncates** the file to zero length if it exists
3. Replaces stdout (fd 1) with the opened file
4. Command writes to the file instead of terminal

**Example:**
```bash
echo "hello" > output.txt
ls -la > directory_listing.txt
```

**Details:**
- The file is opened write-only. It is created if it is absent, and truncated if it is present
- A created file is requested with mode `0644` (§8.2)
- The previous contents of an existing file are destroyed

### 3.3 Output Redirection (Append): `>> file`

**Syntax:**
```
command >> filename
```

**Behavior:**
1. Opens or creates `filename` for writing
2. **Appends** to existing content (does not truncate)
3. Replaces stdout (fd 1) with the opened file
4. New output is written after existing content

**Example:**
```bash
echo "line 1" > log.txt
echo "line 2" >> log.txt
# log.txt now contains:
# line 1
# line 2
```

**Details:**
- The file is opened write-only in append mode. It is created if it is absent
- A created file is requested with mode `0644` (§8.2)
- Each write goes to the end of the file

### 3.4 Error Redirection (Truncate): `2> file`

**Syntax:**
```
command 2> filename
```

**Behavior:**
1. Opens or creates `filename` for writing
2. **Truncates** the file to zero length if it exists
3. Replaces stderr (fd 2) with the opened file
4. Error messages are written to the file instead of terminal

**Example:**
```bash
ls /nonexistent 2> errors.txt
make 2> build_errors.log
```

**Details:**
- The file is opened write-only. It is created if it is absent, and truncated if it is present
- A created file is requested with mode `0644` (§8.2)
- Whitespace MUST NOT separate the `2` from the `>`. The lexer reads `2>` as one token

### 3.5 Error Redirection (Append): `2>> file`

**Syntax:**
```
command 2>> filename
```

**Behavior:**
1. Opens or creates `filename` for writing
2. **Appends** to existing content (does not truncate)
3. Replaces stderr (fd 2) with the opened file
4. Error messages are appended after existing content

**Example:**
```bash
command1 2>> errors.log
command2 2>> errors.log
# All errors accumulated in errors.log
```

**Details:**
- The file is opened write-only in append mode. It is created if it is absent
- A created file is requested with mode `0644` (§8.2)
- Whitespace MUST NOT separate the `2` from the `>>`. The lexer reads `2>>` as one token

---

## 4. Operator Summary Table

| Operator | File Descriptor | Mode     | Creates File | Truncates |
|----------|-----------------|----------|--------------|-----------|
| `<`      | 0 (stdin)       | Read     | No           | N/A       |
| `>`      | 1 (stdout)      | Write    | Yes          | Yes       |
| `>>`     | 1 (stdout)      | Append   | Yes          | No        |
| `2>`     | 2 (stderr)      | Write    | Yes          | Yes       |
| `2>>`    | 2 (stderr)      | Append   | Yes          | No        |

---

## 5. Order of Redirections

### 5.1 Position Independence

Redirections may appear **anywhere** within a command line. The following are all equivalent:

```bash
cat < input.txt > output.txt
< input.txt cat > output.txt
< input.txt > output.txt cat
cat > output.txt < input.txt
```

**Parsing Behavior:**
- Redirection operators and their targets are extracted during parsing
- Remaining tokens form the command name and arguments
- The order of redirections in the command line does not affect processing order

### 5.2 Processing Order

All redirections for a single command are processed in a defined order during execution:

1. **Input redirection** (`<`) is processed first
2. **Output redirection** (`>` or `>>`) is processed second
3. **Error redirection** (`2>` or `2>>`) is processed third

This order ensures that:
- Input files are available before command execution
- Output destinations are established before any output is generated

### 5.3 Last-Wins Semantics (Parse-Time)

If the same file descriptor is redirected multiple times, the **last** redirection wins — at PARSE time. Earlier targets are discarded before execution begins, so they are never opened:

```bash
echo test > file1.txt > file2.txt
# Only file2.txt receives output; file1.txt is NEVER created
```

The parser stores a single target per redirection type (`InputFile`, `OutputFile`, `ErrorFile`); a later redirection to the same descriptor overwrites the previous value. This is a documented deviation from POSIX shells, which open every redirection in order (bash creates — and truncates — `file1.txt`).

---

## 6. Multiple Redirections on Same Command

A single command can have any combination of input, output, and error redirections:

```bash
# All three redirections
command < input.txt > output.txt 2> error.txt

# Input and output only
sort < unsorted.txt > sorted.txt

# Output and error only
make > build.log 2> errors.log

# Same file for output and error (both write to same file)
command > combined.txt 2> combined.txt
```

### 6.1 Independent File Handles

Each redirection opens its own independent file handle. If stdout and stderr are redirected to the same file, two separate file handles are opened:

```bash
command > log.txt 2> log.txt
```

**Behavior:**
- Two file handles are opened to `log.txt`
- Both handles are opened with truncate mode
- Output may be interleaved unpredictably
- The second open operation truncates what the first may have written

**Recommendation:** To capture both stdout and stderr to the same file, use file descriptor duplication (not yet supported in Foundation Shell).

---

## 7. Redirection with Pipes

### 7.1 Pipe Precedence for stdout

When a command is part of a pipeline, the **pipe takes precedence** over stdout redirection for connecting commands:

```bash
# Pipe connects cmd1's stdout to cmd2's stdin
cmd1 | cmd2
```

However, explicit output redirection **overrides** the pipe connection:

```bash
# cmd1's stdout goes to file.txt, NOT to cmd2
# cmd2 receives empty input
cmd1 > file.txt | cmd2
```

### 7.2 Stderr Independent of Pipes

Stderr is **not affected** by pipe operators. You can redirect stderr while piping stdout:

```bash
# stdout goes through pipe, stderr goes to file
cmd1 2> errors.txt | cmd2

# Both commands can have independent stderr redirections
cmd1 2> errors1.txt | cmd2 2> errors2.txt
```

### 7.3 Pipeline with Final Output Redirection

The last command in a pipeline can redirect its output to a file:

```bash
# stdout flows through pipeline, final output goes to file
cat input.txt | grep pattern | sort > result.txt

# Can also redirect stderr on any pipeline stage
cat input.txt | grep pattern 2> grep_errors.txt | sort > result.txt 2> sort_errors.txt
```

### 7.4 Input Redirection in Pipelines

The first command in a pipeline can use input redirection:

```bash
# First command reads from file, output flows through pipeline
< data.txt grep pattern | sort | uniq
```

Middle or end commands in a pipeline that use input redirection have their stdin replaced, disconnecting them from the pipe:

```bash
# WARNING: grep ignores pipe input, reads from file instead
cmd1 | grep < search_terms.txt pattern
```

The disconnected pipe's read end is closed immediately at wiring time, so the upstream producer terminates instead of blocking forever on a pipe nobody reads (execution.md §Early Exit Terminates Producers). The pipeline always completes.

---

## 8. File Creation

### 8.1 Automatic File Creation

Output redirection operators (`>`, `>>`, `2>`, `2>>`) automatically create the target file if it does not exist:

```bash
# Creates new_file.txt if it doesn't exist
echo "content" > new_file.txt
```

### 8.2 File Permissions

Newly created files are assigned permissions `0644`:

| Permission | Meaning                            |
|------------|-----------------------------------|
| Owner      | Read + Write (6)                  |
| Group      | Read only (4)                     |
| Others     | Read only (4)                     |

A file the shell creates is therefore requested with mode `0644`. The process file-creation mask applies to it as usual.

### 8.3 Parent Directory Requirements

The parent directory must exist. Foundation Shell does NOT create intermediate directories:

```bash
# ERROR if /path/to directory doesn't exist
echo "test" > /path/to/newdir/file.txt
```

### 8.4 Existing Files

- **Truncate mode** (`>`, `2>`): Existing file contents are destroyed
- **Append mode** (`>>`, `2>>`): Existing file contents are preserved

---

## 9. Error Handling

Redirection produces two classes of errors: PARSE-time errors (§9.3–§9.5), which reject the whole input before anything runs, and RUNTIME open failures (§9.1–§9.2), which fail only the affected command — the rest of the chain continues per operator logic (execution.md §Runtime Failures Never Abort the Chain).

### 9.1 Runtime Open Failures

**Condition:** A redirection target fails to open at execution time — input file missing, permission denied, unwritable path, missing parent directory.

**Behavior:**
- The affected command does NOT execute
- The command's exit status is 1, which feeds operator evaluation; the chain CONTINUES (`cat < /missing || echo recovered` prints `recovered`)
- The message is printed to stderr in EXACTLY this format — no wrapper prefix (no `execution error:`), and the filename appears exactly once (the OS reason is unwrapped; no doubled `open <file>:` prefix):

```
cannot open input file <filename>: <os reason>
cannot open output file <filename>: <os reason>
```

`input` is used for `<`; `output` for `>`, `>>`, `2>`, and `2>>`.

**Example transcript:**
```bash
$ cat < nonexistent.txt
cannot open input file nonexistent.txt: no such file or directory
$ cat < nonexistent.txt || echo recovered
cannot open input file nonexistent.txt: no such file or directory
recovered
```

### 9.2 Permission Denied

Permission failures are runtime open failures (§9.1) with the OS reason `permission denied`:

```bash
$ cat < /etc/shadow
cannot open input file /etc/shadow: permission denied
$ echo x > /etc/readonly.conf
cannot open output file /etc/readonly.conf: permission denied
```

### 9.3 Missing Target After Operator

**Condition:** A redirection operator appears without a following filename token.

**Syntax Errors:**
```bash
command >           # End of input after operator
command > |         # Operator instead of filename
command > &&        # Operator instead of filename
```

**Behavior:**
- Parse error (canonical strings in diagnostics.md §5.2): `missing redirection target: >` — or, when an operator follows, `missing redirection target: > followed by operator |`
- No partial execution occurs

### 9.4 Empty Target After Expansion

**Condition:** A redirection target token exists but expands to the EMPTY string (e.g. an unset variable, or a substitution with empty output).

**Behavior:**
- Parse error: `empty redirection target`
- Nothing on the line executes; the shell records status 1 (as for any parse error)
- The redirection is NOT silently dropped — a redirection that was written must never vanish

```bash
$ echo hi > $UNSET_VAR
empty redirection target
```

(POSIX-shell counterpart: bash's `ambiguous redirect`.)

### 9.5 File Descriptor Duplication Is Guarded

**Condition:** An UNQUOTED target word begins with `&`.

File descriptor duplication (`2>&1`, `>&2`) is a documented FUTURE feature (§15). Today `2>&1` lexes as the operator `2>` followed by the word `&1` (lexer.md §3.3), which would silently create a file named `&1`. To prevent that trap, the parser rejects it:

**Behavior:**
- Parse error: `file descriptor duplication is not supported: <word>`
- The check inspects the target token AS WRITTEN (before expansion) and applies only when the token is unquoted (`WasQuoted == false`)
- A QUOTED target is a legitimate filename: `echo x > '&1'` creates a file named `&1`

```bash
$ echo hi 2>&1
file descriptor duplication is not supported: &1
$ echo hi > '&1'      # OK: quoted - writes a file literally named &1
```

### 9.6 Here-Documents Are Guarded

**Condition:** A `<` operator is immediately followed by another `<` operator.

Here-documents (`<<`) and here-strings (`<<<`) are documented FUTURE features (§15). The lexer has no `<<` operator, so `cat <<EOF` lexes as `<`, `<`, `EOF` (lexer.md §3.3). Reported by §9.3's rule that would be `missing redirection target: < followed by operator <`, which describes the token stream accurately and the reader's actual mistake not at all: it sends them looking for a filename, when the real answer is that the construct does not exist here. The parser names it instead:

**Behavior:**
- Parse error: `here-documents are not supported`
- `<<<` is three `<`, forming two adjacent pairs. It reports ONCE — a doubled diagnostic for one construct teaches the reader to skim the output
- A single `<` is unaffected: `cat < in.txt` is an ordinary input redirection

```bash
$ cat <<EOF
here-documents are not supported
$ cat <<<word
here-documents are not supported
```

### 9.7 Error Message Output

Runtime redirection errors are written to stderr — the ORIGINAL stderr if the failing redirection is `2>`/`2>>` itself (the error is reported before the redirection would have been applied).

### 9.8 Error Summary

| Error Condition | Class | Exit Status | Message |
|-----------------|-------|-------------|---------|
| Input file not found | Runtime | 1 (command only; chain continues) | `cannot open input file <name>: <os reason>` |
| Permission denied | Runtime | 1 (command only; chain continues) | `cannot open input file <name>: permission denied` / `cannot open output file <name>: permission denied` |
| Cannot create output file | Runtime | 1 (command only; chain continues) | `cannot open output file <name>: <os reason>` |
| Missing redirection target | Parse | line rejected; shell records 1 | `missing redirection target: <op>` |
| Empty target after expansion | Parse | line rejected; shell records 1 | `empty redirection target` |
| Unquoted target starting with `&` | Parse | line rejected; shell records 1 | `file descriptor duplication is not supported: <word>` |
| `<` followed by `<` (here-document) | Parse | line rejected; shell records 1 | `here-documents are not supported` |

---

## 10. Expansion in Redirection Targets

Redirection target filenames undergo the SAME expansion pipeline as command arguments — including command substitution (expansion.md is canonical for the pipeline; this section shows its application to targets).

### 10.1 Variable Expansion

Environment variables are expanded in redirection targets:

```bash
# Using $VAR syntax
echo "log" > $LOGDIR/output.txt

# Using ${VAR} syntax
echo "log" > ${LOGDIR}/output.txt
```

**Behavior:**
- Variables are expanded before file operations
- Undefined variables expand to empty string
- Expansion occurs for both `$VAR` and `${VAR}` syntax

### 10.2 Tilde Expansion

The tilde character (`~`) is expanded to `$HOME` at the start of a path:

```bash
# Expands ~ to home directory
echo "data" > ~/output.txt

# Expands to /home/user/logs/app.log (if HOME=/home/user)
echo "log" > ~/logs/app.log
```

**Tilde Expansion Rules:**
- `~` alone expands to `$HOME`
- `~/path` expands to `$HOME/path`
- `~user` is NOT expanded (user-specific home directories not supported)
- Tilde must be at the start of the token
- Quoting ANY part of the target suppresses tilde expansion (`WasQuoted`, whole-token granularity — expansion.md §Tilde Expansion): `> "~/f.txt"` and `> ~/"f.txt"` both name a literal `~/f.txt`

### 10.3 Single Quotes Prevent Expansion

Single-quoted filenames are NOT expanded:

```bash
# Literal filename "$HOME/file.txt", no expansion
echo "test" > '$HOME/file.txt'

# Literal filename "~/file.txt", no expansion
echo "test" > '~/file.txt'
```

### 10.4 Double Quotes Allow Expansion

Double-quoted filenames allow variable expansion but preserve spaces:

```bash
# Variable is expanded
echo "test" > "$LOGDIR/output.txt"

# Spaces are preserved in filename
echo "test" > "my file.txt"
```

### 10.5 Escaped Characters

Backslash escapes prevent expansion of the following character:

```bash
# Literal $HOME, not expanded
echo "test" > \$HOME/file.txt
```

### 10.6 Command Substitution in Targets

Command substitution runs in redirection targets exactly as in arguments:

```bash
echo "log" > $(date +%F).log     # writes to e.g. 2026-07-15.log
```

### 10.7 Expansion Order

The full pipeline of expansion.md §Expansion Order, applied to the target token:

1. Tilde expansion (token starts with unquoted `~`; skipped when `WasQuoted`)
2. Environment variable expansion (`$VAR`, `${VAR}`, `$?`)
3. Command substitution (`$(...)`, `` `...` ``)
4. Escape marker stripping (unconditional)

A single-quoted target (`WasSingleQuoted`) skips steps 1–3 entirely (§10.3). After expansion, an empty result is a parse error (§9.4).

---

## 11. Internal Representation

### 11.1 Command Structure

Each parsed command carries these fields (parser.md):

| Field | Meaning |
|-------|---------|
| `Args` | The command name and its arguments |
| `InputFile` | The target of a `<` redirection. Empty when there is none |
| `OutputFile` | The target of a `>` or `>>` redirection. Empty when there is none |
| `ErrorFile` | The target of a `2>` or `2>>` redirection. Empty when there is none |
| `AppendOutput` | True for `>>`, false for `>` |
| `AppendError` | True for `2>>`, false for `2>` |

### 11.2 Token Types

Redirection operators are recognized as distinct token types:

| Token Type           | String Representation |
|---------------------|----------------------|
| `RedirectStdIn`     | `<`                  |
| `RedirectStdOut`    | `>`                  |
| `RedirectStdOutAppend` | `>>`              |
| `RedirectStdErr`    | `2>`                 |
| `RedirectStdErrAppend` | `2>>`             |

---

## 12. Execution Semantics

### 12.1 File Opening Sequence

1. Parse command and extract redirections
2. Open input file (if `InputFile` specified)
3. Open output file (if `OutputFile` specified)
4. Open error file (if `ErrorFile` specified)
5. Execute command with redirected I/O
6. Close all opened files (in reverse order)

### 12.2 File Handle Cleanup

Every opened file closes after the command completes. This holds in each case:
- The command succeeded or failed
- A signal interrupted the command
- An internal failure ended the command

The close MUST be unconditional. It cannot depend on the path the command took out of execution.

### 12.3 Builtin Commands

Builtin commands (execution.md §Builtin Commands) respect redirections:

```bash
# pwd output goes to file
pwd > current_dir.txt

# export's environment listing goes to file
export > environment.txt
```

(`echo` is NOT a builtin — it is an external command, which naturally respects redirections too.)

---

## 13. Examples

### 13.1 Basic Redirections

```bash
# Read from file
cat < input.txt

# Write to file (truncate)
ls > listing.txt

# Append to file
echo "new line" >> log.txt

# Redirect errors
make 2> errors.txt

# Append errors
./script.sh 2>> error_log.txt
```

### 13.2 Combined Redirections

```bash
# Input and output
sort < unsorted.txt > sorted.txt

# All three streams
command < input.txt > output.txt 2> errors.txt

# Output and error to same file (separate handles)
command > all.txt 2> all.txt
```

### 13.3 Pipelines with Redirection

```bash
# Basic pipeline
cat file.txt | grep pattern | sort

# Pipeline with final output to file
cat file.txt | grep pattern | sort > results.txt

# Pipeline with stderr redirection at each stage
cmd1 2> err1.txt | cmd2 2> err2.txt | cmd3 > out.txt 2> err3.txt

# Input redirection at start of pipeline
< data.txt grep pattern | sort | uniq > unique.txt
```

### 13.4 Variable Expansion in Targets

```bash
# Environment variable
LOG=/var/log/myapp
echo "starting" > $LOG/startup.log

# Tilde expansion
echo "config" > ~/config.txt

# Combined
echo "user data" > ~/$USER/data.txt
```

---

## 14. Differences from POSIX Shell

Foundation Shell implements a subset of POSIX shell redirection. Notable omissions:

| Feature | POSIX | Foundation Shell |
|---------|-------|------------------|
| `2>&1` (duplicate fd) | Yes | No — guarded parse error (§9.5) |
| `>&2` (stdout to stderr) | Yes | No — guarded parse error (§9.5) |
| `&>` (stdout+stderr) | Bash extension | No |
| `<<` (here-document) | Yes | No — guarded parse error (§9.6) |
| `<<<` (here-string) | Bash extension | No — guarded parse error (§9.6) |
| `<>` (read-write) | Yes | No |
| `n>` (arbitrary fd) | Yes | No (only 1 and 2) |
| `exec` redirections | Yes | No |
| noclobber (`>|`) | Yes | No |

---

## 15. Future Considerations

The following features may be added in future versions:

1. **File descriptor duplication** (`2>&1`, `>&2`)
2. **Here-documents** (`<<EOF`)
3. **Here-strings** (`<<<`)
4. **noclobber mode** (prevent accidental overwrites)
5. **Read-write mode** (`<>`)

---

## Appendix A: Grammar

```
redirection     = input_redir | output_redir | error_redir
input_redir     = '<' WORD
output_redir    = '>' WORD | '>>' WORD
error_redir     = '2>' WORD | '2>>' WORD

WORD            = a value token, non-empty after the full expansion
                  pipeline (§10.7); must not start with unquoted '&' (§9.5)
```
