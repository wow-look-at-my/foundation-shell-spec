---
title: Lexer Specification
description: Tokenization rules, operator recognition, command-substitution-aware scanning, escape sequences, and quote state tracking.
---

# Lexer Specification

> **Canonical for:** tokenization — word/token vocabulary, word boundaries, operator lexing, escape processing, quote state, and command-substitution scanning. The authority map lives in the README.

## 1. Overview

The lexer (tokenizer) is the first stage of Foundation Shell's input processing pipeline. It transforms raw input strings into a sequence of tokens that preserve semantic information about quoting context. The lexer operates as a single-pass scanner that handles escape sequences, quote state tracking, operator recognition, command-substitution scanning, and whitespace-based token separation.

### 1.1 Design Philosophy

Foundation Shell's lexer follows these principles:

1. **Quote Context Preservation**: Tokens retain metadata about whether any part was quoted (`WasQuoted`) and whether any part was single-quoted (`WasSingleQuoted`), enabling downstream components to decide whether to perform expansions.
2. **Escape Marker System**: Escaped dollar signs and escaped backticks are marked with a special character (`\x01`) to prevent expansion while preserving the original intent.
3. **Operator Recognition**: The lexer recognizes operators directly (maximal munch, no whitespace required) and marks the resulting tokens with `IsOperator`. The parser maps marked operator tokens to formal token types and never re-derives operators from token content.
4. **Command-Substitution Awareness**: The lexer tracks `$(...)` parenthesis depth and backtick state so that an entire substitution — including embedded whitespace and quotes — stays in one token. Substitution bodies are preserved verbatim and re-parsed recursively when the substitution executes.
5. **Unicode Support**: The lexer operates on runes, supporting full Unicode input.

### 1.2 Processing Pipeline Position

```
Input String
     |
     v
  [LEXER] --> TokenContext[] (Content + WasSingleQuoted + WasQuoted + IsOperator)
     |
     v
  [PARSER] --> Operator mapping, expansion, classification
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
    Content         string  // The token's text content (escapes processed, quote delimiters removed)
    WasSingleQuoted bool    // True if any part was inside single quotes
    WasQuoted       bool    // True if any part was inside any quotes (single or double)
    IsOperator      bool    // True if the lexer recognized this token as an operator
}
```

Downstream consumers of the flags:

| Flag | Consumer | Effect |
|------|----------|--------|
| `WasSingleQuoted` | Expander | Suppresses ALL expansion for the token |
| `WasQuoted` | Expander | Suppresses tilde expansion for the token |
| `IsOperator` | Parser | The ONLY basis for classifying a token as an operator |

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

### 2.3 Word and Token Vocabulary

These two terms are used throughout the specification and are defined here once:

- A **word** is a maximal run of input characters between word boundaries (§3.2), before any classification. Quote delimiters and escape sequences are part of the word as written, and are processed while the word is accumulated.
- A **token** is the `TokenContext` the lexer emits — either the processed content of a word, or a recognized operator.

The parser's classified tokens and the analyzer's `AnalyzedToken`s (highlighting.md) are distinct downstream structures. When other specification files say "token" without qualification, they mean the lexer's `TokenContext`.

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
| Outside quotes and substitutions | Terminates current token, starts new token |
| Inside single quotes | Preserved literally as part of token |
| Inside double quotes | Preserved literally as part of token |
| Inside `$(...)` or backticks | Preserved literally as part of token |
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

