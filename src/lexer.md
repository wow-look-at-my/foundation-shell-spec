# Lexer Specification

## 1. Overview

The lexer (tokenizer) is the first stage of Foundation Shell's input processing pipeline. It transforms raw input strings into a sequence of tokens that preserve semantic information about quoting context. The lexer operates as a single-pass scanner that handles escape sequences, quote state tracking, and whitespace-based token separation.

### 1.1 Design Philosophy

Foundation Shell's lexer follows these principles:

1. **Quote Context Preservation**: Tokens retain metadata about whether they were single-quoted, enabling downstream components to decide whether to perform expansions.
2. **Escape Marker System**: Escaped dollar signs are marked with a special character (`\x01`) to prevent expansion while preserving the original intent.
3. **Operator Deferral**: The lexer does NOT recognize operators; it produces raw string tokens. Operator recognition happens during parsing.
4. **Unicode Support**: The lexer operates on runes, supporting full Unicode input.

### 1.2 Processing Pipeline Position

```
Input String
     |
     v
  [LEXER] --> TokenContext[] (Content + WasSingleQuoted)
     |
     v
  [PARSER] --> Operator recognition, expansion, classification
     |
     v
  [CHAIN BUILDER] --> CommandSpec structures
```

---

## 2. Token Types

### 2.1 Lexer Output: TokenContext

The lexer produces `TokenContext` structures, NOT the formal `Token` types. Token classification happens in the parser.

```go
type TokenContext struct {
    Content         string  // The token's text content (escapes processed)
    WasSingleQuoted bool    // True if any part was inside single quotes
}
```

### 2.2 Formal Token Types (Classified by Parser)

After lexer output passes through the parser, tokens are classified into these formal types:

#### 2.2.1 Value Types

| Type | Description | Example |
|------|-------------|---------|
| `Command` | First word in a command | `ls`, `echo`, `git` |
| `CommandArgument` | Arguments following a command | `-la`, `--help`, `file.txt` |

Value tokens carry a string value in their `Value` field.

#### 2.2.2 Operator Types

| Type | Symbol | Description |
|------|--------|-------------|
| `Pipe` | `\|` | Pipeline operator |
| `And` | `&&` | Logical AND (short-circuit) |
| `Or` | `\|\|` | Logical OR (short-circuit) |
| `Semicolon` | `;` | Command separator |
| `RedirectStdIn` | `<` | Input redirection |
| `RedirectStdOut` | `>` | Output redirection (overwrite) |
| `RedirectStdOutAppend` | `>>` | Output redirection (append) |
| `RedirectStdErr` | `2>` | Stderr redirection (overwrite) |
| `RedirectStdErrAppend` | `2>>` | Stderr redirection (append) |

Operator tokens have an empty `Value` field; their meaning is conveyed by their type.

---

## 3. Tokenization Rules

### 3.1 Whitespace Handling

Whitespace serves as a token separator, NOT a token itself. The lexer consumes whitespace without producing tokens for it.

#### 3.1.1 Whitespace Characters

The lexer uses Go's `unicode.IsSpace()` to identify whitespace, which includes:

- Space (U+0020)
- Horizontal tab (U+0009)
- Newline (U+000A)
- Vertical tab (U+000B)
- Form feed (U+000C)
- Carriage return (U+000D)
- Various Unicode space characters

#### 3.1.2 Whitespace Behavior

| Context | Behavior |
|---------|----------|
| Outside quotes | Terminates current token, starts new token |
| Inside single quotes | Preserved literally as part of token |
| Inside double quotes | Preserved literally as part of token |
| Leading whitespace | Ignored (no empty token produced) |
| Trailing whitespace | Ignored (no empty token produced) |
| Multiple consecutive spaces | Treated as single separator |

#### 3.1.3 Examples

```
Input: "echo hello world"
Tokens: ["echo", "hello", "world"]

Input: "echo    hello   world"
Tokens: ["echo", "hello", "world"]

Input: "  echo hello  "
Tokens: ["echo", "hello"]

Input: "echo 'hello world'"
Tokens: ["echo", "hello world"]
```

### 3.2 Word Boundaries

A word (token) boundary occurs when:

1. Unquoted whitespace is encountered
2. End of input is reached

Words are accumulated character-by-character until a boundary is reached.

### 3.3 Operator Recognition

**IMPORTANT**: The lexer does NOT perform operator recognition. Operators like `|`, `&&`, `||`, `;`, `>`, `<`, `>>`, `2>`, `2>>` are treated as regular characters during lexing.

Operator recognition happens in the parser's `parseOperator()` function, which checks if a token's content matches an operator string:

