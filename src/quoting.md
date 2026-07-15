---
title: Quoting Specification
description: Quote handling for single, double, and backtick quotes with depth tracking for nested constructs.
recommend_after: lexer.md
---

# Quoting Specification

> **Canonical for:** quote semantics (what quoting means for tokens and expansion), quote depth tracking, and the quote-isolation rules. For tokenization mechanics see lexer.md; the authority map lives in the README.

## 1. Overview

Foundation Shell implements a quoting system that controls how text is interpreted, whether expansions occur, and how special characters are treated.

### 1.1 Quoting Mechanisms

Foundation Shell supports four quoting mechanisms:

| Mechanism | Syntax | Purpose |
|-----------|--------|---------|
| Single Quotes | `'...'` | Strong quoting - literal text, no expansion |
| Double Quotes | `"..."` | Weak quoting - preserves whitespace, allows expansion |
| Backticks | `` `...` `` | Command substitution (legacy syntax) |
| Backslash | `\X` | Escape single character |

### 1.2 Processing Pipeline

Quoting affects multiple stages of the processing pipeline:

```
Input String
     |
     v
  [ANALYZER] --> Syntax validation, depth tracking, error detection
     |
     v
  [LEXER] --> Tokenization, escape processing, quote removal
     |
     v
  [EXPANDER] --> Variable/tilde/command expansion (if not single-quoted)
     |
     v
  [PARSER] --> Command chain construction
```

### 1.3 Two Analysis Systems

Foundation Shell has two complementary systems for handling quotes:

1. **Syntax Analyzer** (`syntax/analyzer.go`): Performs depth-tracked quote validation for syntax highlighting and error detection. Uses even/odd count algorithm.

2. **Lexer** (`lexer/lexer.go`): Performs actual tokenization with quote state tracking. Uses traditional toggle-based state machine.

---

## 2. Single Quotes

Single quotes provide **strong quoting** - the strongest form of literal text preservation.

### 2.1 Fundamental Rules

| Rule | Description |
|------|-------------|
| **Literal Preservation** | Every character between single quotes is preserved exactly |
| **No Escape Processing** | Backslash has no special meaning: `'\n'` is two characters |
| **No Variable Expansion** | Dollar signs are literal: `'$HOME'` remains `$HOME` |
| **No Command Substitution** | Backticks and `$()` are literal |
| **Whitespace Preservation** | Spaces and tabs do not split tokens |
| **Quote Removal** | The quote characters themselves are removed from output |

### 2.2 Single Quote Behavior

```
Input:  echo 'hello world'
Output: hello world

Input:  echo 'hello $HOME'
Output: hello $HOME

Input:  echo 'hello\nworld'
Output: hello\nworld    (literal backslash-n, not newline)

Input:  echo 'back\\slash'
Output: back\\slash     (literal double backslash)
```

### 2.3 What Cannot Be Done

**A single quote cannot appear inside single quotes.** There is no escape mechanism within single quotes.

```
# INVALID - Cannot include literal single quote
echo 'it's broken'    # Syntax error or unexpected behavior

# WORKAROUND - End quotes, add escaped quote, restart quotes
echo 'it'\''s working'
# Produces: it's working
```

The workaround works by:
1. `'it'` - single-quoted "it"
2. `\'` - escaped single quote (outside single quotes)
3. `'s working'` - single-quoted "s working"

These concatenate into a single token: `it's working`

### 2.4 WasSingleQuoted Flag

The lexer tracks whether any part of a token was single-quoted via the `WasSingleQuoted` flag in `TokenContext` (see lexer.md §2.1; single-quoted content also sets the broader `WasQuoted` flag):

```go
type TokenContext struct {
    Content         string
    WasSingleQuoted bool  // True if ANY part was inside single quotes
    WasQuoted       bool  // True if ANY part was inside any quotes
    IsOperator      bool
}
```

This flag is used by the expander to suppress all expansions:

