---
title: Expansion Specification
description: Tilde expansion, environment variable expansion, and command substitution processing.
recommend_after: quoting.md
---

# Foundation Shell Expansion Specification

This document is the authoritative specification for Foundation Shell's expansion system. It defines how the shell transforms input text by expanding tildes, environment variables, and command substitutions.

## Table of Contents

1. [Overview](#overview)
2. [Expansion Order](#expansion-order)
3. [Tilde Expansion](#tilde-expansion)
4. [Variable Expansion](#variable-expansion)
5. [Command Substitution](#command-substitution)
6. [Quoting and Expansion](#quoting-and-expansion)
7. [Escape Sequences](#escape-sequences)
8. [Word Splitting](#word-splitting)
9. [Special Variables](#special-variables)
10. [Edge Cases and Error Handling](#edge-cases-and-error-handling)

---

## Overview

Foundation Shell's expansion system transforms tokens through a series of ordered transformations. The expansion phase occurs **after** tokenization and **before** command execution.

### Implementation Reference

The expansion system is implemented in:
- `/go/internal/expander/expander.go` - Core expansion functions
- `/go/internal/lexer/lexer.go` - Tokenization with quote handling
- `/go/pkg/parser/parser.go` - Integration of expansion into parsing

### Key Principles

1. **Single-quoted content is never expanded** - Content inside single quotes is treated as literal text
2. **Expansion order is deterministic** - Tilde first, then variables, then command substitution
3. **Non-existent variables expand to empty string** - No error is raised
4. **Trailing newlines are trimmed from command substitution output** - Standard shell behavior

---

## Expansion Order

Foundation Shell performs expansions in a strict, well-defined order:

### Order of Operations

```
1. Tilde Expansion (~)
2. Variable Expansion ($VAR, ${VAR})
3. Command Substitution ($(...), `...`)
```

### Processing Flow

```
Input Token
    |
    v
[Check if single-quoted] ----yes----> Return literal (no expansion)
    |
    no
    v
[Tilde Expansion] - Expand ~ at start of token
    |
    v
[Variable Expansion] - Expand $VAR and ${VAR}
    |
    v
[Command Substitution] - Expand $(...) and `...`
    |
    v
[Strip Escape Markers] - Convert \x01$ to literal $
    |
    v
Expanded Token
```

### Implementation

```go
func Expand(token string, wasSingleQuoted bool) string {
    if wasSingleQuoted {
        return token
    }

    // Apply tilde expansion first (only at start of token)
    result := ExpandTilde(token)

    // Then apply environment variable expansion
    result = ExpandEnvironment(result)

    return result
}
```

---

## Tilde Expansion

Tilde expansion replaces `~` at the beginning of a token with the user's home directory.

### Syntax

| Pattern | Expansion | Description |
|---------|-----------|-------------|
| `~` | `$HOME` | Expands to home directory |
| `~/path` | `$HOME/path` | Expands to path under home |
| `~user` | `~user` | **NOT SUPPORTED** - left unchanged |
| `~user/path` | `~user/path` | **NOT SUPPORTED** - left unchanged |

### Rules

1. **Position requirement**: Tilde expansion ONLY occurs when `~` is the first character of the token
2. **Home directory source**: The value is read from the `HOME` environment variable
3. **Missing HOME**: If `HOME` is not set, the tilde is left unexpanded
4. **User expansion**: The `~username` syntax is NOT supported and is left as-is

### Examples

Given `HOME=/home/testuser`:

| Input | Output | Notes |
|-------|--------|-------|
| `~` | `/home/testuser` | Simple home expansion |
| `~/` | `/home/testuser/` | Home with trailing slash |
| `~/docs` | `/home/testuser/docs` | Path under home |
| `~/a/b/c` | `/home/testuser/a/b/c` | Deep path under home |
| `~/.config` | `/home/testuser/.config` | Hidden directory under home |
| `~user` | `~user` | User expansion not supported |
| `~user/docs` | `~user/docs` | User expansion not supported |
| `/path/~` | `/path/~` | Tilde not at start |
| `/home/other` | `/home/other` | No tilde to expand |
| `echo~` | `echo~` | Tilde not at start |

### Implementation

```go
func ExpandTilde(token string) string {
    if len(token) == 0 || token[0] != '~' {
        return token
    }

    home := os.Getenv("HOME")
    if home == "" {
        return token
    }

    // Just "~"
    if len(token) == 1 {
        return home
    }

    // "~/..." - expand to home + rest
    if token[1] == '/' {
        return home + token[1:]
    }

    // "~user" or "~something" - leave unchanged
    return token
}
```

---

## Variable Expansion

Variable expansion replaces references to environment variables with their values.

### Syntax

Foundation Shell supports two syntaxes for variable expansion:

| Syntax | Description |
|--------|-------------|
| `$VAR` | Simple variable reference |
| `${VAR}` | Braced variable reference |

### Variable Name Rules

Valid variable names consist of:
- **First character**: Letter (a-z, A-Z) or underscore (`_`)
- **Subsequent characters**: Letters, digits (0-9), or underscore

```
Valid:   VAR, _VAR, VAR_123, _START, myVar, MY_VAR_2
Invalid: 123VAR, VAR-NAME, VAR.NAME
```

### Expansion Rules

1. **Greedy matching**: For `$VAR` syntax, the longest valid variable name is matched
2. **Non-existent variables**: Expand to empty string (no error)
3. **Empty braces**: `${}` is left as literal `${}`
4. **Unclosed brace**: `${VAR` with no closing `}` is treated as literal text
5. **Dollar at end**: A lone `$` at end of string is kept literal
6. **Dollar followed by non-variable character**: Kept as literal `$`

### Examples

Given environment:
```
TEST_VAR=hello
TEST_VAR2=world
A=a
AB=ab
ABC=abc
```

| Input | Output | Notes |
|-------|--------|-------|
| `$TEST_VAR` | `hello` | Simple expansion |
| `${TEST_VAR}` | `hello` | Braced expansion |
| `$TEST_VAR!` | `hello!` | Text after variable |
| `say $TEST_VAR please` | `say hello please` | Variable in middle |
| `$TEST_VAR $TEST_VAR2` | `hello world` | Multiple variables |
| `${TEST_VAR}suffix` | `hellosuffix` | Braced with immediate suffix |
| `$NONEXISTENT` | `` (empty) | Non-existent variable |
| `${NONEXISTENT}` | `` (empty) | Non-existent braced variable |
| `test$` | `test$` | Dollar at end |
| `$$` | `$$` | Dollar not followed by var char |
| `$123` | `$123` | Dollar followed by digit |
| `${}` | `${}` | Empty braces |
| `${TEST_VAR` | `${TEST_VAR` | Unclosed brace |
| `$ TEST_VAR` | `$ TEST_VAR` | Dollar followed by space |
| `$TEST_VAR$TEST_VAR2` | `helloworld` | Adjacent variables |
| `$ABC` | `abc` | Greedy matching (not `a` + `BC`) |
| `${A}BC` | `aBC` | Braces limit variable name |
| `$A1` | `` (empty) | Expands `A1`, not `A` + `1` |
| `${A}1` | `a1` | Braces limit to `A`, then literal `1` |

### Implementation

```go
func ExpandEnvironment(token string) string {
    if !strings.Contains(token, "$") {
        return token
    }

    var result strings.Builder
    i := 0
    for i < len(token) {
        // Check for escape marker before $
        if i < len(token)-1 && token[i] == '\x01' && token[i+1] == '$' {
            result.WriteByte('$')
            i += 2
            continue
        }

        if token[i] != '$' {
            result.WriteByte(token[i])
            i++
            continue
        }

        // Handle $ at end of string
        if i+1 >= len(token) {
            result.WriteByte('$')
            i++
            continue
        }

        next := token[i+1]

        // Handle ${VAR} syntax
        if next == '{' {
            closeIdx := strings.Index(token[i+2:], "}")
            if closeIdx == -1 {
                result.WriteByte('$')
                i++
                continue
            }
            varName := token[i+2 : i+2+closeIdx]
            if varName == "" {
                result.WriteString("${}")
                i += 3
                continue
            }
            result.WriteString(os.Getenv(varName))
            i += 3 + closeIdx
            continue
        }

        // Handle $VAR syntax
        if !isVarStartChar(next) {
            result.WriteByte('$')
            i++
            continue
        }

        // Find end of variable name (greedy)
        varStart := i + 1
        varEnd := varStart
        for varEnd < len(token) && isVarChar(token[varEnd]) {
            varEnd++
        }
        result.WriteString(os.Getenv(token[varStart:varEnd]))
        i = varEnd
    }

    return result.String()
}

func isVarStartChar(c byte) bool {
    return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isVarChar(c byte) bool {
    return c == '_' || unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c))
}
```

---

## Command Substitution

Command substitution executes a command and replaces the substitution with the command's standard output.

### Syntax

Foundation Shell supports two syntaxes:

| Syntax | Description |
|--------|-------------|
| `$(command)` | Modern syntax (preferred) |
| `` `command` `` | Legacy backtick syntax |

### Rules

1. **Nested substitution**: Innermost substitutions are processed first
2. **Trailing newlines**: Trimmed from command output (standard shell behavior)
3. **Exit code**: The exit code of the substituted command is discarded; only output is captured
4. **Requires executor**: Command substitution only works when an executor is provided during parsing

### Processing Order for Nested Substitutions

For nested command substitutions, the shell processes from innermost to outermost:

```
echo $(cat $(echo file.txt))
      ^^^^^^^^^^^^^^^^^^^^^
           outer substitution
                  ^^^^^^^^^^^^^^^^
                  inner substitution (processed first)
```

**Step 1**: Execute `echo file.txt` -> outputs `file.txt`
**Step 2**: Execute `cat file.txt` -> outputs file contents
**Step 3**: Result replaces the entire substitution

### Examples

| Input | Behavior |
|-------|----------|
| `echo $(pwd)` | Prints current working directory |
| `echo $(echo hello)` | Prints `hello` |
| `` echo `date` `` | Prints current date |
| `$(echo ls)` | Executes `ls` command |
| `$(cat $(echo file))` | Nested: reads file named by inner command |

### Implementation

```go
func ExpandCommandSubstitution(token string, executor SubshellExecutor) (string, error) {
    if executor == nil {
        return "", errors.New("executor cannot be nil")
    }

    result := token

    for {
        // Find innermost $(...) or `...`
        dollarStart, dollarEnd, dollarCmd := findInnermostDollarParen(result)
        backtickStart, backtickEnd, backtickCmd := findInnermostBacktick(result)

        if dollarStart == -1 && backtickStart == -1 {
            break
        }

        // Process the substitution that appears first
        var start, end int
        var cmd string

        if dollarStart == -1 {
            start, end, cmd = backtickStart, backtickEnd, backtickCmd
        } else if backtickStart == -1 {
            start, end, cmd = dollarStart, dollarEnd, dollarCmd
        } else if dollarStart <= backtickStart {
            start, end, cmd = dollarStart, dollarEnd, dollarCmd
        } else {
            start, end, cmd = backtickStart, backtickEnd, backtickCmd
        }

        // Execute and capture output
        output, _, err := executor.Execute(cmd)
        if err != nil {
            return "", err
        }

        // Trim trailing newlines (standard shell behavior)
        output = strings.TrimRight(output, "\n")

        // Replace substitution with output
        result = result[:start] + output + result[end:]
    }

    return result, nil
}
```

### Parenthesis Matching for $()

The `$(...)` syntax handles nested parentheses correctly:

```go
func findMatchingParen(s string, startIdx int) int {
    depth := 1
    for i := startIdx; i < len(s); i++ {
        switch s[i] {
        case '(':
            depth++
        case ')':
            depth--
            if depth == 0 {
                return i
            }
        }
    }
    return -1
}
```

---

## Quoting and Expansion

Quoting controls whether and how expansion occurs within tokens.

### Quote Types

| Quote Type | Expansion Behavior |
|------------|-------------------|
| Unquoted | All expansions occur |
| Double quotes (`"..."`) | Variable and command expansion occur |
| Single quotes (`'...'`) | NO expansion - completely literal |

### Single Quotes

Single quotes preserve the literal value of every character within them:

```bash
echo '$HOME'      # Outputs: $HOME
echo '~'          # Outputs: ~
echo '$(pwd)'     # Outputs: $(pwd)
echo '\n'         # Outputs: \n (backslash and n)
```

**Key behaviors**:
- No variable expansion
- No tilde expansion
- No command substitution
- No escape sequence processing
- Cannot include a literal single quote inside single quotes

### Double Quotes

Double quotes allow expansions but prevent word splitting:

```bash
echo "$HOME"      # Outputs: /home/user (expanded)
echo "~"          # Outputs: ~ (tilde NOT expanded in double quotes)
echo "$(pwd)"     # Outputs: /current/path (command substitution works)
echo "hello world" # Outputs: hello world (spaces preserved as one token)
```

**Key behaviors**:
- Variable expansion occurs
- Command substitution occurs
- Tilde expansion does NOT occur (tilde is literal inside quotes)
- Spaces do not cause word splitting
- Escape sequences are processed

### Mixed Quoting

Adjacent quoted and unquoted sections are concatenated into a single token:

```bash
echo "hello"'world'     # Outputs: helloworld (single token)
echo hello"world"       # Outputs: helloworld (single token)
echo 'single'"double"un # Outputs: singledoubleun (single token)
```

### Quote Tracking

The lexer tracks whether any part of a token was single-quoted:

```go
type TokenContext struct {
    Content         string
    WasSingleQuoted bool
}
```

If ANY part of a token was single-quoted, `WasSingleQuoted` is `true` and the ENTIRE token is treated as literal (no expansion):

```bash
echo 'hello'$HOME   # WasSingleQuoted=true, no expansion on entire token
```

---

## Escape Sequences

Escape sequences allow including special characters literally.

### Supported Escape Sequences

| Sequence | Result | Context |
|----------|--------|---------|
| `\\` | `\` | Outside single quotes |
| `\$` | `$` (literal, no expansion) | Outside single quotes |
| `\ ` (backslash-space) | ` ` (literal space, no split) | Outside single quotes |
| `\"` | `"` | Outside single quotes |
| `\'` | `'` | Outside single quotes |
| `\n` | newline character | Outside single quotes |
| `\t` | tab character | Outside single quotes |
| `\X` (other) | `X` | Outside single quotes |

### Escape Processing Rules

1. **Single quotes**: Escape sequences are NOT processed inside single quotes
2. **Double quotes**: Escape sequences ARE processed inside double quotes
3. **Unquoted**: Escape sequences ARE processed

### Escaped Dollar Sign

The `\$` escape is handled specially to prevent variable expansion:

1. Lexer converts `\$` to `\x01$` (escape marker + dollar)
2. Expander skips expansion when it sees `\x01$`
3. Final cleanup removes the `\x01` marker, leaving literal `$`

```go
const EscapeMarker = '\x01'

// In lexer:
case '$':
    current.WriteRune(EscapeMarker)
    current.WriteRune('$')

// In expander:
if token[i] == '\x01' && token[i+1] == '$' {
    result.WriteByte('$')  // Literal dollar
    i += 2
    continue
}
```

### Examples

| Input | Output | Notes |
|-------|--------|-------|
| `echo hello\ world` | `echo` `hello world` | Space escaped, single token |
| `echo hello\\world` | `echo` `hello\world` | Escaped backslash |
| `echo \$HOME` | `echo` `$HOME` | Escaped dollar, no expansion |
| `echo \"hello\"` | `echo` `"hello"` | Escaped quotes |
| `echo \'hello\'` | `echo` `'hello'` | Escaped single quotes |
| `echo hello\nworld` | `echo` `hello<newline>world` | Newline escape |
| `echo hello\tworld` | `echo` `hello<tab>world` | Tab escape |

---

## Word Splitting

Word splitting determines how expanded values become separate arguments.

### Splitting Rules

1. **Tokenization phase**: Whitespace (spaces, tabs, newlines) separates tokens
2. **Inside quotes**: NO word splitting - quoted content is a single token
3. **Multiple whitespace**: Consecutive whitespace is treated as single separator
4. **Leading/trailing whitespace**: Stripped (does not create empty tokens)

### Word Splitting Behavior

| Input | Tokens | Notes |
|-------|--------|-------|
| `echo hello world` | `echo`, `hello`, `world` | Normal splitting |
| `echo    hello   world` | `echo`, `hello`, `world` | Multiple spaces = one split |
| `echo "hello world"` | `echo`, `hello world` | Quotes prevent splitting |
| `  echo hello  ` | `echo`, `hello` | Leading/trailing stripped |
| `echo` | `echo` | Single word |
| `echo\thello` | `echo`, `hello` | Tab is whitespace |

### Post-Expansion Word Splitting

**Foundation Shell does NOT perform word splitting on expansion results**.

Unlike traditional shells (bash, zsh), if a variable contains spaces, the expanded value remains a single token:

```bash
VAR="hello world"
echo $VAR   # In Foundation Shell: 2 tokens (echo, "hello world")
            # In bash (unquoted): 3 tokens (echo, hello, world)
```

This is because expansion happens AFTER tokenization. The lexer has already determined token boundaries before variables are expanded.

---

## Special Variables

Foundation Shell currently supports standard environment variables. Special shell variables are NOT implemented.

### Supported: Environment Variables

All environment variables set in the shell's environment are accessible:

| Variable | Description |
|----------|-------------|
| `$HOME` | User's home directory |
| `$PATH` | Executable search path |
| `$USER` | Current username |
| `$PWD` | Current working directory |
| `$SHELL` | Path to current shell |
| (any env var) | Any exported environment variable |

### NOT Supported: Special Shell Variables

The following special variables are NOT currently implemented:

| Variable | Description | Status |
|----------|-------------|--------|
| `$?` | Exit code of last command | Not implemented |
| `$$` | Process ID of shell | Not implemented |
| `$!` | PID of last background process | Not implemented |
| `$0` | Name of script/shell | Not implemented |
| `$1`, `$2`, ... | Positional parameters | Not implemented |
| `$#` | Number of positional parameters | Not implemented |
| `$@` | All positional parameters | Not implemented |
| `$*` | All positional parameters (as single word) | Not implemented |
| `$_` | Last argument of previous command | Not implemented |

### NOT Supported: Parameter Expansion

Advanced parameter expansion syntaxes are NOT implemented:

| Syntax | Description | Status |
|--------|-------------|--------|
| `${VAR:-default}` | Use default if unset | Not implemented |
| `${VAR:=default}` | Assign default if unset | Not implemented |
| `${VAR:+value}` | Use value if set | Not implemented |
| `${VAR:?error}` | Error if unset | Not implemented |
| `${#VAR}` | String length | Not implemented |
| `${VAR%pattern}` | Remove suffix | Not implemented |
| `${VAR#pattern}` | Remove prefix | Not implemented |
| `${VAR/pat/rep}` | Pattern substitution | Not implemented |

---

## Edge Cases and Error Handling

### Unclosed Quotes

Unclosed quotes result in a tokenization error:

```bash
echo 'hello     # Error: unclosed single quote
echo "hello     # Error: unclosed double quote
```

### Empty Tokens

Empty quoted strings do not produce tokens:

```bash
echo ""         # Single token: echo
echo ''         # Single token: echo (empty string not added)
```

### Trailing Backslash

A backslash at the end of input (with nothing to escape) is kept literal:

```bash
echo hello\     # Tokens: echo, hello\
```

### Unclosed Command Substitution

Unclosed `$(` results in the `$` being treated literally:

```bash
echo $(pwd      # $(pwd is treated as literal text
```

### Empty Variable Names

Empty braces `${}` are left literal:

```bash
echo ${}        # Outputs: ${}
```

### Non-Existent Variables

Non-existent variables silently expand to empty string:

```bash
echo $NONEXISTENT_VAR    # Outputs: (empty)
echo prefix${MISSING}end # Outputs: prefixend
```

### Variable Expansion in Tilde Result

After tilde expansion, the result undergoes variable expansion:

```bash
HOME="/home/user"
MYVAR="value"
echo ~/$MYVAR   # Outputs: /home/user/value
```

---

## Complete Processing Example

Given:
```
HOME=/home/alice
NAME=Bob
```

Input: `echo "Hello, $NAME" ~/docs '$(pwd)' \$HOME`

**Step 1: Tokenization**
```
Token 1: "echo"         WasSingleQuoted=false
Token 2: "Hello, $NAME" WasSingleQuoted=false
Token 3: "~/docs"       WasSingleQuoted=false
Token 4: "$(pwd)"       WasSingleQuoted=true
Token 5: "\x01$HOME"    WasSingleQuoted=false  (escape marker added)
```

**Step 2: Expansion of each token**

Token 1 (`echo`):
- Tilde: No change
- Variable: No change
- Result: `echo`

Token 2 (`Hello, $NAME`):
- Tilde: No change (no tilde at start)
- Variable: `$NAME` -> `Bob`
- Result: `Hello, Bob`

Token 3 (`~/docs`):
- Tilde: `~` -> `/home/alice`
- Variable: No change
- Result: `/home/alice/docs`

Token 4 (`$(pwd)`):
- **SKIP** (WasSingleQuoted=true)
- Result: `$(pwd)`

Token 5 (`\x01$HOME`):
- Tilde: No change
- Variable: `\x01$` detected, literal `$`
- Result: `$HOME`

**Final command**:
```
echo "Hello, Bob" /home/alice/docs $(pwd) $HOME
```

---

## Implementation Files

| File | Purpose |
|------|---------|
| `/go/internal/expander/expander.go` | Core expansion functions |
| `/go/internal/expander/expander_test.go` | Expansion tests |
| `/go/internal/lexer/lexer.go` | Tokenization with quote handling |
| `/go/internal/lexer/lexer_test.go` | Lexer tests |
| `/go/pkg/parser/parser.go` | Integration of expansion into parsing |
| `/go/internal/chain/chain.go` | Command substitution executor |

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2024-01-12 | Initial specification |