```go
func parseOperator(s string) (token.TokenType, bool) {
    switch s {
    case "|":   return token.Pipe, true
    case "&&":  return token.And, true
    case "||":  return token.Or, true
    case ";":   return token.Semicolon, true
    case "<":   return token.RedirectStdIn, true
    case ">":   return token.RedirectStdOut, true
    case ">>":  return token.RedirectStdOutAppend, true
    case "2>":  return token.RedirectStdErr, true
    case "2>>": return token.RedirectStdErrAppend, true
    default:    return 0, false
    }
}
```

This means operators MUST be whitespace-separated from adjacent tokens:

```
Valid:   "echo hello | grep world"
Invalid: "echo hello|grep world"  (produces token "hello|grep")
```

---

## 4. Quote Handling

Foundation Shell supports three quoting mechanisms, each with distinct semantics.

### 4.1 Single Quotes (`'...'`)

Single quotes create **strong quoting** - everything inside is preserved literally.

#### 4.1.1 Single Quote Rules

| Rule | Description |
|------|-------------|
| No escape processing | Backslashes are literal: `'a\b'` -> `a\b` |
| No variable expansion | Dollar signs are literal: `'$VAR'` -> `$VAR` |
| Preserves whitespace | Spaces don't split: `'a b'` -> single token `a b` |
| Cannot contain single quote | No way to include `'` inside single quotes |
| Sets WasSingleQuoted flag | Token marked for expansion suppression |

#### 4.1.2 Single Quote Examples

```
Input: echo 'hello world'
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "hello world", WasSingleQuoted: true}]

Input: echo 'hello $VAR \n world'
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "hello $VAR \n world", WasSingleQuoted: true}]

Input: echo 'back\\slash'
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "back\\slash", WasSingleQuoted: true}]
```

### 4.2 Double Quotes (`"..."`)

Double quotes create **weak quoting** - whitespace is preserved but escapes are processed.

#### 4.2.1 Double Quote Rules

| Rule | Description |
|------|-------------|
| Escape processing enabled | Backslash sequences are interpreted |
| Variable expansion allowed | `$VAR` and `${VAR}` will be expanded (by expander) |
| Preserves whitespace | Spaces don't split tokens |
| Can contain escaped quotes | `\"` produces literal `"` |
| Does NOT set WasSingleQuoted | Token eligible for expansion |

#### 4.2.2 Double Quote Examples

```
Input: echo "hello world"
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "hello world", WasSingleQuoted: false}]

Input: echo "say \"hello\""
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "say \"hello\"", WasSingleQuoted: false}]

Input: echo "path\\to\\file"
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "path\to\file", WasSingleQuoted: false}]
```

### 4.3 Backticks (`` `...` ``)

Backticks are used for command substitution. The lexer does NOT process backticks specially - they are treated as regular characters. Command substitution expansion happens in the expander phase.

#### 4.3.1 Backtick Behavior in Lexer

```
Input: echo `date`
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "`date`", WasSingleQuoted: false}]
```

The backtick-delimited content remains in the token for later expansion.

### 4.4 Quote Concatenation

Adjacent quoted and unquoted segments without whitespace are concatenated into a single token.

#### 4.4.1 Concatenation Rules

| Pattern | Result |
|---------|--------|
| `"hello"'world'` | Single token: `helloworld` |
| `hello"world"` | Single token: `helloworld` |
| `'single'"double"` | Single token: `singledouble` |
| `"hello""world"` | Single token: `helloworld` |

#### 4.4.2 WasSingleQuoted with Concatenation

If ANY part of a concatenated token was single-quoted, the entire token is marked as `WasSingleQuoted: true`. This is a conservative approach to prevent unintended expansion.

```
Input: echo 'single'"double"unquoted
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "singledoubleunquoted", WasSingleQuoted: true}]
```

### 4.5 Empty Quoted Strings

Empty quoted strings (`""` or `''`) produce no content:

```
Input: echo ""
Tokens: [{Content: "echo", WasSingleQuoted: false}]
// Note: The empty string does not produce a separate token

Input: echo ''
Tokens: []
// If only empty quotes with no other content, no tokens produced
```

---

## 5. Escape Sequences

