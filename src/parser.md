---
title: Parser Specification
description: Parsing rules, AST structure, token classification, and command chain building.
recommend_after: lexer.md
---

# Parser Specification

> **Canonical for:** token classification, chain building, parse-time validation, and the Chain/CommandSpec structures. The authority map lives in the README.

This document defines the complete specification for Foundation Shell's parser component. The parser orchestrates lexer tokenization, expansion, token classification, and command chain building.

## Overview

The parser is the central component that transforms raw input text into an executable command chain. It coordinates several subsystems:

1. **Lexer** - Tokenizes raw input into token contexts
2. **Expander** - Expands tildes, variables, and command substitutions
3. **Classifier** - Assigns semantic types to tokens
4. **Chain Builder** - Constructs the command chain structure

## Architecture

```
Input String
     │
     ▼
┌─────────┐
│  Lexer  │  → TokenContext[]
└────┬────┘
     │
     ▼
┌──────────┐
│ Expander │  → Expanded values (unless single-quoted)
└────┬─────┘
     │
     ▼
┌────────────┐
│ Classifier │  → classifiedToken[]
└─────┬──────┘
      │
      ▼
┌───────────────┐
│ Chain Builder │  → Chain (CommandSpec[] + Operators[])
└───────────────┘
```

## Data Structures

### CommandSpec

Represents a single command with its arguments and I/O configuration.

#### Fields

| Field | Type | Description |
|-------|------|-------------|
| `Args` | word list | Command name at index 0, arguments at indices 1 and up |
| `InputFile` | path | Target of a `<` redirection. Empty when there is none |
| `OutputFile` | path | Target of a `>` or `>>` redirection. Empty when there is none |
| `ErrorFile` | path | Target of a `2>` or `2>>` redirection. Empty when there is none |
| `AppendOutput` | flag | True for `>>`, false for `>` |
| `AppendError` | flag | True for `2>>`, false for `2>` |

### Chain

Represents a sequence of commands connected by operators.

| Field | Type | Description |
|-------|------|-------------|
| `Commands` | command list | The commands, in input order |
| `Operators` | operator list | The operators between them |

#### Invariant

For N commands there are exactly N−1 operators connecting them.

### The Classified Token

The intermediate form, after expansion and classification. It carries a token type and a value.

## Parsing Pipeline

### Step 1: Tokenization

The lexer tokenizes the input into `TokenContext` values (lexer.md §2.1).

If tokenization fails, return an error:
```
tokenization error: <underlying error>
```

If the result is empty (whitespace-only input):
```
ErrEmptyInput
```

### Step 2: Expansion and Classification

For each token context:

1. **Check for Operator**
   - If the lexer marked the token `IsOperator: true`, map its `Content` to the operator token type (see Operator Detection below)
   - The parser consults ONLY the lexer's operator marking — it NEVER re-derives operators from content. A token whose content is `|` but whose `IsOperator` is `false` (because it was quoted or escaped) is an ordinary value token:
     - `echo "|"` → `Args: ["echo", "|"]` (literal pipe argument, no pipeline)
     - `echo '>'` → `Args: ["echo", ">"]` (literal argument, no redirection)
   - Operators are NOT expanded
   - Chain operators (`|`, `&&`, `||`, `;`) reset `isFirstInCommand` to `true`

2. **Expand Value Tokens**
   - Skip all expansion for single-quoted tokens (`WasSingleQuoted == true`)
   - Apply tilde expansion (skipped when `WasQuoted == true`)
   - Apply environment variable expansion
   - Apply command substitution (if executor provided)

3. **Strip Escape Markers**
   - UNCONDITIONALLY, for every value token — including single-quoted tokens (a concatenation like `'a'\$HOME` carries a marker inside a `WasSingleQuoted` token). This step is NOT part of the expansion block that `WasSingleQuoted` skips

4. **Classify as Command or Argument**
   - If `isFirstInCommand == true`: classify as `Command`, set flag to `false`
   - Otherwise: classify as `CommandArgument`

#### Expansion Order

```
1. Tilde expansion      (~, ~/path)          - suppressed by WasQuoted
2. Variable expansion   ($VAR, ${VAR}, $?)
3. Command substitution ($(...), `...`)     - only if executor provided
   (steps 1-3 all suppressed by WasSingleQuoted)
4. Escape marker strip  (unconditional, every value token)
```

### Step 3: Chain Building

The `buildChain` function validates syntax and constructs the command chain.

#### Validation Rules