Input: "echo $(echo hello world)"
Tokens: ["echo", "$(echo hello world)"]
```

### 3.2 Word Boundaries

A word boundary occurs when:

1. Unquoted whitespace outside any command substitution is encountered
2. An operator is recognized (§3.3)
3. End of input is reached

Words are accumulated character-by-character until a boundary is reached. At EVERY word boundary the per-word flags (`WasSingleQuoted`, `WasQuoted`) are reset — whether or not a token was emitted at that boundary. (A run of whitespace produces no token but still resets the flags; this guarantees that `echo '' $HOME` expands `$HOME`.)

### 3.3 Operator Recognition

The lexer recognizes operators directly. Operators do NOT require whitespace separation from adjacent words.

When the lexer encounters an unquoted, unescaped operator character (`|`, `&`, `;`, `<`, `>`) outside any command substitution, it:

1. Flushes the current word (if any) as a token
2. Lexes the operator by maximal munch over the operator set: `||`, `&&`, `>>`, `2>>`, `2>`, `|`, `;`, `<`, `>`
3. Emits the operator as a token with `IsOperator: true`

Maximal munch means the longest operator is matched first: `2>>` before `>>` before `>`, and `&&`/`||` before `|`.

#### 3.3.1 The `2>` / `2>>` Rule

`2>` and `2>>` are recognized only when the pending word is exactly an unquoted, unescaped `2`. The `2` is consumed into the operator token instead of being emitted as a word. In every other case `>` / `>>` are lexed on their own:

```
echo 2>err.log      -> [echo, 2>, err.log]       (stderr redirect)
echo 2>>err.log     -> [echo, 2>>, err.log]      (stderr append)
echo a2>out.log     -> [echo, a2, >, out.log]    (word "a2", stdout redirect)
echo 22>out.log     -> [echo, 22, >, out.log]    (word "22", stdout redirect)
echo "2">out.log    -> [echo, 2, >, out.log]     (quoted "2" is an argument)
echo 2 >out.log     -> [echo, 2, >, out.log]     (whitespace separates the 2)
```

#### 3.3.2 Single `&` Is Not an Operator

Foundation Shell has no background jobs. A lone `&` is a literal word character; only the two-character `&&` is an operator:

```
echo a&b     -> [echo, a&b]          (one word)
echo a&&b    -> [echo, a, &&, b]
```

#### 3.3.3 Examples

```
echo hello | grep world   -> [echo, hello, |, grep, world]
echo hello|grep world     -> [echo, hello, |, grep, world]
                             (identical: "hello|grep" lexes as three tokens
                              hello, |, grep)
echo>file                 -> [echo, >, file]
cat<in>out                -> [cat, <, in, >, out]
```

#### 3.3.4 Quoting and Escaping Defeat Operator Recognition

An operator character inside quotes is literal content, and an escaped operator character outside quotes is a literal character — never an operator:

```
echo "|"    -> [echo, {Content: "|", IsOperator: false}]   literal argument
echo '>'    -> [echo, {Content: ">", IsOperator: false}]   literal argument
echo \|     -> [echo, {Content: "|", IsOperator: false}]   literal argument
```

The parser maps operator tokens to formal token types by content (see parser.md), but ONLY for tokens the lexer marked with `IsOperator: true`.

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
| Sets WasSingleQuoted flag | Token marked for expansion suppression (WasQuoted is also set) |

#### 4.1.2 Single Quote Examples

```
Input: echo 'hello world'
Tokens: [{Content: "echo"},
         {Content: "hello world", WasSingleQuoted: true, WasQuoted: true}]

Input: echo 'hello $VAR \n world'
Tokens: [{Content: "echo"},
         {Content: "hello $VAR \n world", WasSingleQuoted: true, WasQuoted: true}]

Input: echo 'back\\slash'
Tokens: [{Content: "echo"},
         {Content: "back\\slash", WasSingleQuoted: true, WasQuoted: true}]
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
| Sets WasQuoted flag | Tilde expansion suppressed; other expansions still eligible |
| Does NOT set WasSingleQuoted | Token eligible for variable/command expansion |

#### 4.2.2 Double Quote Examples

```
Input: echo "hello world"
Tokens: [{Content: "echo"},
         {Content: "hello world", WasQuoted: true}]

Input: echo "say \"hello\""
Tokens: [{Content: "echo"},
         {Content: "say \"hello\"", WasQuoted: true}]

Input: echo "path\\to\\file"
Tokens: [{Content: "echo"},
         {Content: "path\to\file", WasQuoted: true}]
```