Escape processing occurs outside single quotes. The backslash character (`\`) initiates an escape sequence.

### 5.1 Escape Sequence Table

| Sequence | Result | Description |
|----------|--------|-------------|
| `\\` | `\` | Literal backslash |
| `\ ` (backslash-space) | ` ` | Literal space (prevents word splitting) |
| `\$` | `\x01$` | Escaped dollar (marked to prevent expansion) |
| `\"` | `"` | Literal double quote |
| `\'` | `'` | Literal single quote |
| `\n` | newline (0x0A) | Newline character |
| `\t` | tab (0x09) | Tab character |
| `\X` (any other) | `X` | The character itself (backslash removed) |

### 5.2 Escape Context Rules

| Context | Escape Processing |
|---------|-------------------|
| Outside all quotes | YES - all escapes processed |
| Inside double quotes | YES - all escapes processed |
| Inside single quotes | NO - backslash is literal |

### 5.3 The Escape Marker System

When `\$` is encountered (outside single quotes), the lexer produces the two-character sequence `\x01$` rather than just `$`. This "escape marker" (`\x01`, ASCII SOH) serves as a flag to the expander that this dollar sign should NOT trigger variable expansion.

#### 5.3.1 Escape Marker Constant

```go
const EscapeMarker = '\x01'  // ASCII Start of Heading
```

#### 5.3.2 Escape Marker Lifecycle

1. **Lexer**: `\$VAR` becomes `\x01$VAR`
2. **Expander**: Sees `\x01$`, skips expansion, outputs `$VAR`
3. **Post-expansion**: `StripEscapeMarkers()` removes any remaining `\x01` characters

#### 5.3.3 StripEscapeMarkers Function

```go
func StripEscapeMarkers(s string) string {
    return strings.ReplaceAll(s, string(EscapeMarker), "")
}
```

### 5.4 Escape Examples

```
Input: echo hello\ world
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "hello world", WasSingleQuoted: false}]

Input: echo hello\\world
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "hello\world", WasSingleQuoted: false}]

Input: echo \$HOME
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "\x01$HOME", WasSingleQuoted: false}]

Input: echo \"hello\"
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "\"hello\"", WasSingleQuoted: false}]