| Rule | Error |
|------|-------|
| Empty token list | `ErrEmptyInput` |
| Chain operator at start | `ErrOperatorAtStart: <operator>` |
| Empty command (no args — e.g. redirection-only input `> file`) | `ErrEmptyCommand` |
| Trailing chain operator other than `;` | `ErrTrailingOperator: <operator>` |
| Consecutive chain operators | `ErrConsecutiveOperators: <op1> followed by <op2>` |
| Redirection without target | `ErrMissingRedirectionTarget: <operator>` |
| Redirection followed by operator | `ErrMissingRedirectionTarget: <redir> followed by operator <op>` |
| Redirection target empty after expansion | `ErrEmptyRedirectionTarget` (redirection.md §9.4) |
| Unquoted redirection target starting with `&` | `ErrFdDuplicationUnsupported: <word>` (redirection.md §9.5) |
| Lone unquoted `&` as a word | `ErrBackgroundUnsupported` (§Background Execution Is Guarded) |
| `<` followed by `<` | `ErrHeredocUnsupported` (redirection.md §9.6) |
| Variable assigned then expanded in one input | `ErrAssignmentThenUse: <name>` (§Assignment Then Use Is Guarded) |

The exact user-visible strings are pinned in diagnostics.md §5.2.

#### Background Execution Is Guarded

**Condition:** A word token is exactly `&`, unquoted and unescaped.

This shell has no background jobs, so the lexer hands back a lone `&` as an ordinary word (lexer.md §3.3.2). Accepted as a word, it does two silent things at once:

```bash
server &              # & becomes argv[1]; the server runs in the FOREGROUND
                      # and the shell never returns
cmd_a & cmd_b arg     # ONE command: cmd_a with args [&, cmd_b, arg].
                      # cmd_b never runs, and nothing says so
```

Both outcomes are worse than the `2>&1` trap §9.5 already guards against: that one creates a stray file, this one hangs the shell or silently drops half the line. The parser rejects it for the same reason.

**Behavior:**
- Parse error: `background execution is not supported`
- The check inspects the word AS WRITTEN, before expansion, and applies only when the word is exactly `&` with `WasQuoted == false` and `WasEscaped == false`
- Every other `&` is untouched: `a&b` is one word, `&&` is an operator, and `'&'`, `"&"`, `\&` are literal ampersands

```bash
$ sleep 30 &
background execution is not supported
$ echo 'a & b'        # OK: quoted
$ echo "u?x=1&y=2"    # OK: & inside a longer word
```

A caller that wants a command to outlive the shell runs it under a tool that owns that job — `nohup`, a supervisor, or the caller's own process manager.

#### Assignment Then Use Is Guarded

**Condition:** A command expands `$NAME` when an EARLIER command in the same input assigned `NAME`.

The whole input expands in one pass before any of it runs, so the expansion reads the value from before the input (expansion.md §Assignment Is Not Visible to the Same Input). Unguarded, `OUT=$(cmd); echo $OUT` prints an empty line and reports success — the one construct here that corrupts a result rather than stopping the caller.

**Behavior:**
- Parse error: `variable is assigned and used in the same input: <name>`
- Commands are walked in order, and each command's references are tested against what EARLIER commands assigned, so a word never counts as referencing its own assignment (`X=$X` is legal)
- Two forms arm the guard, because both mutate the shell and both are equally invisible to a later expansion: a standalone `NAME=VALUE` (§Standalone Assignment) and `export NAME=VALUE`
- Redirection targets are checked with the command's words: `> $OUT` picks a filename, so a stale one is worse than a stale argument
- A single-quoted reference is NOT a reference: `sh -c 'echo $X'` hands `$X` to the child, which resolves it against the environment the assignment really did set
- Unlike the guards above, this one has no analyzer counterpart — the analyzer does not model expansion — so it prints as a plain `parse error:` line rather than a caret block (diagnostics.md §5.2)

```bash
$ OUT=$(echo hi) ; echo $OUT
variable is assigned and used in the same input: OUT
$ X=hi ; printenv X            # OK: the child reads the environment
$ X=hi ; sh -c 'echo $X'       # OK: quoted, so the child expands it
$ echo $X ; X=hi               # OK: the use precedes the assignment
```

#### Trailing Semicolon

A trailing `;` is VALID. The parser CONSUMES it: it does not appear in `Chain.Operators`, so the invariant `len(Operators) == len(Commands) - 1` is preserved.

```
Input:  "echo hi ;"
Output: Chain{
    Commands: [
        CommandSpec{Args: ["echo", "hi"]}
    ],
    Operators: []
}
// The trailing ; is consumed and has no effect
```

Trailing `|`, `&&`, and `||` remain errors — they require a right operand.

#### Redirection at Start