### 4.3 Backticks (`` `...` ``)

Backticks delimit command substitution. The lexer tracks backtick state (§7): an unescaped backtick outside single quotes opens a substitution and the next one closes it. Between the two, the body — including whitespace and quote characters — is preserved verbatim in the token.

#### 4.3.1 Backtick Behavior in Lexer

```
Input: echo `date +%Y %m`
Tokens: [{Content: "echo"},
         {Content: "`date +%Y %m`"}]
```

The backtick-delimited content remains in the token; the expander executes it later (see §7 and expansion.md).

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

If ANY part of a concatenated token was single-quoted, the entire token is marked as `WasSingleQuoted: true`. This is a conservative approach to prevent unintended expansion. Likewise, if any part was inside any quotes, the token is marked `WasQuoted: true`.

```
Input: echo 'single'"double"unquoted
Tokens: [{Content: "echo"},
         {Content: "singledoubleunquoted", WasSingleQuoted: true, WasQuoted: true}]
```

### 4.5 Empty Quoted Strings

Empty quoted strings (`""` or `''`) produce a token with empty content — empty arguments ARE representable (as in POSIX shells):

```
Input: echo ""
Tokens: [{Content: "echo"},
         {Content: "", WasQuoted: true}]

Input: echo ''
Tokens: [{Content: "echo"},
         {Content: "", WasSingleQuoted: true, WasQuoted: true}]

Input: ''
Tokens: [{Content: "", WasSingleQuoted: true, WasQuoted: true}]
```

A quoted-empty word is still a word: seeing a quote pair makes the current word exist, so a token is emitted at the next boundary even though it has no content. Whitespace runs alone still produce no token.

---

## 5. Escape Sequences

Escape processing occurs outside single quotes. The backslash character (`\`) initiates an escape sequence. (Inside command-substitution bodies, escape sequences are preserved verbatim instead of being processed — see §7.2.)

[include:_partials/escape-sequences.md](_partials/escape-sequences.md)

### 5.1 The Escape Marker System

When `\$` or `` \` `` is encountered (outside single quotes and outside substitution bodies), the lexer produces the two-character sequence `\x01$` (or `` \x01` ``) rather than just the bare character. This "escape marker" (`\x01`, ASCII SOH) serves as a flag to the expander that this dollar sign or backtick should NOT trigger expansion or command substitution.

#### 5.1.1 Escape Marker Constant

```go
const EscapeMarker = '\x01'  // ASCII Start of Heading
```

#### 5.1.2 Escape Marker Lifecycle

1. **Lexer**: `\$VAR` becomes `\x01$VAR`; `` \`date\` `` becomes `` \x01`date\x01` ``
2. **Expander**: Sees `\x01$` / `` \x01` ``, skips expansion, keeps the character literal
3. **Post-expansion**: `StripEscapeMarkers()` removes any remaining `\x01` characters

#### 5.1.3 StripEscapeMarkers Function

```go
func StripEscapeMarkers(s string) string {
    return strings.ReplaceAll(s, string(EscapeMarker), "")
}
```

#### 5.1.4 Reserved Marker Byte (Known Limitation)

U+0001 is reserved for internal use. `StripEscapeMarkers` removes EVERY U+0001 byte, and the expander treats `\x01$` / `` \x01` `` as escape markers regardless of origin. Input that itself contains a literal U+0001 character (typed via Ctrl-V Ctrl-A, or embedded in a script) therefore has **undefined behavior**. U+0001 is formally outside the supported input alphabet.

### 5.2 Escape Examples

```
Input: echo hello\ world
Tokens: [{Content: "echo"},
         {Content: "hello world"}]

Input: echo hello\\world
Tokens: [{Content: "echo"},
         {Content: "hello\world"}]

Input: echo \$HOME
Tokens: [{Content: "echo"},
         {Content: "\x01$HOME"}]