```go
func Expand(token string, wasSingleQuoted bool) string {
    if wasSingleQuoted {
        return token  // No expansion performed
    }
    // ... perform expansions
}
```

### 2.5 Conservative Single-Quote Propagation

If ANY part of a concatenated token was single-quoted, the entire token is marked as single-quoted:

```
Input:  echo 'single'"double"unquoted
Token:  {Content: "singledoubleunquoted", WasSingleQuoted: true, WasQuoted: true}
```

This conservative approach prevents unintended expansion of the double-quoted and unquoted portions.

---

## 3. Double Quotes

Double quotes provide **weak quoting** - whitespace preservation with selective expansion.

### 3.1 Fundamental Rules

| Rule | Description |
|------|-------------|
| **Whitespace Preservation** | Spaces and tabs do not split tokens |
| **Escape Processing** | Backslash sequences are interpreted |
| **Variable Expansion Allowed** | `$VAR` and `${VAR}` will be expanded |
| **Command Substitution Allowed** | `$(cmd)` and `` `cmd` `` will be expanded |
| **Tilde Expansion Suppressed** | Sets `WasQuoted`, which suppresses tilde expansion for the whole token |
| **Single Quotes Literal** | Single quote character has no special meaning |
| **Quote Removal** | The quote characters themselves are removed from output |

### 3.2 Double Quote Behavior

```
Input:  echo "hello world"
Output: hello world

Input:  echo "hello $USER"
Output: hello johndoe    (assuming USER=johndoe)

Input:  echo "Today is $(date)"
Output: Today is Mon Jan 12 10:30:00 UTC 2026

Input:  echo "it's fine"
Output: it's fine    (single quote is literal)
```

### 3.3 Escape Sequences in Double Quotes

Inside double quotes, backslash escapes are processed exactly as outside quotes. The canonical table:

[include:_partials/escape-sequences.md](_partials/escape-sequences.md)

### 3.4 Including Double Quotes

Double quotes can appear inside double-quoted strings using escape:

```
Input:  echo "say \"hello\""
Output: say "hello"

Input:  echo "path is \"$HOME\""
Output: path is "/home/user"
```

### 3.5 Preventing Expansion

To include a literal `$` that should not trigger expansion:

```
Input:  echo "cost is \$100"
Output: cost is $100
```

The escape marker system (`\x01$`) ensures the dollar sign is not expanded.

---

## 4. Backticks (Command Substitution)

Backticks provide legacy command substitution syntax.

### 4.1 Fundamental Rules

| Rule | Description |
|------|-------------|
| **Command Execution** | Content between backticks is executed as a command |
| **Output Substitution** | Command output replaces the backtick expression |
| **Trailing Newline Trimming** | Trailing newlines are removed from output |
| **Not in Single Quotes** | Backticks inside single quotes are literal |

### 4.2 Backtick Behavior

```
Input:  echo `date`
Output: Mon Jan 12 10:30:00 UTC 2026

Input:  echo "Today is `date`"
Output: Today is Mon Jan 12 10:30:00 UTC 2026

Input:  echo 'Today is `date`'
Output: Today is `date`    (literal - single quotes)
```

### 4.3 Modern Alternative: $()

[include:_partials/command-substitution-syntax.md](_partials/command-substitution-syntax.md)

```
# Both produce the same result
echo `date`
echo $(date)

# Nesting is cleaner with $()
echo $(echo $(date))     # Easy
echo `echo \`date\``     # Harder to read
```

### 4.4 Backtick Depth Tracking

The syntax analyzer tracks backtick depth for validation:

```go
if c == '`' && singleQuoteDepth == 0 {
    if backtickDepth%2 == 0 {
        // Opening backtick
    } else {
        // Closing backtick
    }
    backtickDepth++
}
```

An odd backtick count at end of input indicates an unclosed backtick:

```
Input:  echo `date
Error:  unclosed backtick (odd count)
```

