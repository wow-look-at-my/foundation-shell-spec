---
title: I/O Redirection Specification
description: Input/output redirection operators, file descriptor handling, and redirection ordering.
recommend_after: parser.md
---

# Foundation Shell I/O Redirection Specification

This document is the authoritative specification for I/O redirection in Foundation Shell. All implementation behavior MUST conform to this specification.

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

This specification does NOT cover:
- Here-documents (`<<`)
- Here-strings (`<<<`)
- File descriptor duplication (`2>&1`, `>&2`)
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

**Implementation Details:**
- Uses `os.Open()` with read-only mode
- File must exist and be readable
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

**Implementation Details:**
- Uses `os.OpenFile()` with flags: `O_WRONLY | O_CREATE | O_TRUNC`
- Creates file with permissions `0644` if it does not exist
- Existing file contents are destroyed

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

**Implementation Details:**
- Uses `os.OpenFile()` with flags: `O_WRONLY | O_CREATE | O_APPEND`
- Creates file with permissions `0644` if it does not exist
- File pointer is positioned at end of file before each write

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

**Implementation Details:**
- Uses `os.OpenFile()` with flags: `O_WRONLY | O_CREATE | O_TRUNC`
- Creates file with permissions `0644` if it does not exist
- The `2` and `>` MUST NOT be separated by whitespace (parsed as single token `2>`)

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

**Implementation Details:**
- Uses `os.OpenFile()` with flags: `O_WRONLY | O_CREATE | O_APPEND`
- Creates file with permissions `0644` if it does not exist
- The `2` and `>>` MUST NOT be separated by whitespace (parsed as single token `2>>`)

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

### 5.3 Last-Wins Semantics

If the same file descriptor is redirected multiple times, the **last** redirection wins:

```bash
echo test > file1.txt > file2.txt
# Only file2.txt receives output; file1.txt is created but empty
```

**Implementation Note:** The parser only stores a single target per redirection type (`InputFile`, `OutputFile`, `ErrorFile`). Multiple redirections to the same descriptor simply overwrite the previous value.

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

Middle or end commands in a pipeline that use input redirection will have their stdin replaced, breaking the pipe connection:

```bash
# WARNING: grep ignores pipe input, reads from file instead
cmd1 | grep < search_terms.txt pattern
```

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

**Implementation:**
```go
os.OpenFile(path, flags, 0644)
```

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

### 9.1 Input File Not Found

**Condition:** Input redirection (`<`) references a non-existent file

**Behavior:**
- Command execution fails immediately
- Exit code: 1
- Error message format: `cannot open input file <filename>: <os error>`

**Example:**
```bash
$ cat < nonexistent.txt
cannot open input file nonexistent.txt: no such file or directory
```

### 9.2 Permission Denied

**Condition:** Insufficient permissions to read input file or write/create output file

**Behavior:**
- Command execution fails immediately
- Exit code: 1
- Error message format: `cannot open input file <filename>: permission denied` or `cannot open output file <filename>: permission denied`

**Example:**
```bash
$ cat < /etc/shadow
cannot open input file /etc/shadow: permission denied
```

### 9.3 Missing Target After Operator

**Condition:** Redirection operator appears without a following filename

**Syntax Errors:**
```bash
command >           # Missing target
command > |         # Operator instead of filename
command > &&        # Operator instead of filename
command >           # End of input
```

**Behavior:**
- Parse error (not execution error)
- Error message: `missing redirection target: >`
- No partial execution occurs

### 9.4 Error Message Output

Redirection errors are written to stderr (unless stderr itself is being redirected, in which case the error goes to the original stderr before redirection is applied).

### 9.5 Error Codes

| Error Condition              | Exit Code |
|-----------------------------|-----------|
| Input file not found         | 1         |
| Permission denied            | 1         |
| Cannot create output file    | 1         |
| Missing redirection target   | Parse error (no exit code) |

---

## 10. Expansion in Redirection Targets

Redirection target filenames undergo the same expansion as command arguments.

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

### 10.6 Expansion Order

1. Tilde expansion (if token starts with `~`)
2. Environment variable expansion (`$VAR`, `${VAR}`)
3. Escape marker stripping (internal processing)

---

## 11. Internal Representation

### 11.1 CommandSpec Structure

Each parsed command is represented as a `CommandSpec`:

```go
type CommandSpec struct {
    Args         []string  // Command name and arguments
    InputFile    string    // Target for < redirection (empty if none)
    OutputFile   string    // Target for > or >> redirection (empty if none)
    ErrorFile    string    // Target for 2> or 2>> redirection (empty if none)
    AppendOutput bool      // true for >>, false for >
    AppendError  bool      // true for 2>>, false for 2>
}
```

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

All file handles are closed after command completion, regardless of:
- Command success or failure
- Signal interruption
- Exception/panic

**Implementation uses `defer`:**
```go
defer func() {
    for _, f := range filesToClose {
        f.Close()
    }
}()
```

### 12.3 Builtin Commands

Builtin commands (e.g., `cd`, `pwd`, `echo`, `exit`) respect redirections:

```bash
# pwd output goes to file
pwd > current_dir.txt

# echo output goes to file
echo "hello" > greeting.txt
```

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
| `2>&1` (duplicate fd) | Yes | No |
| `>&2` (stdout to stderr) | Yes | No |
| `&>` (stdout+stderr) | Bash extension | No |
| `<<` (here-document) | Yes | No |
| `<<<` (here-string) | Bash extension | No |
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

WORD            = expanded_token
expanded_token  = (after tilde and variable expansion)
```

## Appendix B: Implementation Files

| File | Purpose |
|------|---------|
| `internal/token/tokentype.go` | Token type definitions for redirection operators |
| `pkg/parser/parser.go` | Parsing logic, CommandSpec construction |
| `internal/command/command.go` | Execution with I/O redirection |
| `internal/expander/expander.go` | Variable and tilde expansion |
| `internal/lexer/lexer.go` | Tokenization of input |

## Appendix C: Test Coverage

Redirection behavior is verified by tests in:
- `pkg/parser/parser_test.go` - Parsing tests
- `internal/command/command_test.go` - Execution tests
- `internal/chain/chain_test.go` - Pipeline integration tests