Redirection operators at the start ARE valid:
```
< input.txt cat    # Valid: redirects stdin before cat
```

Chain operators at the start are NOT valid:
```
| cat              # Invalid: pipe at start
&& echo hello      # Invalid: AND at start
```

## Operator Classification

### Chain Operators

Chain operators connect commands and control execution flow.

| Operator | Token Type | Behavior |
|----------|------------|----------|
| `\|` | `Pipe` | Connect stdout to stdin |
| `&&` | `And` | Execute next only if previous succeeded |
| `\|\|` | `Or` | Execute next only if previous failed |
| `;` | `Semicolon` | Execute unconditionally |

### Redirection Operators

Redirection operators modify I/O streams.

| Operator | Token Type | Behavior |
|----------|------------|----------|
| `<` | `RedirectStdIn` | Redirect stdin from file |
| `>` | `RedirectStdOut` | Redirect stdout to file (truncate) |
| `>>` | `RedirectStdOutAppend` | Redirect stdout to file (append) |
| `2>` | `RedirectStdErr` | Redirect stderr to file (truncate) |
| `2>>` | `RedirectStdErrAppend` | Redirect stderr to file (append) |

### Operator Detection

Operator detection applies ONLY to tokens the lexer marked `IsOperator: true` (see Step 2 above). For those tokens, the content-to-type mapping is:

| Content | Token type |
|---------|------------|
| `\|` | `Pipe` |
| `&&` | `And` |
| `\|\|` | `Or` |
| `;` | `Semicolon` |
| `<` | `RedirectStdIn` |
| `>` | `RedirectStdOut` |
| `>>` | `RedirectStdOutAppend` |
| `2>` | `RedirectStdErr` |
| `2>>` | `RedirectStdErrAppend` |

No other content maps to an operator type. A token marked as an operator whose content is absent from this table is an internal failure, never a word.

## Error Handling

### Defined Errors

| Name | Message |
|------|---------|
| `ErrEmptyInput` | `empty input` |
| `ErrOperatorAtStart` | `unexpected operator at start` |
| `ErrMissingRedirectionTarget` | `missing redirection target` |
| `ErrTrailingOperator` | `unexpected operator at end` |
| `ErrConsecutiveOperators` | `consecutive operators` |
| `ErrEmptyCommand` | `empty command` |
| `ErrEmptyRedirectionTarget` | `empty redirection target` |
| `ErrFdDuplicationUnsupported` | `file descriptor duplication is not supported` |

The canonical strings, with their placeholder suffixes, are in diagnostics.md §5.

### Error Format

Errors are returned with context using `fmt.Errorf`:

```
tokenization error: <error>
command substitution error: <error>
<error>: <context>
```

The canonical, exact user-visible strings live in diagnostics.md §5.2.

### Error Examples

```
Input: ""
Error: empty input

Input: "| cat"
Error: unexpected operator at start: |

Input: "echo hello |"
Error: unexpected operator at end: |

Input: "echo && || cat"
Error: consecutive operators: && followed by ||

Input: "echo >"
Error: missing redirection target: >

Input: "echo > |"
Error: missing redirection target: > followed by operator |
```

## Public Interface

A parse takes the input string, and optionally a substitution executor (expansion.md §Executor Interface). It yields a chain, or one error.

### Parsing Without an Executor

- `$(...)` and backticks stay as literal text
- This form suits syntax validation and any parse that must not run a command

### Parsing With an Executor

- `$(...)` and backticks expand during the parse
- The executor runs the body in the shell's own process, never in an external shell (execution.md §Command Substitution Executor)
- The interactive shell always parses this way

The name is deliberate. "Subshell" is reserved for the future `()` grouping construct.

## Examples

### Simple Command

```
Input:  "echo hello world"
Output: Chain{
    Commands: [
        CommandSpec{Args: ["echo", "hello", "world"]}
    ],
    Operators: []
}
```

### Pipeline

```
Input:  "cat file.txt | grep pattern | wc -l"
Output: Chain{
    Commands: [
        CommandSpec{Args: ["cat", "file.txt"]},
        CommandSpec{Args: ["grep", "pattern"]},
        CommandSpec{Args: ["wc", "-l"]}
    ],
    Operators: [Pipe, Pipe]
}
```

### Conditional Execution

```
Input:  "make && make test"
Output: Chain{
    Commands: [
        CommandSpec{Args: ["make"]},
        CommandSpec{Args: ["make", "test"]}
    ],
    Operators: [And]
}
```

### Redirection