---

## 5. Quote Depth Tracking

Foundation Shell uses a sophisticated depth-tracking algorithm for quote validation.

### 5.1 The Even/Odd Count Algorithm

For each quote type, the analyzer maintains a depth counter:

- **Even count (0, 2, 4, ...)**: Valid - all quotes are balanced
- **Odd count (1, 3, 5, ...)**: Invalid - unclosed quote

```go
// Single quote depth tracking
if c == '\'' && doubleQuoteDepth == 0 && backtickDepth == 0 {
    if singleQuoteDepth%2 == 0 {
        // Opening quote - depth goes 0->1, 2->3, etc.
        quoteStarts = append(quoteStarts, a.pos)
    } else {
        // Closing quote - depth goes 1->2, 3->4, etc.
        if len(quoteStarts) > 0 {
            quoteStarts = quoteStarts[:len(quoteStarts)-1]
        }
    }
    singleQuoteDepth++
}
```

### 5.2 Depth Counter Behavior

| Single Quote | Depth | State |
|--------------|-------|-------|
| (start) | 0 | Outside |
| `'` | 1 | Inside |
| `'` | 2 | Outside (one pair complete) |
| `'` | 3 | Inside (second pair started) |
| `'` | 4 | Outside (two pairs complete) |

### 5.3 Quote Isolation Rules

Quotes are isolated from each other based on nesting context:

| Context | Quote Behavior |
|---------|---------------|
| Single quotes active | Double quotes and backticks are literal |
| Double quotes active | Single quotes are literal, backticks active |
| Backticks active | Single quotes are literal, double quotes active |

```go
// Single quotes only active when NOT in double quotes or backticks
if c == '\'' && doubleQuoteDepth == 0 && backtickDepth == 0 { ... }

// Double quotes only active when NOT in single quotes or backticks
if c == '"' && singleQuoteDepth == 0 && backtickDepth == 0 { ... }

// Backticks only active when NOT in single quotes
if c == '`' && singleQuoteDepth == 0 { ... }
```

### 5.4 Outside Quotes Detection

A position is "outside all quotes" when all depth counters have even values:

```go
outsideQuotes := singleQuoteDepth%2 == 0 &&
                 doubleQuoteDepth%2 == 0 &&
                 backtickDepth%2 == 0 &&
                 parenDepth == 0
```

This is used to determine:
- Whether whitespace terminates a token
- Whether operators are recognized
- Whether the input is syntactically complete

### 5.5 Validation at End of Input

At end of input, all depths must be even:

```go
if singleQuoteDepth%2 != 0 {
    errors = append(errors, "unclosed single quote (odd count)")
}
if doubleQuoteDepth%2 != 0 {
    errors = append(errors, "unclosed double quote (odd count)")
}
if backtickDepth%2 != 0 {
    errors = append(errors, "unclosed backtick (odd count)")
}
if parenDepth > 0 {
    errors = append(errors, "unclosed subshell $(...)")
}
```

---

## 6. Nested Quotes

Foundation Shell supports sophisticated nested quote handling through depth tracking.

### 6.1 Nested Single Quotes

Multiple pairs of single quotes can appear in a single word:

```
Input:  echo 'outer 'inner' end'
Depth:       1      2      1    0

# This is VALID because:
# - 4 single quotes total (even count)
# - Final depth is 0
```

**Note**: This behavior differs from traditional shells where `'outer 'inner' end'` would be parsed differently.

### 6.2 Nested Double Quotes

Similarly, double quotes can be nested:

```
Input:  echo "echo "word""
Depth:       1      2     1    0

# Valid due to even count (4 quotes)
```

### 6.3 Nested Backticks

Backticks support nesting through depth tracking:

```
Input:  echo `echo `date``
Depth:       1      2     1    0