Input: echo \`date\`
Tokens: [{Content: "echo"},
         {Content: "\x01`date\x01`"}]
// The marked backticks are NOT executed; after expansion and marker
// stripping the argument is `date` (literal backticks)

Input: echo \"hello\"
Tokens: [{Content: "echo"},
         {Content: "\"hello\""}]

Input: echo hello\nworld
Tokens: [{Content: "echo"},
         {Content: "hello\nworld"}]
// Note: \n becomes actual newline character (0x0A)
```

### 5.3 Trailing Backslash

A backslash at the end of input (with nothing to escape) is preserved literally:

```
Input: echo hello\
Tokens: [{Content: "echo"},
         {Content: "hello\"}]
```

---

## 6. Variable Recognition

**IMPORTANT**: The lexer does NOT perform variable expansion. Variables (`$VAR`, `${VAR}`) are preserved as literal text in tokens. Variable expansion is handled by the expander phase (see expansion.md).

[include:_partials/variable-syntax.md](_partials/variable-syntax.md)

### 6.1 Variable Expansion Prevention

Variables are NOT expanded when:

1. Token was single-quoted (`WasSingleQuoted: true`)
2. Dollar sign was escaped (`\$` becomes `\x01$`)

---

## 7. Command Substitution Recognition

The lexer recognizes command-substitution **delimiters** so that an entire substitution stays inside one token. It does NOT execute or expand substitutions — that happens in the expander phase (see expansion.md).

[include:_partials/command-substitution-syntax.md](_partials/command-substitution-syntax.md)

### 7.1 Substitution Scanning Rules

1. An unescaped `$(` outside single quotes opens a command substitution and increments the parenthesis depth. `$(...)` may nest: each `$(` inside an open substitution increments the depth again.
2. An unescaped backtick outside single quotes toggles backtick state: it opens a substitution if none is open, and closes the open one otherwise. (For backtick nesting semantics see quoting.md §6.3; escaped backticks — `` \` `` — are literal at this level and take effect when the body is re-parsed.)
3. While inside an open substitution (parenthesis depth > 0, or backtick open), the body is preserved **verbatim**:
   - Whitespace does NOT split tokens; it is copied into the token.
   - Quote characters (`'`, `"`) are copied verbatim — they are not removed and they do NOT set the token's `WasQuoted`/`WasSingleQuoted` flags. They do update the body's own quote state (rule 4).
   - Backslash escape sequences are copied verbatim (backslash retained). The escaped character is skipped for state purposes: it cannot close the substitution, toggle body quote state, or start a nested substitution.
   - Operator characters are NOT recognized; they are body content.

   The body takes effect when the substitution executes: the expander re-parses the body recursively with the full lexer (quotes, escapes, operators and all). This is why the body must be preserved exactly as written.
4. Finding the closing delimiter — body quote state:
   - Each `$(` begins a fresh quote context. The enclosing context (for example an open double quote around the whole substitution) is saved and restored when the substitution closes. `echo "$(date)"` is therefore valid: the `)` closes the substitution even though the outer double quote is still open.
   - A `)` closes the innermost open `$(` only when the body's own single-quote and double-quote counts are even and no backtick opened inside the body is still open. Otherwise the `)` is body content. Both `echo $(echo ")")` and `echo $(echo ')')` are valid.
   - Inside an open backtick substitution, single quotes are literal (they do not open a quote context — see quoting.md §5.3) and nothing except an unescaped backtick closes the body. Use `` \` `` for a literal backtick inside a backtick body.
5. Substitutions still open at end of input are lexer errors, using the same canonical strings as the analyzer (§9): `unclosed command substitution $(...)` and `unclosed backtick`.

### 7.2 Examples

```
Input: echo $(echo hello world)
Tokens: [{Content: "echo"}, {Content: "$(echo hello world)"}]
// Whitespace inside the substitution does not split the token

Input: echo $(echo "a  b")
Tokens: [{Content: "echo"}, {Content: "$(echo \"a  b\")"}]
// The double quotes stay in the body; when executed, the inner command
// preserves the two spaces

