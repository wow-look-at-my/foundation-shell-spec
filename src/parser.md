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

```go
type CommandSpec struct {
    Args         []string  // Command name and arguments
    InputFile    string    // Input redirection target (< file)
    OutputFile   string    // Output redirection target (> or >> file)
    ErrorFile    string    // Error redirection target (2> or 2>> file)
    AppendOutput bool      // True if >> was used
    AppendError  bool      // True if 2>> was used
}
```

#### Fields

| Field | Type | Description |
|-------|------|-------------|
| `Args` | `[]string` | Command name at index 0, arguments at indices 1+ |
| `InputFile` | `string` | Path for stdin redirection, empty if not redirected |
| `OutputFile` | `string` | Path for stdout redirection, empty if not redirected |
| `ErrorFile` | `string` | Path for stderr redirection, empty if not redirected |
| `AppendOutput` | `bool` | `true` for `>>`, `false` for `>` |
| `AppendError` | `bool` | `true` for `2>>`, `false` for `2>` |

### Chain

Represents a sequence of commands connected by operators.

```go
type Chain struct {
    Commands  []*CommandSpec      // List of commands
    Operators []token.TokenType   // Operators between commands
}
```

#### Invariant

```
len(Operators) == len(Commands) - 1
```

For N commands, there are exactly N-1 operators connecting them.

### classifiedToken

Internal representation after expansion and classification.

```go
type classifiedToken struct {
    tokenType token.TokenType
    value     string
}
```

## Parsing Pipeline

### Step 1: Tokenization

The lexer tokenizes the input into `TokenContext` structures:

```go
tokenContexts, err := lexer.Tokenize(input)
```

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
1. Tilde expansion      (~, ~/path)      - suppressed by WasQuoted
2. Variable expansion   ($VAR, ${VAR})
3. Command substitution ($(...), `...`) - only if executor provided
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

The exact user-visible strings are pinned in diagnostics.md §5.2.

#### Trailing Semicolon

A trailing `;` is VALID. The parser CONSUMES it: it does not appear in `Chain.Operators`, so the invariant `len(Operators) == len(Commands) - 1` is preserved.

```
Input:  "echo hi ;"
Output: Chain{
    Commands: [
        &CommandSpec{Args: ["echo", "hi"]}
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

```go
// Called only for tokens with IsOperator == true
func parseOperator(s string) (token.TokenType, bool) {
    switch s {
    case "|":  return token.Pipe, true
    case "&&": return token.And, true
    case "||": return token.Or, true
    case ";":  return token.Semicolon, true
    case "<":  return token.RedirectStdIn, true
    case ">":  return token.RedirectStdOut, true
    case ">>": return token.RedirectStdOutAppend, true
    case "2>": return token.RedirectStdErr, true
    case "2>>": return token.RedirectStdErrAppend, true
    default:   return 0, false
    }
}
```

## Error Handling

### Defined Errors

```go
var (
    ErrEmptyInput              = errors.New("empty input")
    ErrOperatorAtStart         = errors.New("unexpected operator at start")
    ErrMissingRedirectionTarget = errors.New("missing redirection target")
    ErrTrailingOperator        = errors.New("unexpected operator at end")
    ErrConsecutiveOperators    = errors.New("consecutive operators")
    ErrEmptyCommand            = errors.New("empty command")
)
```

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

## Public API

### Parse

```go
func Parse(input string) (*Chain, error)
```

Parses input without command substitution expansion.

- Equivalent to `ParseWithExecutor(input, nil)`
- `$(...)` and backticks are preserved as literal text
- Suitable for syntax validation and non-interactive parsing

### ParseWithExecutor

```go
func ParseWithExecutor(input string, executor expander.SubstitutionExecutor) (*Chain, error)
```

(`SubstitutionExecutor` is the renamed `SubshellExecutor`; "subshell" is reserved for future `()` grouping.)

Parses input with optional command substitution expansion.

- If `executor` is non-nil, `$(...)` and backticks are expanded
- Executor runs in the same process (not external shell)
- Used by the interactive shell with chain.Executor

## Examples

### Simple Command

```
Input:  "echo hello world"
Output: Chain{
    Commands: [
        &CommandSpec{Args: ["echo", "hello", "world"]}
    ],
    Operators: []
}
```

### Pipeline

```
Input:  "cat file.txt | grep pattern | wc -l"
Output: Chain{
    Commands: [
        &CommandSpec{Args: ["cat", "file.txt"]},
        &CommandSpec{Args: ["grep", "pattern"]},
        &CommandSpec{Args: ["wc", "-l"]}
    ],
    Operators: [Pipe, Pipe]
}
```

### Conditional Execution

```
Input:  "make && make test"
Output: Chain{
    Commands: [
        &CommandSpec{Args: ["make"]},
        &CommandSpec{Args: ["make", "test"]}
    ],
    Operators: [And]
}
```

### Redirection

```
Input:  "cat < input.txt > output.txt 2>> errors.log"
Output: Chain{
    Commands: [
        &CommandSpec{
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
        &CommandSpec{Args: ["echo", "/home/user"]}  // Expanded
    ],
    Operators: []
}
```

### Single-Quoted (No Expansion)

```
Input:  "echo '$HOME'"
Output: Chain{
    Commands: [
        &CommandSpec{Args: ["echo", "$HOME"]}  // Literal
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
- Uses internal parser and chain executor
- Only stdout is captured
- Trailing newlines are stripped
- Exit code is available but typically unused in expansion

### Nested Substitution

```
Input:  "echo $(echo $(whoami))"
```

Substitutions are expanded from innermost to outermost.

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
2. `expander.ExpandEnvironment(value)` - Expand `$VAR` and `${VAR}`
3. `expander.ExpandCommandSubstitution(value, executor)` - Expand `$(...)` and backticks

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

## Testing Considerations

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