# Valid - processed innermost first
```

### 6.4 Mixed Nesting

Different quote types can be mixed:

```
Input:  echo "hello 'world'"
# Double quote depth: 1 (at 'world'), 1 (end) -> valid
# Single quote is literal inside double quotes
Output: hello 'world'

Input:  echo 'hello "world"'
# Single quote depth: 1 (at "world"), 1 (end) -> valid
# Double quote is literal inside single quotes
Output: hello "world"
```

### 6.5 Subshell Depth Tracking

The `$()` syntax uses parenthesis depth tracking, separate from quote depth:

```go
// Handle $() subshell
if c == '$' && singleQuoteDepth == 0 && a.pos+1 < len(a.input) && a.input[a.pos+1] == '(' {
    parenDepth++
    // ...
}

// Handle closing ) for $()
if c == ')' && parenDepth > 0 && singleQuoteDepth == 0 {
    parenDepth--
    // ...
}
```

Subshells can be arbitrarily nested:

```
Input:  echo $(echo $(echo $(date)))
Depth:       1      2      3       210

# Valid - properly balanced parentheses
```

---

## 7. Escape Sequences

Backslash escapes provide character-level quoting outside of single quotes.

[include:_partials/escape-sequences.md](_partials/escape-sequences.md)

### 7.1 The Escape Marker System

When `\$` (or `` \` ``) is processed, the lexer does not simply produce the bare character. Instead, it produces the sequence `\x01$` (escape marker + character):

```go
const EscapeMarker = '\x01'  // ASCII SOH (Start of Heading)

case '$':
    // \$ becomes marker + $ to prevent expansion
    current.WriteRune(EscapeMarker)
    current.WriteRune('$')
case '`':
    // \` becomes marker + ` to prevent command substitution
    current.WriteRune(EscapeMarker)
    current.WriteRune('`')
```

U+0001 is a reserved internal byte; input containing a literal U+0001 has undefined behavior (lexer.md §5.1.4).

### 7.2 Escape Marker Lifecycle

1. **Lexer stage**: `\$VAR` becomes `\x01$VAR` in token content
2. **Expander stage**: Sees `\x01$`, recognizes escaped dollar, keeps `$VAR` literal
3. **Post-expansion**: `StripEscapeMarkers()` removes remaining `\x01` — unconditionally, for every value token (lexer.md §11.3)

```go
// In expander
if i < len(token)-1 && token[i] == '\x01' && token[i+1] == '$' {
    // Escaped dollar - write literal $ and skip the marker
    result.WriteByte('$')
    i += 2
    continue
}

// Post-expansion cleanup (every value token, single-quoted included)
expandedValue = lexer.StripEscapeMarkers(expandedValue)
```

### 7.3 Escape Examples

```
Input:  echo hello\ world
Tokens: ["echo", "hello world"]
# Escaped space prevents word splitting

Input:  echo \$HOME
Tokens: ["echo", "\x01$HOME"]
Expanded: ["echo", "$HOME"]
# Dollar sign remains literal

Input:  echo "say \"hi\""
Tokens: ["echo", "say \"hi\""]
# Escaped quotes inside double quotes

Input:  echo 'hello\nworld'
Tokens: ["echo", "hello\\nworld"]
# Inside single quotes, backslash is literal
```

### 7.4 Trailing Backslash

A backslash at end of input (with nothing to escape) is preserved:

```
Input:  echo hello\
Tokens: ["echo", "hello\\"]
```

### 7.5 Escaped Quotes in Syntax Analyzer

The syntax analyzer recognizes escaped characters to avoid counting them as quotes:

```go
// Handle escape sequences (outside single quotes)
if c == '\\' && singleQuoteDepth == 0 && a.pos+1 < len(a.input) {
    next := a.input[a.pos+1]
    // Escaped characters don't count toward quote depth
    builder.WriteRune(c)
    builder.WriteRune(next)
    a.pos += 2
    continue
}
```

This prevents `\"` from affecting double quote depth.

---