```
Input:  "cat < input.txt > output.txt 2>> errors.log"
Output: Chain{
    Commands: [
        CommandSpec{
            Args: ["cat"],
            InputFile: "input.txt",
            OutputFile: "output.txt",
            ErrorFile: "errors.log",
            AppendOutput: false,
            AppendError: true
        }
    ],
    Operators: []
}
```

### Mixed Operators

```
Input:  "cmd1 | cmd2 && cmd3 || cmd4 ; cmd5"
Output: Chain{
    Commands: [cmd1, cmd2, cmd3, cmd4, cmd5],
    Operators: [Pipe, And, Or, Semicolon]
}
```

### Variable Expansion

```
Input:  "echo $HOME"
Output: Chain{
    Commands: [
        CommandSpec{Args: ["echo", "/home/user"]}  // Expanded
    ],
    Operators: []
}
```

### Single-Quoted (No Expansion)

```
Input:  "echo '$HOME'"
Output: Chain{
    Commands: [
        CommandSpec{Args: ["echo", "$HOME"]}  // Literal
    ],
    Operators: []
}
```

## Command Substitution

When an executor is provided, command substitutions are expanded:

```
Input:  "echo $(whoami)"
Result: "echo <username>"  // $(whoami) executed and replaced
```

### Execution Model

- Substitutions execute in the SAME process
- The body is re-parsed recursively with the same parser and executor (expansion.md §Recursive Execution)
- Only stdout is captured
- Trailing newlines are stripped
- The substituted command's exit status is DISCARDED: it does not become `$?` and does not affect the surrounding command line (expansion.md §Failure Semantics)

### Nested Substitution

```
Input:  "echo $(echo $(whoami))"
```

Nesting is handled by the recursive re-parse of the body: syntactically inner substitutions complete first, and substitution OUTPUT is never re-scanned (expansion.md §Single-Pass Expansion).

## Integration with Lexer

The parser depends on the lexer for initial tokenization. Key properties preserved:

| Property | Usage |
|----------|-------|
| `Content` | Raw token value |
| `WasSingleQuoted` | Skip all expansion if true |
| `WasQuoted` | Skip tilde expansion if true |
| `IsOperator` | Sole basis for operator classification |

## Integration with Expander

The parser uses three expansion functions:

1. `expander.ExpandTilde(value)` - Expand `~` to HOME
2. `expander.ExpandEnvironment(value, lastStatus)` - Expand `$VAR`, `${VAR}`, and `$?`
3. `expander.ExpandCommandSubstitution(value, executor)` - Expand `$(...)` and backticks

`lastStatus` is the shell's last recorded command-line status, supplied to the parser for every parse so that `$?` can expand (expansion.md §Special Parameters; execution.md §Last Exit Code).

## Semantic Classification

After expansion, tokens are classified:

| Token Type | When Used |
|------------|-----------|
| `Command` | First token in a command |
| `CommandArgument` | Subsequent tokens |
| Operator types | Detected operators |

The classifier tracks state:
- `isFirstInCommand`: Reset to true after each chain operator
- Position tracking for proper command/argument distinction

## Grammar (Informal)

```
chain       := command (chain_op command)* [';']
chain_op    := PIPE | AND | OR | SEMICOLON
command     := redirection* word (word | redirection)*
redirection := REDIR_OP word
REDIR_OP    := '<' | '>' | '>>' | '2>' | '2>>'
word        := any value token (lexer.md §2.3)
```

Every command contains at least one word — the grammar itself excludes redirection-only commands (`> file`), which the validator reports as `ErrEmptyCommand`. The optional final `;` is the consumed trailing semicolon. `word` is a lexer value token (a `TokenContext` with `IsOperator: false`); there is no separate quoted-string token type — quoting is resolved by the lexer before classification.

## Conformance Cases

### Valid Inputs

- Single command
- Multiple commands with pipes
- Conditional operators (&&, ||)
- Sequential execution (;)
- Trailing semicolon (consumed; not in Chain.Operators)
- Redirection combinations
- Mixed operators
- Quoted arguments (single and double)
- Quoted/escaped operator characters as literal arguments (`echo "|"`)
- Variable expansion
- Tilde expansion

### Invalid Inputs

- Empty or whitespace-only input → `ErrEmptyInput`
- Chain operator at start (redirection at start is valid)
- Trailing chain operator other than `;`
- Consecutive chain operators
- Redirection without target
- Redirection-only command (`> file`) → `ErrEmptyCommand`
- Malformed quotes (detected by lexer/analyzer)

### Edge Cases

- Whitespace-only input
- Multiple redirections to same stream (last wins)
- Redirection before command name
- Empty quotes (`""`, `''`)
- Escaped characters in various contexts