Input: echo $(echo ")")
Tokens: [{Content: "echo"}, {Content: "$(echo \")\")"}]
// The quoted ) does not close the substitution

Input: echo "$(date)"
Tokens: [{Content: "echo"}, {Content: "$(date)", WasQuoted: true}]
// The OUTER double quotes are delimiters (removed, flag set); the
// substitution closes normally inside them
```

### 7.3 Nested Substitutions

The expander processes nested substitutions innermost-first:

```
$(echo $(date))
```

Processing order:
1. Find innermost `$(date)`
2. Execute `date`, replace with output
3. Find remaining `$(echo <output>)`
4. Execute, replace with final output

### 7.4 Substitution Expansion Prevention

Command substitutions are NOT expanded when:

1. Token was single-quoted (`WasSingleQuoted: true`)
2. The introducing character was escaped: `\$(date)` and `` \`date\` `` are not expanded (escape marker, §5.1)
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

The lexer reports scanning errors using the SAME canonical strings as the syntax analyzer for the same conditions. The canonical error-string table lives in diagnostics.md §5; the lexer can produce these four:

| Condition | Error string |
|-----------|--------------|
| Unclosed single quote at end of input | `unclosed single quote` |
| Unclosed double quote at end of input | `unclosed double quote` |
| Backtick substitution still open at end of input | `unclosed backtick` |
| `$(` depth > 0 at end of input | `unclosed command substitution $(...)` |

### 9.1 Examples

```
echo 'hello          -> Error: unclosed single quote
echo 'it'\''s ok'    -> Valid (the escaped \' does not count as a quote)
echo "hello          -> Error: unclosed double quote
echo "say \"hi\""    -> Valid (escaped quotes don't count)
echo $(date          -> Error: unclosed command substitution $(...)
echo `date           -> Error: unclosed backtick
```

### 9.2 Error Behavior

When an error occurs:
- The lexer returns `nil` for the token slice
- The error message is exactly one of the canonical strings above
- No partial results are returned

```go
tokens, err := Tokenize(input)
if err != nil {
    // err.Error() is one of the four canonical strings above
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
| `current` | `strings.Builder` | Current word being built |
| `sawWord` | `bool` | A word exists since the last boundary (content appended OR a quote pair seen) |
| `wasSingleQuoted` | `bool` | Current word contains single-quoted content |
| `wasQuoted` | `bool` | Current word contains quoted content (any quote type) |
| `inSingleQuotes` | `bool` | Single quote open in the CURRENT context |
| `inDoubleQuotes` | `bool` | Double quote open in the CURRENT context |
| `inBacktickBody` | `bool` | Current context is a backtick substitution body |
| `dollarParenDepth` | `int` | Open `$(` count |
| `contextStack` | stack | Saved quote state per substitution level (§7.1 rule 4) |

### 10.2 Processing Loop

```
for each rune c in input:
    inBody := dollarParenDepth > 0 OR inBacktickBody

    if c == '\\' AND NOT inSingleQuotes AND has next char:
        if inBody: append '\\' and the next char verbatim (no conversion)
        else:      append the escape result (§5; \$ and \` gain the marker)
        skip next char; sawWord = true
        continue

    if c == '$' AND next char == '(' AND NOT inSingleQuotes:
        push {inSingleQuotes, inDoubleQuotes, inBacktickBody}; clear all three
        dollarParenDepth++
        append "$("; skip '('; sawWord = true
        continue

    if c == ')' AND dollarParenDepth > 0
              AND NOT inSingleQuotes AND NOT inDoubleQuotes AND NOT inBacktickBody:
        dollarParenDepth--
        pop context (restore enclosing quote state)
        append ')'
        continue

    if c == '`' AND NOT inSingleQuotes:
        if inBacktickBody:  pop context           // closing backtick
        else:               push context; clear; inBacktickBody = true
        append '`'; sawWord = true
        continue

    if c == '\'' AND NOT inDoubleQuotes AND NOT inBacktickBody:
        toggle inSingleQuotes
        if inBody: append '\''                    // body content, verbatim, no flags
        else:
            sawWord = true                        // delimiter not appended
            wasSingleQuoted = true; wasQuoted = true
        continue

    if c == '"' AND NOT inSingleQuotes:
        toggle inDoubleQuotes
        if inBody: append '"'                     // body content, verbatim, no flags
        else:
            sawWord = true                        // delimiter not appended
            wasQuoted = true
        continue

    if c is an operator character (| & ; < >)
              AND NOT inSingleQuotes AND NOT inDoubleQuotes AND NOT inBody:
        if c == '&' AND next char != '&':
            fall through                          // lone & is a literal character
        else:
            flush current word (emit token if sawWord)
            reset current, flags, sawWord
            lex operator by maximal munch (§3.3), applying the 2>/2>> rule
            emit {Content: operator, IsOperator: true}
            continue

    if isSpace(c) AND NOT inSingleQuotes AND NOT inDoubleQuotes AND NOT inBody:
        if sawWord: emit {Content: current, wasSingleQuoted, wasQuoted}
        reset current, flags, sawWord             // flags reset even if nothing emitted
        continue

    append c to current; sawWord = true
```

### 10.3 Post-Loop Processing

After the loop:

1. `inSingleQuotes` -> error `unclosed single quote`
2. `inDoubleQuotes` -> error `unclosed double quote`
3. `inBacktickBody` -> error `unclosed backtick`
4. `dollarParenDepth > 0` -> error `unclosed command substitution $(...)`
5. If `sawWord`, emit the final token
6. Return token slice

### 10.4 Complexity

- **Time**: O(n) where n is input length - single pass
- **Space**: O(n) for storing tokens - worst case each character is a token

---

## 11. Integration with Parser

### 11.1 Parser's Use of Lexer Output

The parser calls `lexer.Tokenize(input)` and receives `[]TokenContext`. It then:

1. Maps tokens marked `IsOperator: true` to formal operator types by content. The parser NEVER re-derives operators from content: a token whose `Content` is `|` but whose `IsOperator` is `false` (because it was quoted or escaped) is an ordinary value token.
2. For value tokens, applies expansions (tilde, environment, command substitution) unless `WasSingleQuoted`; tilde expansion is additionally suppressed by `WasQuoted`
3. Strips escape markers — unconditionally, for every value token
4. Classifies as `Command` or `CommandArgument` based on position

### 11.2 Expansion Suppression

The quoting flags control expansion:

```go
if !tc.WasSingleQuoted {
    if !tc.WasQuoted {
        expandedValue = expander.ExpandTilde(expandedValue)
    }
    expandedValue = expander.ExpandEnvironment(expandedValue)
    // ... command substitution if executor provided
}
// Marker stripping happens AFTER this block, for every value token (§11.3)
```

### 11.3 Escape Marker Stripping

After all expansions, escape markers are removed. Stripping is UNCONDITIONAL — it applies to every value token, including single-quoted tokens (a concatenation such as `'a'\$HOME` carries a marker inside a `WasSingleQuoted` token):

```go
expandedValue = lexer.StripEscapeMarkers(expandedValue)
```

This converts `\x01$VAR` to `$VAR` (literal dollar sign, not expanded).

---

## 12. Examples

### 12.1 Simple Command

```
Input:  ls -la /home/user
Lexer:  [{Content: "ls"},
         {Content: "-la"},
         {Content: "/home/user"}]
Parser: [Command("ls"), CommandArgument("-la"), CommandArgument("/home/user")]
```

### 12.2 Quoted Arguments

```
Input:  git commit -m "Fix bug in parser"
Lexer:  [{Content: "git"},
         {Content: "commit"},
         {Content: "-m"},
         {Content: "Fix bug in parser", WasQuoted: true}]
Parser: [Command("git"), CommandArgument("commit"),
         CommandArgument("-m"), CommandArgument("Fix bug in parser")]
```

### 12.3 Single Quotes Preserve Literals

```
Input:  echo '$HOME is not expanded'
Lexer:  [{Content: "echo"},
         {Content: "$HOME is not expanded", WasSingleQuoted: true, WasQuoted: true}]
Parser: [Command("echo"), CommandArgument("$HOME is not expanded")]
// Note: $HOME remains literal due to WasSingleQuoted
```

### 12.4 Escaped Characters

```
Input:  echo hello\ world \$HOME
Lexer:  [{Content: "echo"},
         {Content: "hello world"},
         {Content: "\x01$HOME"}]
Parser: [Command("echo"), CommandArgument("hello world"),
         CommandArgument("$HOME")]
// Note: Space preserved by escape, $HOME literal due to escape marker
```

### 12.5 Pipeline with Redirection

```
Input:  cat input.txt | grep error>output.log
Lexer:  [{Content: "cat"},
         {Content: "input.txt"},
         {Content: "|", IsOperator: true},
         {Content: "grep"},
         {Content: "error"},
         {Content: ">", IsOperator: true},
         {Content: "output.log"}]
Parser: [Command("cat"), CommandArgument("input.txt"), Pipe,
         Command("grep"), CommandArgument("error"), RedirectStdOut,
         CommandArgument("output.log")]
// The operators are recognized with or without surrounding whitespace
```

### 12.6 Complex Quoting

```
Input:  echo "Hello, "'"'"$USER"'"'"!"
Lexer:  [{Content: "echo"},
         {Content: "Hello, \"$USER\"!", WasSingleQuoted: true, WasQuoted: true}]
// Segments: "Hello, " -> Hello,_   '"' -> "   "$USER" -> $USER   '"' -> "   "!" -> !
// All quote DELIMITERS are removed; the single-quoted segments contribute
// literal " characters. WasSingleQuoted is true, so NO expansion occurs.
// The argument is exactly:  Hello, "$USER"!
```

---

## 13. Differences from POSIX Shell

Foundation Shell's lexer differs from POSIX shell in several ways:

| Feature | POSIX Shell | Foundation Shell |
|---------|-------------|------------------|
| `&` background operator | Supported | Not an operator (literal character) |
| `#` comments | Supported | Not supported |
| Line continuation (`\newline`) | Joins lines | Not supported |
| Here-documents (`<<`) | Supported | Not supported |
| Process substitution (`<()`) | Bash extension | Not supported |
| Arithmetic expansion (`$(())`) | Supported | Not supported |
| Brace expansion (`{a,b}`) | Bash extension | Not supported |
| Word splitting after expansion | Supported | Not supported |
| Glob/pathname expansion | Supported | Not supported |

For quoting-level differences from POSIX (quote nesting and related behavior), see quoting.md §13.

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
- `[]TokenContext`: Slice of tokens with content, quoting metadata, and operator marking
- `error`: Non-nil if an unclosed quote or unclosed command substitution is detected

**Behavior:**
- Empty input returns `nil, nil` (no tokens, no error)
- Whitespace-only input returns `nil, nil`
- Unclosed quotes or substitutions return `nil, error` (canonical strings, §9)

### 15.2 TokenContext Structure

```go
type TokenContext struct {
    Content         string  // Token text (escapes processed, quote delimiters removed)
    WasSingleQuoted bool    // True if any part was single-quoted
    WasQuoted       bool    // True if any part was inside any quotes
    IsOperator      bool    // True if the lexer recognized this token as an operator
}
```

### 15.3 StripEscapeMarkers Function

```go
func StripEscapeMarkers(s string) string
```

**Parameters:**
- `s`: String potentially containing escape markers (`\x01`)

**Returns:**
- String with all `\x01` characters removed (see §5.1.4 for the reserved-byte caveat)

### 15.4 EscapeMarker Constant

```go
const EscapeMarker = '\x01'  // ASCII SOH (Start of Heading)
```

Used to mark escaped dollar signs and escaped backticks that should not be expanded.