## 8. Quote Removal During Word Processing

### 8.1 When Quotes Are Removed

Quote characters (single and double) acting as DELIMITERS are **removed** during lexer tokenization. The output token contains only the content:

```
Input:  echo "hello world"
Token:  {Content: "hello world", WasQuoted: true}
# Note: The " characters are not in Content

Input:  echo 'hello world'
Token:  {Content: "hello world", WasSingleQuoted: true, WasQuoted: true}
# Note: The ' characters are not in Content
```

**Exception:** quote characters inside a command-substitution body are NOT delimiters at the outer level — they are preserved verbatim in the token and take effect when the body is re-parsed (lexer.md §7.1):

```
Input:  echo $(echo "a  b")
Token:  {Content: "$(echo \"a  b\")", WasQuoted: false}
```

### 8.2 How Quote Removal Works

The lexer skips delimiter quote characters rather than appending them. The full state machine is normative in lexer.md §10.2; the quote handling in outline:

```go
// Single quote toggle (literal inside double quotes and backtick bodies)
if c == '\'' && !inDoubleQuotes && !inBacktickBody {
    toggle inSingleQuotes
    if insideSubstitutionBody {
        current.WriteRune(c)   // body content - preserved verbatim
    } else if entering {
        wasSingleQuoted = true // delimiter - removed, flags set
        wasQuoted = true
    }
}

// Double quote toggle (literal inside single quotes)
if c == '"' && !inSingleQuotes {
    toggle inDoubleQuotes
    if insideSubstitutionBody {
        current.WriteRune(c)   // body content - preserved verbatim
    } else {
        wasQuoted = true       // delimiter - removed, flag set
    }
}
```

### 8.3 Syntax Analyzer vs Lexer

The syntax analyzer **preserves** quotes in token values for highlighting purposes:

```go
// Analyzer token
{Type: TypeSingleQuotedString, Value: "'hello world'", ...}
# Includes quote characters

// Lexer token
{Content: "hello world", WasSingleQuoted: true, WasQuoted: true}
# Excludes quote characters
```

This difference exists because:
- Analyzer: Needs exact positions for syntax highlighting
- Lexer: Needs clean content for command execution

### 8.4 Empty Quoted Strings

Empty quoted strings produce an empty token — empty arguments are representable (lexer.md §4.5):

```
Input:  echo ""
Lexer:  [{Content: "echo"},
         {Content: "", WasQuoted: true}]

Input:  echo "" arg
Lexer:  [{Content: "echo"},
         {Content: "", WasQuoted: true},
         {Content: "arg"}]
```

The quoting flags reset at every word boundary, so the quotes of an empty token never leak into the next word: in `echo '' $HOME`, the `$HOME` token has `WasSingleQuoted: false` and is expanded.

### 8.5 Adjacent Quote Concatenation

Adjacent quoted segments (without whitespace) are concatenated into a single token:

```
Input:  echo "hello"'world'
Lexer:  [{Content: "echo"},
         {Content: "helloworld", WasSingleQuoted: true, WasQuoted: true}]

Input:  echo hello"world"
Lexer:  [{Content: "echo"},
         {Content: "helloworld", WasQuoted: true}]
```

---

## 9. Semantic Types for Highlighting

The syntax analyzer assigns semantic types for syntax highlighting (canonical list in highlighting.md §3):

### 9.1 Quote-Related Semantic Types

| Type | Description | Example |
|------|-------------|---------|
| `TypeSingleQuotedString` | Entire token is single-quoted | `'hello world'` |
| `TypeDoubleQuotedString` | Entire token is double-quoted | `"hello world"` |
| `TypeBacktick` | Entire token is backtick-delimited | `` `date` `` |
| `TypeCommandSubst` | Entire token is `$(...)` | `$(date)` |
| `TypeVariable` | `$` + letter/underscore/`{` (variable reference) | `$HOME` |
| `TypeError` | Unclosed quotes or other errors | `"unclosed` |

