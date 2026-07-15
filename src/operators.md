---
title: Operators Specification
description: Control flow operators (pipe, AND, OR, semicolon), precedence rules, and short-circuit evaluation.
recommend_after: parser.md
---

# Operators Specification

> **Canonical for:** chain operator semantics (`|`, `&&`, `||`, `;`), precedence, and operator-related error cases. The authority map lives in the README.

This document provides the exhaustive specification for Foundation Shell's chain operators. (Operator *tokenization* — maximal munch, no whitespace required, quoting/escaping immunity — is specified in lexer.md §3.3.)

## Table of Contents

1. [Overview](#overview)
2. [Pipe Operator](#pipe-operator)
3. [AND Operator](#and-operator)
4. [OR Operator](#or-operator)
5. [Semicolon Operator](#semicolon-operator)
6. [Operator Precedence and Associativity](#operator-precedence-and-associativity)
7. [Combining Operators in Complex Chains](#combining-operators-in-complex-chains)
8. [Error Cases](#error-cases)
9. [Implementation Notes](#implementation-notes)

---

## Overview

Foundation Shell supports four chain operators that control the flow of command execution:

| Operator | Name      | Symbol | Purpose                                          |
|----------|-----------|--------|--------------------------------------------------|
| Pipe     | Pipe      | `\|`   | Connect stdout of left command to stdin of right |
| AND      | Logical AND | `&&`  | Execute right only if left succeeds              |
| OR       | Logical OR  | `\|\|` | Execute right only if left fails                 |
| Semicolon| Separator   | `;`   | Execute right unconditionally after left         |

### Command Chain Structure

A command chain consists of one or more commands connected by operators. For N commands, there are exactly N-1 operators:

```
command_1  operator_1  command_2  operator_2  ...  command_N
```

**Invariant:** `len(operators) == len(commands) - 1`

### Exit Code Definition

- An exit code of `0` indicates **success**
- Any non-zero exit code indicates **failure**
- The exit code range is 0-255 (standard Unix convention; normative table in execution.md §Exit Codes)

A command that fails to START — command not found (127), found but not executable (126), redirection target that fails to open (1) — yields its failure status to operator evaluation like any other non-zero exit. It NEVER aborts the chain (execution.md §Runtime Failures Never Abort the Chain):

```bash
nosuchcmd || echo fallback   # prints fallback; exit status 0
nosuchcmd ; echo next        # prints next
```

---

## Pipe Operator

### Syntax

```
command1 | command2
```

### Semantics

The pipe operator connects the standard output (stdout) of the left command to the standard input (stdin) of the right command.

#### Data Flow

```
command1 stdout ──────> stdin command2
         stderr ──────> (shared stderr)
```

#### Concurrent Execution

Both commands in a pipeline execute **concurrently**. The pipe operator does NOT wait for the left command to complete before starting the right command. This is essential for streaming data between processes.

```
echo "line1\nline2\nline3" | grep "line"
```

In this example:
1. `echo` begins writing to stdout
2. `grep` begins reading from stdin simultaneously
3. Data flows through the pipe as it is produced

#### Exit Code

The exit code of a pipeline is the exit code of the **rightmost command**.

```
# Exit code is the exit code of `grep`
echo test | grep test    # Exit code: 0 (grep found match)
echo test | grep missing # Exit code: 1 (grep found no match)

# Left command failure does NOT affect exit code
false | true             # Exit code: 0 (from `true`)
true | false             # Exit code: 1 (from `false`)
```

#### Multi-Stage Pipelines

Pipelines can chain multiple commands:

```
command1 | command2 | command3 | command4
```

All commands execute concurrently. The exit code is from the rightmost command (`command4`).

#### Standard Error Handling

Standard error (stderr) is NOT piped between commands. Each command in the pipeline writes stderr to the same destination (typically the terminal). Stderr writes from concurrent commands are **serialized** via a synchronized writer to prevent interleaved output.

```
cmd1 2>/dev/null | cmd2  # Only cmd1's stderr is suppressed
```

### Examples

```bash
# Basic pipe
echo "hello world" | tr a-z A-Z
# Output: HELLO WORLD
# Exit code: 0

# Multi-stage pipeline
cat file.txt | grep pattern | sort | uniq
# Exit code: exit code of `uniq`

# Pipeline with failing left command
false | echo "still runs"
# Output: still runs
# Exit code: 0 (from echo)

# Pipeline with failing right command
echo test | grep nonexistent
# Exit code: 1 (from grep, no match)
```

---

## AND Operator

### Syntax

```
command1 && command2
```

### Semantics

The AND operator implements **short-circuit evaluation**. The right command executes **only if** the left command succeeds (exits with code 0).

#### Execution Flow

```
Execute command1
IF exit_code == 0 THEN
    Execute command2
    RETURN exit_code of command2
ELSE
    RETURN exit_code of command1 (non-zero)
END
```

#### Exit Code Propagation

| Left Exit Code | Right Executes? | Chain Exit Code        |
|----------------|-----------------|------------------------|
| 0 (success)    | Yes             | Exit code of right     |
| Non-zero       | No              | Exit code of left      |

The exit code propagates from whichever command was last executed.

### Examples

```bash
# Left succeeds, right runs
true && echo "success"
# Output: success
# Exit code: 0

# Left fails, right is skipped
false && echo "never printed"
# Output: (none)
# Exit code: 1 (from `false`)

# Chained AND
mkdir dir && cd dir && touch file
# All three run only if all previous succeed

# Using AND for conditional execution
test -f config.txt && cat config.txt
# Only cat if file exists
```

### Use Cases

1. **Conditional execution**: Run a command only if a prerequisite succeeds
2. **Guard conditions**: `test -d dir && rm -rf dir`
3. **Sequential dependencies**: Commands that depend on prior success

---

## OR Operator

### Syntax

```
command1 || command2
```

### Semantics

The OR operator implements **short-circuit evaluation**. The right command executes **only if** the left command fails (exits with non-zero code).

#### Execution Flow

```
Execute command1
IF exit_code != 0 THEN
    Execute command2
    RETURN exit_code of command2
ELSE
    RETURN exit_code of command1 (0)
END
```

#### Exit Code Propagation

| Left Exit Code | Right Executes? | Chain Exit Code        |
|----------------|-----------------|------------------------|
| 0 (success)    | No              | 0 (from left)          |
| Non-zero       | Yes             | Exit code of right     |

The exit code propagates from whichever command was last executed.

### Examples

```bash
# Left succeeds, right is skipped
true || echo "never printed"
# Output: (none)
# Exit code: 0

# Left fails, right runs
false || echo "fallback"
# Output: fallback
# Exit code: 0 (from `echo`)

# Error recovery
rm nonexistent_file || echo "file not found"
# Output: file not found
# Exit code: 0 (from `echo`)

# Default values pattern
cat config.txt || cat default_config.txt
# Try first file, fall back to second
```

### Use Cases

1. **Error handling**: Provide fallback behavior on failure
2. **Default values**: Try preferred option, fall back to default
3. **Error messages**: `command || echo "command failed"`

---

## Semicolon Operator

### Syntax

```
command1 ; command2
```

### Semantics

The semicolon operator provides **unconditional sequential execution**. The right command **always** executes after the left command completes, regardless of the left command's exit code.

#### Execution Flow

```
Execute command1
(ignore exit code)
Execute command2
RETURN exit_code of command2
```

#### Exit Code Propagation

The exit code is always the exit code of the **last command** in the sequence.

| Left Exit Code | Right Executes? | Chain Exit Code        |
|----------------|-----------------|------------------------|
| 0 (success)    | Yes             | Exit code of right     |
| Non-zero       | Yes             | Exit code of right     |

### Examples

```bash
# Both commands always run
echo "first" ; echo "second"
# Output:
# first
# second
# Exit code: 0

# Left failure does not stop execution
false ; echo "still runs"
# Output: still runs
# Exit code: 0 (from `echo`)

# Exit code is from last command
true ; false
# Exit code: 1 (from `false`)

false ; true
# Exit code: 0 (from `true`)

# Multiple commands
echo "a" ; echo "b" ; echo "c"
# All three execute in order
```

### Newlines Are Equivalent Separators

An unquoted newline at depth 0 is lexed as a soft `;` (lexer.md §3.4): within one parsed input, `echo a ; echo b` and `echo a` ⏎ `echo b` build the SAME chain.

```bash
# Equivalent behavior (one parsed input):
echo a ; echo b
echo a
echo b
```

- Interactive mode reads one LINE at a time, so each line is a separate parse and newlines never reach the lexer
- Non-interactive input — scripts, piped stdin, `fsh-exec` strings — is parsed as ONE input in which newlines separate commands (execution.md §Non-Interactive Mode)
- After a chain operator a newline is a CONTINUATION, not a separator: `cmd1 &&` ⏎ `cmd2` behaves exactly like `cmd1 && cmd2` (lexer.md §3.4)

### Use Cases

1. **Multiple independent commands**: Run several commands in sequence
2. **Cleanup operations**: `command ; cleanup` (cleanup always runs)
3. **Command grouping**: Multiple operations on one line

---

## Operator Precedence and Associativity

### Precedence Hierarchy

Foundation Shell has two precedence levels:

| Precedence | Operators     | Description           |
|------------|---------------|-----------------------|
| Higher     | `\|`          | Pipeline operator     |
| Lower      | `&&` `\|\|` `;` | Logical/sequential    |

**Key Rule:** Pipes bind tighter than logical operators. Commands connected by pipes form a **pipeline unit** that is treated as a single command for logical operators.

### Associativity

All operators are **left-associative**. Operations are evaluated from left to right at the same precedence level.

### Precedence Examples

```bash
# Example 1: Pipe binds tighter than AND
echo test | grep test && echo found

# Parsed as: (echo test | grep test) && echo found
# 1. Execute pipeline: echo test | grep test
# 2. If pipeline succeeds (exit 0), execute: echo found
```

```bash
# Example 2: Pipe binds tighter than OR
cat file | grep pattern || echo "not found"

# Parsed as: (cat file | grep pattern) || echo "not found"
# 1. Execute pipeline: cat file | grep pattern
# 2. If pipeline fails (exit != 0), execute: echo "not found"
```

```bash
# Example 3: Mixed logical operators (left-to-right)
cmd1 && cmd2 || cmd3

# Parsed as: (cmd1 && cmd2) || cmd3
# 1. If cmd1 succeeds, run cmd2
# 2. If (cmd1 && cmd2) fails, run cmd3
```

```bash
# Example 4: Multiple pipelines with logical operators
echo a | grep a && echo b | grep b || echo c

# Parsed as: ((echo a | grep a) && (echo b | grep b)) || echo c
# Pipeline segments are: [echo a | grep a], [echo b | grep b], [echo c]
```

### Precedence Resolution Algorithm

1. **Scan for pipeline segments**: Group consecutive commands connected by `|`
2. **Process logical operators left-to-right**: Apply `&&`, `||`, `;` between pipeline segments

```
Input: cmd1 | cmd2 && cmd3 | cmd4 || cmd5

Step 1 - Identify pipelines:
  Pipeline A: cmd1 | cmd2
  Pipeline B: cmd3 | cmd4
  Command C:  cmd5

Step 2 - Apply logical operators:
  ((Pipeline A) && (Pipeline B)) || (Command C)
```

---

## Combining Operators in Complex Chains

### Valid Combinations

All operators can be combined in a single command line:

```bash
# Pipeline with AND
echo test | grep test && echo "match found"

# Pipeline with OR
cat file | grep missing || echo "no match"

# Pipeline with semicolon
echo start | cat ; echo end

# Mixed logical operators
false || echo "one" && echo "two"
# Output: one
#         two
# Explanation: false fails -> "one" runs (exit 0) -> "two" runs

# Complex chain
cmd1 | cmd2 && cmd3 | cmd4 || cmd5 ; cmd6
```

### Execution Tracing

For the command: `false || echo one && echo two`

```
Step 1: Execute `false`
        Exit code: 1 (failure)

Step 2: OR operator sees failure
        Execute `echo one`
        Output: one
        Exit code: 0 (success)

Step 3: AND operator sees success
        Execute `echo two`
        Output: two
        Exit code: 0 (success)

Final exit code: 0
```

For the command: `true && false || echo fallback`

```
Step 1: Execute `true`
        Exit code: 0 (success)

Step 2: AND operator sees success
        Execute `false`
        Exit code: 1 (failure)

Step 3: OR operator sees failure
        Execute `echo fallback`
        Output: fallback
        Exit code: 0 (success)

Final exit code: 0
```

### Pipeline Interaction with Logical Operators

When a pipeline precedes a logical operator, the **entire pipeline** must complete before the logical operator evaluates:

```bash
cmd1 | cmd2 | cmd3 && cmd4
```

1. `cmd1`, `cmd2`, `cmd3` execute concurrently as a pipeline
2. Wait for all pipeline commands to complete
3. Take exit code from `cmd3` (rightmost)
4. If exit code is 0, execute `cmd4`

### Skip Behavior for Pipelines

When a logical operator causes skipping, the **entire subsequent pipeline** is skipped:

```bash
false && cmd1 | cmd2 | cmd3 || echo "skipped pipeline"
```

1. `false` fails (exit 1)
2. AND operator skips `cmd1 | cmd2 | cmd3` entirely
3. OR operator sees failure, runs `echo "skipped pipeline"`

---

## Error Cases

### Operator at Start of Line

An operator cannot appear as the first token in a command line.

```bash
# ERROR: unexpected operator at start
| cmd
&& cmd
|| cmd
; cmd    # Note: semicolon at start is also invalid
```

**Error Message:** `unexpected operator at start: <operator>`

**Error Code:** Parse error, exit code 1

### Operator at End of Line

Chain operators (`|`, `&&`, `||`) cannot appear as the last token. They require a right operand.

```bash
# ERROR: unexpected operator at end
cmd |
cmd &&
cmd ||
```

**Error Message:** `unexpected operator at end: <operator>`

**Error Code:** Parse error, exit code 1

**Note:** A trailing semicolon (`;`) is **valid** in Foundation Shell (like bash). It simply has no effect.

```bash
# Valid: trailing semicolon is allowed
cmd ;
```

### Missing Operands

A chain operator with a missing operand is reported by the position rules above — there is no separate "missing operand" error:

```bash
| cmd     # ERROR: unexpected operator at start: |
cmd |     # ERROR: unexpected operator at end: |
```

The `empty command` error is distinct: it applies when a command consists of redirections only, with no words at all:

```bash
# ERROR: empty command (redirection-only command)
> file
```

**Error Message:** `empty command`

**Error Code:** Parse error, exit code 1

(Canonical error strings: diagnostics.md §5.2.)

### Consecutive Operators

Two chain operators cannot appear consecutively without a command between them.

```bash
# ERROR: consecutive operators
cmd && || other
cmd | | other
cmd && && other
cmd ; ; other
```

**Error Message:** `consecutive operators: <op1> followed by <op2>`

**Error Code:** Parse error, exit code 1

### Empty Input

An empty command line or whitespace-only input is an error (for parse operations).

```bash
# ERROR: empty input
""
"   "
```

**Error Message:** `empty input`

**Note:** In interactive mode, empty lines are silently ignored rather than producing an error.

### Summary of Error Conditions

| Error Condition         | Example          | Error Message                    |
|-------------------------|------------------|----------------------------------|
| Operator at start       | `\| cmd`         | unexpected operator at start: \| |
| Operator at end         | `cmd &&`         | unexpected operator at end: &&   |
| Empty command           | `> file`         | empty command                    |
| Consecutive operators   | `cmd \| \| x`    | consecutive operators: \| followed by \| |
| Empty input             | (empty string)   | empty input                      |

---

## Implementation Notes

### Internal Representation

The parser produces a `Chain` structure:

```go
type Chain struct {
    Commands  []*CommandSpec   // List of commands
    Operators []token.TokenType // Operators between commands (len = len(Commands) - 1)
}
```

### Token Types

```go
const (
    Pipe      TokenType // |
    And       TokenType // &&
    Or        TokenType // ||
    Semicolon TokenType // ;
)
```

### Execution Algorithm

The decision whether a segment runs is made BEFORE executing it, from the operator that PRECEDES it and the current propagated status. A skipped segment preserves `lastStatus` unchanged, so the operator after a skipped segment is evaluated against the ORIGINAL status — `false && a && b` runs nothing and returns 1; `true || a || b` runs nothing and returns 0.

```
function ExecuteChain(chain):
    lastStatus = 0
    i = 0
    while i < len(chain.Commands):
        // Find extent of the current pipeline segment
        pipelineEnd = i
        while pipelineEnd < len(chain.Operators) and chain.Operators[pipelineEnd] == Pipe:
            pipelineEnd++

        // The operator PRECEDING this segment decides whether it runs,
        // evaluated against the CURRENT lastStatus
        run = true
        if i > 0:
            switch chain.Operators[i-1]:
                case And:       run = (lastStatus == 0)
                case Or:        run = (lastStatus != 0)
                case Semicolon: run = true

        if run:
            pipelineCommands = chain.Commands[i : pipelineEnd+1]
            lastStatus = ExecutePipeline(pipelineCommands)
        // else: segment SKIPPED - lastStatus is PRESERVED, and the next
        // iteration evaluates the following operator against it

        // Move past this pipeline segment
        i = pipelineEnd + 1

    return lastStatus
```

Worked example — `false && echo a && echo b`:

```
Segment 1: false            runs (first segment)      -> lastStatus = 1
Segment 2: echo a           preceded by && , status 1 -> SKIPPED, lastStatus stays 1
Segment 3: echo b           preceded by && , status 1 -> SKIPPED, lastStatus stays 1
Result: no output, exit status 1
```

### Pipeline Execution

Pipelines execute all commands concurrently (mechanics canonical in execution.md §Pipeline Execution):

1. Create pipes between adjacent commands
2. Launch all commands in goroutines
3. Connect stdout[i] to stdin[i+1] via pipes
4. When a command finishes, close BOTH of its pipe ends — an early-exiting consumer terminates its producer (`yes | head -1` must not hang)
5. Wait for all commands to complete
6. Return exit code of rightmost command

### Thread Safety

Stderr from concurrent pipeline commands is serialized through a synchronized writer to prevent output interleaving.

---

## Conformance Notes

### POSIX Compatibility

Foundation Shell operators follow POSIX shell semantics with these characteristics:

1. **Pipe exit code**: Returns rightmost command's exit code (POSIX compliant)
2. **Short-circuit evaluation**: AND/OR behave per POSIX specification
3. **Left-to-right evaluation**: Matches POSIX evaluation order
4. **Precedence**: Pipes bind tighter than logical operators (POSIX compliant)
5. **Trailing semicolon**: allowed and consumed, like bash and POSIX shells (parser.md)

### Differences from Bash

1. **No `pipefail`**: Foundation Shell does not support `set -o pipefail`
2. **No `PIPESTATUS`**: No array of individual pipeline exit codes

### Future Considerations

Potential future additions (not currently implemented):

- Background operator (`&`)
- Subshell grouping with `()`
- Brace grouping with `{}`
- Negation operator (`!`)
- `pipefail` option for pipeline exit codes