Input: echo hello\nworld
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "hello\nworld", WasSingleQuoted: false}]
// Note: \n becomes actual newline character (0x0A)
```

### 5.5 Trailing Backslash

A backslash at the end of input (with nothing to escape) is preserved literally:

```
Input: echo hello\
Tokens: [{Content: "echo", WasSingleQuoted: false},
         {Content: "hello\", WasSingleQuoted: false}]
```

---

## 6. Variable Recognition

**IMPORTANT**: The lexer does NOT perform variable recognition or expansion. Variables (`$VAR`, `${VAR}`) are preserved as literal text in tokens. Variable expansion is handled by the expander phase.

### 6.1 Variable Syntax (Recognized by Expander)

The expander recognizes two variable syntaxes:

| Syntax | Example | Description |
|--------|---------|-------------|
| `$VAR` | `$HOME`, `$PATH` | Simple variable reference |
| `${VAR}` | `${HOME}`, `${USER}` | Braced variable reference |

### 6.2 Variable Name Rules

Variable names follow these rules (enforced by expander):

- **First character**: Letter (a-z, A-Z) or underscore (_)
- **Subsequent characters**: Letters, digits (0-9), or underscore
- Names are case-sensitive: `$var` and `$VAR` are different

### 6.3 Variable Expansion Prevention

Variables are NOT expanded when:

1. Token was single-quoted (`WasSingleQuoted: true`)
2. Dollar sign was escaped (`\$` becomes `\x01$`)

### 6.4 Special Dollar Sequences

These sequences are NOT expanded as variables (passed through literally):

| Sequence | Behavior |
|----------|----------|
| `$$` | Literal `$$` (no process ID expansion) |
| `$!` | Literal `$!` (no background PID expansion) |
| `$?` | Literal `$?` (no exit status expansion) |
| `$` at end | Literal `$` |
| `${}`| Literal `${}` (empty variable name) |

---

## 7. Subshell Recognition

**IMPORTANT**: The lexer does NOT perform subshell recognition or expansion. Subshell syntax is preserved as literal text and processed by the expander phase.

### 7.1 Subshell Syntax (Recognized by Expander)

| Syntax | Example | Description |
|--------|---------|-------------|
| `$(...)` | `$(date)`, `$(echo hello)` | Modern command substitution |
| `` `...` `` | `` `date` ``, `` `echo hello` `` | Legacy command substitution |

### 7.2 Nested Subshells

The expander supports nested subshells by processing innermost substitutions first:

```
$(echo $(date))
```

Processing order:
1. Find innermost `$(date)`
2. Execute `date`, replace with output
3. Find remaining `$(echo <output>)`
4. Execute, replace with final output

### 7.3 Subshell Expansion Prevention

Subshells are NOT expanded when:

1. Token was single-quoted (`WasSingleQuoted: true`)
2. Dollar sign was escaped (for `$(...)` syntax): `\$(date)` is not expanded
3. No executor is provided to the parser

---

## 8. Comment Handling

**Foundation Shell does NOT currently implement comment handling in the lexer.**

The `#` character is treated as a regular character and becomes part of tokens:

```
Input: echo hello # this is not a comment
Tokens: ["echo", "hello", "#", "this", "is", "not", "a", "comment"]
```

If comment support is added in the future, the typical shell behavior would be:

- `#` outside quotes begins a comment
- Everything from `#` to end of line is ignored
- `#` inside quotes is literal

---

## 9. Error Conditions

The lexer can produce the following errors:

### 9.1 Unclosed Single Quote

**Error**: `"unclosed single quote"`

**Cause**: Input contains an opening `'` without a matching closing `'`.

**Examples**:
```
echo 'hello          -> Error: unclosed single quote
'hello world         -> Error: unclosed single quote
echo 'it's fine'     -> Valid (middle ' closes first, opens second)
```

### 9.2 Unclosed Double Quote

**Error**: `"unclosed double quote"`

**Cause**: Input contains an opening `"` without a matching closing `"`.

**Examples**:
```
echo "hello          -> Error: unclosed double quote
"hello world         -> Error: unclosed double quote
echo "say \"hi\""    -> Valid (escaped quotes don't count)
```

### 9.3 Error Behavior

When an error occurs:
- The lexer returns `nil` for the token slice
- The error describes the problem
- No partial results are returned

```go
tokens, err := Tokenize(input)
if err != nil {
    // err.Error() == "unclosed single quote" or "unclosed double quote"
    // tokens == nil
}
```

---

## 10. Lexer Algorithm

### 10.1 State Variables

The lexer maintains these state variables:

| Variable | Type | Purpose |
|----------|------|---------|
| `tokens` | `[]TokenContext` | Accumulated tokens |
| `current` | `strings.Builder` | Current token being built |
| `wasSingleQuoted` | `bool` | Whether current token contains single-quoted content |
| `inSingleQuotes` | `bool` | Currently inside single quotes |
| `inDoubleQuotes` | `bool` | Currently inside double quotes |

### 10.2 Processing Loop

```
for each rune c in input:
    if c == '\\' AND NOT inSingleQuotes AND has next char:
        process escape sequence
        continue

    if c == '\'' AND NOT inDoubleQuotes:
        if NOT inSingleQuotes: wasSingleQuoted = true
        toggle inSingleQuotes
        continue

    if c == '"' AND NOT inSingleQuotes:
        toggle inDoubleQuotes
        continue

    if isSpace(c) AND NOT inSingleQuotes AND NOT inDoubleQuotes:
        if current has content:
            emit token
            reset current and wasSingleQuoted
        continue

    append c to current
```

### 10.3 Post-Loop Processing

After the loop:

1. Check for unclosed single quotes -> return error
2. Check for unclosed double quotes -> return error
3. If `current` has content, emit final token
4. Return token slice

### 10.4 Complexity

- **Time**: O(n) where n is input length - single pass
- **Space**: O(n) for storing tokens - worst case each character is a token

---

## 11. Integration with Parser

### 11.1 Parser's Use of Lexer Output

The parser calls `lexer.Tokenize(input)` and receives `[]TokenContext`. It then:

1. Iterates through tokens
2. Checks if each token is an operator using `parseOperator()`
3. For non-operators, applies expansions (tilde, environment, command substitution) unless `WasSingleQuoted`
4. Strips escape markers after expansion
5. Classifies as `Command` or `CommandArgument` based on position

### 11.2 Expansion Suppression

The `WasSingleQuoted` flag controls expansion:

```go
if !tc.WasSingleQuoted {
    expandedValue = expander.ExpandTilde(expandedValue)
    expandedValue = expander.ExpandEnvironment(expandedValue)
    // ... command substitution if executor provided
}
```

### 11.3 Escape Marker Stripping

After all expansions, escape markers are removed:

```go
expandedValue = lexer.StripEscapeMarkers(expandedValue)
```

This converts `\x01$VAR` to `$VAR` (literal dollar sign, not expanded).

---

## 12. Examples

### 12.1 Simple Command

```
Input:  ls -la /home/user
Lexer:  [{Content: "ls", WasSingleQuoted: false},
         {Content: "-la", WasSingleQuoted: false},
         {Content: "/home/user", WasSingleQuoted: false}]
Parser: [Command("ls"), CommandArgument("-la"), CommandArgument("/home/user")]
```

### 12.2 Quoted Arguments

```
Input:  git commit -m "Fix bug in parser"
Lexer:  [{Content: "git", WasSingleQuoted: false},
         {Content: "commit", WasSingleQuoted: false},
         {Content: "-m", WasSingleQuoted: false},
         {Content: "Fix bug in parser", WasSingleQuoted: false}]
Parser: [Command("git"), CommandArgument("commit"),
         CommandArgument("-m"), CommandArgument("Fix bug in parser")]
```

### 12.3 Single Quotes Preserve Literals

```
Input:  echo '$HOME is not expanded'
Lexer:  [{Content: "echo", WasSingleQuoted: false},
         {Content: "$HOME is not expanded", WasSingleQuoted: true}]
Parser: [Command("echo"), CommandArgument("$HOME is not expanded")]
// Note: $HOME remains literal due to WasSingleQuoted
```

### 12.4 Escaped Characters

```
Input:  echo hello\ world \$HOME
Lexer:  [{Content: "echo", WasSingleQuoted: false},
         {Content: "hello world", WasSingleQuoted: false},
         {Content: "\x01$HOME", WasSingleQuoted: false}]
Parser: [Command("echo"), CommandArgument("hello world"),
         CommandArgument("$HOME")]
// Note: Space preserved by escape, $HOME literal due to escape marker
```

### 12.5 Pipeline with Redirection

```
Input:  cat input.txt | grep error > output.log
Lexer:  [{Content: "cat", WasSingleQuoted: false},
         {Content: "input.txt", WasSingleQuoted: false},
         {Content: "|", WasSingleQuoted: false},
         {Content: "grep", WasSingleQuoted: false},
         {Content: "error", WasSingleQuoted: false},
         {Content: ">", WasSingleQuoted: false},
         {Content: "output.log", WasSingleQuoted: false}]
Parser: [Command("cat"), CommandArgument("input.txt"), Pipe,
         Command("grep"), CommandArgument("error"), RedirectStdOut,
         CommandArgument("output.log")]
```

### 12.6 Complex Quoting

```
Input:  echo "Hello, "'"'"$USER"'"'"!"
Lexer:  [{Content: "echo", WasSingleQuoted: false},
         {Content: "Hello, '\"$USER\"'!", WasSingleQuoted: true}]
// The complex quoting produces: Hello, '"$USER"'!
// WasSingleQuoted is true because single quotes were used
```

---

## 13. Differences from POSIX Shell

Foundation Shell's lexer differs from POSIX shell in several ways:

| Feature | POSIX Shell | Foundation Shell |
|---------|-------------|------------------|
| Operator tokenization | During lexing | During parsing |
| `#` comments | Supported | Not supported |
| Line continuation (`\newline`) | Joins lines | Not supported |
| Here-documents (`<<`) | Supported | Not supported |
| Process substitution (`<()`) | Bash extension | Not supported |
| Arithmetic expansion (`$(())`) | Supported | Not supported |
| Brace expansion (`{a,b}`) | Bash extension | Not supported |
| Word splitting after expansion | Supported | Not supported |
| Glob/pathname expansion | Supported | Not supported |

---

## 14. Future Considerations

Potential enhancements for the lexer:

1. **Comment Support**: Add `#` comment handling
2. **Line Continuation**: Support `\` at end of line to continue
3. **Here-Documents**: Support `<<` and `<<-` syntax
4. **Glob Preservation**: Mark tokens containing glob characters
5. **Position Tracking**: Add line/column information to tokens for better error messages

---

## 15. API Reference

### 15.1 Tokenize Function

```go
func Tokenize(input string) ([]TokenContext, error)
```

**Parameters:**
- `input`: The shell command string to tokenize

**Returns:**
- `[]TokenContext`: Slice of tokens with content and quoting metadata
- `error`: Non-nil if unclosed quotes detected

**Behavior:**
- Empty input returns `nil, nil` (no tokens, no error)
- Whitespace-only input returns `nil, nil`
- Unclosed quotes return `nil, error`

### 15.2 TokenContext Structure

```go
type TokenContext struct {
    Content         string  // Token text with escapes processed
    WasSingleQuoted bool    // True if any part was single-quoted
}
```

### 15.3 StripEscapeMarkers Function

```go
func StripEscapeMarkers(s string) string
```

**Parameters:**
- `s`: String potentially containing escape markers (`\x01`)

**Returns:**
- String with all `\x01` characters removed

### 15.4 EscapeMarker Constant

```go
const EscapeMarker = '\x01'  // ASCII SOH (Start of Heading)
```

Used to mark escaped dollar signs that should not be expanded.