### 9.2 Semantic Type Determination

```go
func (a *analyzer) determineWordType(value string, singleDepth, doubleDepth, backtickDepth, parenDepth int) SemanticType {
    // Check for errors first
    if singleDepth%2 != 0 || doubleDepth%2 != 0 || backtickDepth%2 != 0 || parenDepth > 0 {
        return TypeError
    }

    // Check for quote types (when entire token is quoted)
    if len(value) >= 2 {
        if value[0] == '\'' && value[len(value)-1] == '\'' {
            return TypeSingleQuotedString
        }
        if value[0] == '"' && value[len(value)-1] == '"' {
            return TypeDoubleQuotedString
        }
        if value[0] == '`' && value[len(value)-1] == '`' {
            return TypeBacktick
        }
    }
    // ...
}
```

A word that mixes quote styles or has unquoted parts (e.g. `'a'"b"`) is NOT a string type — it falls through to `TypeArgument`/`TypeCommand`. The string types apply only when the whole word is wrapped in one matching pair (highlighting.md §4.5).

### 9.3 Depth Information

Each token includes depth information for nested structures:

```go
type AnalyzedToken struct {
    Type  SemanticType
    Value string
    Start int
    End   int
    Depth int  // Maximum nesting depth for this token
}
```

---

## 10. Command Substitution Details

### 10.1 Syntax and Rules

[include:_partials/command-substitution-syntax.md](_partials/command-substitution-syntax.md)

### 10.2 Execution and Substitution

Command substitution:
1. Re-parses and executes the command inside
2. Captures stdout
3. Removes trailing newlines
4. Replaces the substitution with output

```go
// Trim trailing newlines (standard shell behavior)
output = strings.TrimRight(output, "\n")
```

### 10.3 Innermost-First Processing

Nested substitutions are processed from innermost to outermost:

```go
// Keep expanding until no more substitutions are found.
// This naturally handles nesting by processing innermost first.
for {
    dollarStart, dollarEnd, dollarCmd := findInnermostDollarParen(result)
    backtickStart, backtickEnd, backtickCmd := findInnermostBacktick(result)

    // If no substitutions found, we're done
    if dollarStart == -1 && backtickStart == -1 {
        break
    }
    // ... execute and replace
}
```

### 10.4 Suppression by Single Quotes

Command substitution is NOT performed when the token was single-quoted:

```
Input:  echo '$(date)'
Output: $(date)    (literal text)

Input:  echo "$(date)"
Output: Mon Jan 12 10:30:00 UTC 2026
```

### 10.5 Balanced Parentheses

The `$()` syntax requires balanced parentheses. The scan operates on runes:

```go
func findMatchingParen(runes []rune, startIdx int) int {
    depth := 1
    for i := startIdx; i < len(runes); i++ {
        switch runes[i] {
        case '(':
            depth++
        case ')':
            depth--
            if depth == 0 {
                return i
            }
        }
    }
    return -1  // Not found
}
```

---

## 11. Error Conditions

### 11.1 Quote-Related Errors

The canonical error-string table lives in diagnostics.md §5. The quote-related conditions:

| Error | Condition | Message |
|-------|-----------|---------|
| Unclosed single quote | Unclosed single quote at end of input | `unclosed single quote` |
| Unclosed double quote | Unclosed double quote at end of input | `unclosed double quote` |
| Unclosed backtick | Unclosed backtick at end of input | `unclosed backtick` |
| Unclosed command substitution | `parenDepth > 0` | `unclosed command substitution $(...)` |

The lexer reports the same strings for the same conditions (lexer.md §9).

### 11.2 Error Detection Code

```go
// Check for unclosed quotes
if singleQuoteDepth%2 != 0 {
    a.errors = append(a.errors, SyntaxError{
        Start:   start,
        End:     a.pos,
        Message: "unclosed single quote",
    })
}
if doubleQuoteDepth%2 != 0 {
    a.errors = append(a.errors, SyntaxError{
        Start:   start,
        End:     a.pos,
        Message: "unclosed double quote",
    })
}
if backtickDepth%2 != 0 {
    a.errors = append(a.errors, SyntaxError{
        Start:   start,
        End:     a.pos,
        Message: "unclosed backtick",
    })
}
if parenDepth > 0 {
    a.errors = append(a.errors, SyntaxError{
        Start:   start,
        End:     a.pos,
        Message: "unclosed command substitution $(...)",
    })
}
```

### 11.3 Error Position Tracking

Errors include position information for accurate error reporting (positions are rune indices):

```go
type SyntaxError struct {
    Start   int    // Rune position (0-indexed)
    End     int    // Rune position (exclusive)
    Message string
}
```

---

## 12. Complete Examples

### 12.1 Simple Quoting

```
# Single quotes - literal
Input:  echo 'Hello $USER'
Output: Hello $USER

# Double quotes - with expansion
Input:  echo "Hello $USER"
Output: Hello johndoe

# No quotes - with expansion
Input:  echo Hello $USER
Output: Hello johndoe
```

### 12.2 Nested Quotes (Depth Tracking)

```
# Four single quotes - valid (even count)
Input:  echo 'outer 'inner' outer'
Depths: 0    1      2      1      0
Valid:  YES

# Three single quotes - invalid (odd count)
Input:  echo 'outer 'inner'
Depths: 0    1      2      1
Valid:  NO - unclosed single quote
```

### 12.3 Mixed Quoting

```
# Double quotes inside single quotes
Input:  echo 'say "hello"'
Output: say "hello"

# Single quotes inside double quotes
Input:  echo "it's fine"
Output: it's fine

# Adjacent quotes concatenate
Input:  echo 'single'"double"
Output: singledouble
```

### 12.4 Escape Sequences

```
# Escaped space prevents splitting
Input:  echo hello\ world
Output: hello world

# Escaped dollar prevents expansion
Input:  echo \$HOME
Output: $HOME

# Escaped quote inside double quotes
Input:  echo "say \"hi\""
Output: say "hi"

# No escapes in single quotes
Input:  echo 'path\nto\nfile'
Output: path\nto\nfile
```

### 12.5 Command Substitution

```
# Backticks
Input:  echo `date +%Y`
Output: 2026

# Modern syntax
Input:  echo $(date +%Y)
Output: 2026

# Nested
Input:  echo $(echo $(date +%Y))
Output: 2026

# Suppressed by single quotes
Input:  echo '$(date)'
Output: $(date)
```

### 12.6 Complex Real-World Examples

```
# Git commit with message
Input:  git commit -m "Fix bug in parser"
Tokens: [git, commit, -m, Fix bug in parser]

# Grep with pattern
Input:  grep -r 'func main' ./src
Tokens: [grep, -r, func main, ./src]
# Note: 'func main' has WasSingleQuoted: true

# Complex mixed quoting
Input:  echo "Hello, "'"'"$USER"'"'"!"
Tokens: [echo, Hello, '", $USER (expanded), "'!]
# Actually concatenates to: Hello, '"johndoe"'!
```

---

## 13. Differences from POSIX Shell

| Feature | POSIX Shell | Foundation Shell |
|---------|-------------|------------------|
| Single quote nesting | Not allowed | Allowed via depth tracking |
| Double quote nesting | Not allowed | Allowed via depth tracking |
| `$'...'` ANSI-C quoting | Supported (Bash) | Not supported |
| `$"..."` locale translation | Supported (Bash) | Not supported |
| Line continuation `\<newline>` | Joins lines | Not supported |
| Here-documents `<<` | Supported | Not supported |
| Here-strings `<<<` | Supported (Bash) | Not supported |
| `\` at EOL in double quotes | Line continuation | Not supported |

### 13.1 Key Behavioral Differences

**Quote Depth Tracking**: Foundation Shell allows what POSIX considers invalid:

```
# Foundation Shell: VALID (4 quotes, even count)
echo 'a 'b' c'

# POSIX: Would be parsed as:
# - 'a ' (single quoted)
# - b (unquoted)
# - ' c' (unclosed quote - ERROR)
```

This is an intentional design choice for Foundation Shell's depth-based quote validation.

---

## 14. Implementation Reference

### 14.1 Key Source Files

Paths are relative to the foundation-shell repository root:

| File | Purpose |
|------|---------|
| `src/internal/syntax/analyzer.go` | Syntax analysis, depth tracking, error detection |
| `src/internal/lexer/lexer.go` | Tokenization, escape processing, quote removal |
| `src/internal/expander/expander.go` | Variable/command expansion |
| `src/pkg/parser/parser.go` | Command chain construction |

### 14.2 Key Functions

```go
// Syntax Analysis
func Analyze(input string) *AnalysisResult

// Tokenization
func Tokenize(input string) ([]TokenContext, error)

// Expansion
func Expand(token string, wasSingleQuoted bool) string
func ExpandEnvironment(token string) string
func ExpandTilde(token string) string
func ExpandCommandSubstitution(token string, executor SubstitutionExecutor) (string, error)

// Utilities
func StripEscapeMarkers(s string) string
```

(`SubstitutionExecutor` is the renamed `SubshellExecutor` — see expansion.md; "subshell" is reserved for future `()` grouping.)

### 14.3 Key Constants

```go
const EscapeMarker = '\x01'  // Marks escaped dollar signs and backticks
```

### 14.4 Key Data Structures

```go
// Lexer output
type TokenContext struct {
    Content         string
    WasSingleQuoted bool
    WasQuoted       bool
    IsOperator      bool
}

// Analyzer output
type AnalyzedToken struct {
    Type  SemanticType
    Value string
    Start int
    End   int
    Depth int
}

type SyntaxError struct {
    Start   int
    End     int
    Message string
}
```

---

## 15. Testing Considerations

### 15.1 Critical Test Cases

1. **Even/odd quote counts**: Test that even counts are valid, odd counts error
2. **Quote isolation**: Verify quotes inside other quotes are literal (§5.3)
3. **Escape processing**: Test all escape sequences in all contexts
4. **Concatenation**: Test adjacent quoted segments
5. **Empty strings**: Test that `""` and `''` produce empty tokens
6. **Nested structures**: Test deeply nested quotes and substitutions
7. **Substitution bodies**: Quotes/escapes inside `$()`/backticks preserved verbatim; quoted `)` does not close
8. **WasSingleQuoted/WasQuoted propagation**: Verify conservative flag behavior and per-boundary reset
9. **Error messages**: Verify canonical strings (diagnostics.md §5) and positions
10. **Lexer/analyzer agreement**: The two scanners accept exactly the same inputs (highlighting.md §7.1)

### 15.2 Edge Cases

```
# Trailing backslash
echo hello\

# Adjacent empty quotes
echo ""''

# Quote at word boundary
echo hello"world"there

# Maximum nesting
echo "$(echo '$(echo `date`)')"

# Mixed escapes
echo "\\\"\\$"
```

---

## 16. Summary

Foundation Shell's quoting system provides:

1. **Single quotes** for complete literal preservation
2. **Double quotes** for whitespace preservation with expansion
3. **Backticks and $()** for command substitution
4. **Escape sequences** for character-level control
5. **Depth tracking** for robust quote validation
6. **Quote removal** during tokenization (substitution bodies preserved verbatim)
7. **WasSingleQuoted / WasQuoted flags** for expansion control

The system prioritizes:
- Safety (conservative single-quote propagation)
- Robustness (depth-based validation)
- Compatibility (standard shell conventions where practical)
- Clarity (explicit error messages)
