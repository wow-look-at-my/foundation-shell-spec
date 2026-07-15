---
title: Syntax Highlighting Specification
description: Semantic token types, theme system, real-time REPL highlighting, and error visualization.
recommend_after: execution.md
---

# Foundation Shell Syntax Highlighting Specification

> **Canonical for:** the syntax analyzer, semantic token types, the theme system, and REPL highlighting. The authority map lives in the README. (Implementation: `src/internal/syntax/` in the foundation-shell repository.)

---

## Table of Contents

1. [Overview](#1-overview)
2. [Architecture](#2-architecture)
3. [Semantic Token Types](#3-semantic-token-types)
4. [Analyzer Behavior](#4-analyzer-behavior)
5. [Theme System](#5-theme-system)
6. [REPL Integration](#6-repl-integration)
7. [Error Highlighting](#7-error-highlighting)
8. [Diagnostics Formatting](#8-diagnostics-formatting)
9. [Quote and Substitution State Tracking](#9-quote-and-substitution-state-tracking)
10. [Edge Cases](#10-edge-cases)
11. [Future Enhancements](#11-future-enhancements)

---

## 1. Overview

Foundation Shell provides real-time syntax highlighting for interactive shell input. The highlighting system is designed around three core principles:

1. **Single Source of Truth**: The same `Analyze()` function powers both syntax highlighting AND syntax validation. This ensures consistent behavior between what the user sees highlighted and what the shell considers valid.

2. **Semantic Token Types**: Highlighting is based on semantic meaning, not colors. Token types (e.g., `TypeCommand`, `TypeOperator`) are decoupled from visual presentation, allowing themes to customize appearance.

3. **Real-Time Feedback**: Highlighting updates on every keystroke via the readline `Painter` interface, providing immediate visual feedback including error highlighting for invalid syntax.

### System Components

```mermaid
flowchart TD
    Input[User Input]
    Painter[readline<br>Painter Interface]
    Analyze[Analyze&lpar;&rpar;<br>Single Source of Truth]
    Result[AnalysisResult<br>• Tokens&lbrack;&rbrack;<br>• Errors&lbrack;&rbrack;<br>• Valid bool]
    Highlighter[Highlighter<br>Apply Theme]
    Diagnostics[Diagnostics<br>Error Display]

    Input --> Analyze
    Painter --> Analyze
    Analyze --> Result
    Result --> Highlighter
    Result --> Diagnostics
```

---

## 2. Architecture

### 2.1 Core Types

#### SemanticType

An enumerated type representing the semantic meaning of a token:

```go
type SemanticType int

const (
    TypeUnknown SemanticType = iota
    TypeCommand
    TypeArgument
    TypeOperator
    TypeRedirection
    TypeRedirectionTarget
    TypeSingleQuotedString
    TypeDoubleQuotedString
    TypeBacktick
    TypeCommandSubst
    TypeVariable
    TypeParenGroup
    TypeError
    TypeWhitespace
)
```

(`TypeCommandSubst` was formerly named `TypeSubshell`; it was renamed because `$(...)` is command substitution — "subshell" is reserved for future `( ... )` command grouping.)

#### AnalyzedToken

A token with semantic information and position data:

```go
type AnalyzedToken struct {
    Type  SemanticType  // Semantic meaning
    Value string        // Raw text of token
    Start int           // Rune position (0-indexed, inclusive)
    End   int           // Rune position (exclusive)
}
```

There is no depth field: quote counts and parenthesis depth are internal scanning state (§9), not token output.

#### SyntaxError

A syntax error with position and message:

```go
type SyntaxError struct {
    Start   int     // Start position (0-indexed)
    End     int     // End position (exclusive)
    Message string  // Human-readable error description
}
```

#### AnalysisResult

The complete result of syntax analysis:

```go
type AnalysisResult struct {
    Tokens []AnalyzedToken
    Errors []SyntaxError
    Valid  bool  // true if len(Errors) == 0
}
```

### 2.2 Key Interfaces

#### Theme

Maps semantic types to ANSI escape codes:

```go
type Theme map[SemanticType]string
```

#### Highlighter

Applies syntax highlighting using a theme:

```go
type Highlighter struct {
    theme Theme
}

func NewHighlighter(theme Theme) *Highlighter
func (h *Highlighter) Highlight(input string) string
func (h *Highlighter) HighlightResult(input string) (string, []SyntaxError)
```

---

## 3. Semantic Token Types

Each token type represents a specific semantic meaning in shell syntax. Token types are color-agnostic; visual presentation is determined by the theme.

### 3.1 TypeCommand

**Definition:** The first word in a command position.

**Trigger Conditions:**
- First word of input
- First word after pipe (`|`)
- First word after `&&`
- First word after `||`
- First word after `;`

**Examples:**
```sh
echo hello          # "echo" is TypeCommand
cat file | grep x   # "cat" and "grep" are both TypeCommand
cmd1 && cmd2        # "cmd1" and "cmd2" are both TypeCommand
```

**NOT TypeCommand:**
```sh
echo hello          # "hello" is TypeArgument (not first)
cmd > file          # "file" is TypeRedirectionTarget (after redirection)
```

### 3.2 TypeArgument

**Definition:** A word that is an argument to a command.

**Trigger Conditions:**
- Any word after the first word in command position
- NOT immediately after a redirection operator

**Examples:**
```sh
echo hello world    # "hello" and "world" are TypeArgument
ls -la /tmp         # "-la" and "/tmp" are TypeArgument
```

### 3.3 TypeOperator

**Definition:** Control flow and pipeline operators.

**Recognized Operators:**
| Operator | Length | Description |
|----------|--------|-------------|
| `\|`     | 1      | Pipe |
| `;`      | 1      | Command separator |
| `&&`     | 2      | Logical AND |
| `\|\|`   | 2      | Logical OR |

**State Change:** After an operator (except `;` at end), the next word is TypeCommand.

**Examples:**
```sh
cmd1 | cmd2         # "|" is TypeOperator
cmd1 && cmd2        # "&&" is TypeOperator
cmd1 ; cmd2         # ";" is TypeOperator
```

### 3.4 TypeRedirection

**Definition:** File redirection operators.

**Recognized Operators:**
| Operator | Length | Description |
|----------|--------|-------------|
| `>`      | 1      | Redirect stdout (overwrite) |
| `<`      | 1      | Redirect stdin |
| `>>`     | 2      | Redirect stdout (append) |
| `2>`     | 2      | Redirect stderr (overwrite) |
| `2>>`    | 3      | Redirect stderr (append) |

**Matching Order:** Longer operators MUST be matched before shorter ones (e.g., `2>>` before `>>` before `>`).

**State Change:** After a redirection operator, the next word is TypeRedirectionTarget.

### 3.5 TypeRedirectionTarget

**Definition:** The filename following a redirection operator.

**Trigger Conditions:**
- The word immediately following any TypeRedirection token

**Examples:**
```sh
echo > output.txt   # "output.txt" is TypeRedirectionTarget
cmd 2>> errors.log  # "errors.log" is TypeRedirectionTarget
cat < input.txt     # "input.txt" is TypeRedirectionTarget
```

### 3.6 TypeSingleQuotedString

**Definition:** Content enclosed in single quotes.

**Recognition Rules:**
- Starts with `'` and ends with `'`
- The entire token (including quotes) must form a complete quoted string
- Single quotes do NOT interpret escape sequences
- Single quotes do NOT expand variables

**Parity Tracking:** Single quote characters are counted; an odd count indicates an unclosed quote (quoting.md §5).

**Examples:**
```sh
echo 'hello world'      # "'hello world'" is TypeSingleQuotedString
echo 'a 'b' c'          # Valid: 4 quotes (even) - two pairs concatenated
echo 'hello             # TypeError (odd count = unclosed)
```

### 3.7 TypeDoubleQuotedString

**Definition:** Content enclosed in double quotes.

**Recognition Rules:**
- Starts with `"` and ends with `"`
- The entire token (including quotes) must form a complete quoted string
- Escape sequences ARE processed (e.g., `\"`, `\\`)
- Variables ARE expanded within double quotes (but the whole token is still TypeDoubleQuotedString)

**Parity Tracking:** Double quote characters are counted; an odd count indicates an unclosed quote.

**Escape Handling:**
- `\"` does NOT count toward the quote count
- `\\` is a literal backslash

**Examples:**
```sh
echo "hello world"           # "\"hello world\"" is TypeDoubleQuotedString
echo "echo \"word\""         # Valid (escaped inner quotes)
echo "outer "inner" outer"   # Valid: 4 quotes (even) - two pairs concatenated
echo "hello                  # TypeError (odd count = unclosed)
```

### 3.8 TypeBacktick

**Definition:** Command substitution using backticks.

**Recognition Rules:**
- Starts with `` ` `` and ends with `` ` ``
- Content between backticks is executed as a command
- The result replaces the backtick expression

**Parity Tracking:** Backtick characters are counted; parity gives the state. Backtick state is a pure toggle — backticks CANNOT nest (quoting.md §6.3). To nest, escape the inner backticks (`` \` ``).

**Context Rules:**
- Backticks are NOT recognized inside single quotes
- Backticks ARE recognized inside double quotes

**Examples:**
```sh
echo `date`              # "`date`" is TypeBacktick
echo `echo \`date\``     # Nesting requires escaped inner backticks
echo `unclosed           # TypeError (odd count = unclosed)
```

### 3.9 TypeCommandSubst

**Definition:** Command substitution using `$(...)` syntax.

**Recognition Rules:**
- Starts with `$(`
- Ends with matching `)`
- Parentheses must be balanced
- A `)` inside the body's open quotes does not close the substitution (quoting.md §6.5)

**Depth Tracking:** Tracks parenthesis depth to handle nested substitutions (a true depth counter — the only real nesting in the model).

**Context Rules:**
- Command substitutions are NOT recognized inside single quotes
- Command substitutions ARE recognized inside double quotes

**Examples:**
```sh
echo $(date)                 # "$(date)" is TypeCommandSubst
echo $(date +%Y-%m-%d)       # "$(date +%Y-%m-%d)" is TypeCommandSubst
echo $(echo $(pwd))          # Nested substitution (valid)
echo $(unclosed              # TypeError (unbalanced parens)
```

### 3.10 TypeVariable

**Definition:** Variable references.

**Recognition Rules:**
- A `$` starts a variable token ONLY when the next character is a letter (a-z, A-Z), an underscore, or `{`: `$VAR`, `$_var`, `${HOME}`
- `$(` starts a command substitution instead (§3.9)
- Any other `$` (followed by a digit, punctuation, whitespace, or end of input) is a literal character within the word — `$?`, `$1`, `$-foo`, and a trailing `$` are NOT variables

**Context Rules:**
- Variables are NOT expanded inside single quotes
- Variables ARE expanded inside double quotes
- When inside double quotes, the entire quoted string is TypeDoubleQuotedString, not TypeVariable

**Examples:**
```sh
echo $HOME               # "$HOME" is TypeVariable
echo ${HOME}             # "${HOME}" is TypeVariable
echo $PATH/bin           # "$PATH/bin" is TypeVariable (entire token)
echo '$HOME'             # "'$HOME'" is TypeSingleQuotedString (not variable)
echo $?                  # "$?" is TypeArgument ($ + ? cannot start a name)
```

### 3.11 TypeParenGroup

**Definition:** Bare parentheses outside a `$(...)` command substitution.

**Recognition Rules:**
- Single `(` or `)` character not belonging to a `$(...)` substitution
- Reserved for command grouping — a FUTURE feature (operators.md); today such input does not execute

**Examples:**
```sh
(echo hello)             # "(" and ")" are TypeParenGroup
```

### 3.12 TypeError

**Definition:** Invalid or error regions in the input.

**Trigger Conditions:**
- Unclosed single quote (odd count)
- Unclosed double quote (odd count)
- Unclosed backtick (odd count)
- Unclosed command substitution `$(...)`

**Highlighting:** Error tokens are still highlighted (with error styling) to show the user exactly what is problematic.

**Examples:**
```sh
echo "unclosed           # "\"unclosed" is TypeError
echo 'missing            # "'missing" is TypeError
echo $(open              # "$(open" is TypeError
```

### 3.13 TypeWhitespace

**Definition:** Spaces and tabs between tokens.

**Recognition Rules:**
- One or more consecutive whitespace characters (space, tab, etc.)
- Whitespace is tokenized to enable accurate position tracking

**Examples:**
```sh
echo hello               # Single space " " is TypeWhitespace
echo    hello            # Multiple spaces "   " is TypeWhitespace
```

### 3.14 TypeUnknown

**Definition:** Default/fallback type for unclassified tokens.

**Usage:** Should rarely appear in practice. Indicates a gap in the analyzer.

---

## 4. Analyzer Behavior

### 4.1 Analysis Function

```go
func Analyze(input string) *AnalysisResult
```

The `Analyze` function is the **single source of truth** for syntax analysis. It performs:
1. Tokenization with semantic classification
2. Position tracking for each token
3. Quote/substitution state tracking for nested constructs
4. Error detection and reporting

### 4.2 State Machine

The analyzer maintains internal state:

```go
type analyzer struct {
    input            []rune          // Input as runes (Unicode support)
    pos              int             // Current position
    tokens           []AnalyzedToken // Accumulated tokens
    errors           []SyntaxError   // Accumulated errors
    isFirstInCommand bool            // Next word should be TypeCommand
    afterRedirection bool            // Next word should be TypeRedirectionTarget
}
```

### 4.3 Analysis Flow

1. **Initialize:** Set `isFirstInCommand = true`, `afterRedirection = false`

2. **Main Loop:** For each character position:
   - If whitespace: consume and emit TypeWhitespace token
   - If operator: match longest operator, emit token, update state
   - Otherwise: parse word (handles quotes, variables, command substitutions)

3. **Word Parsing:**
   - Track quote counts (single, double, backtick; parity = state, quoting.md §5)
   - Track command-substitution parenthesis depth (fresh quote context per substitution, quoting.md §6.5)
   - Handle escape sequences
   - Consume until word boundary (whitespace or operator)

4. **Finalization:**
   - Check for unclosed quotes (odd counts)
   - Check for unclosed command substitutions
   - Check for trailing operators (except `;`)
   - Check for missing redirection targets

### 4.4 Operator Matching Order

Operators MUST be matched longest-first to avoid incorrect tokenization:

1. 3-character: `2>>`
2. 2-character: `&&`, `||`, `>>`, `2>`
3. 1-character: `|`, `;`, `>`, `<`, `(`, `)`

`2>` and `2>>` are recognized only when the `2` begins a new word (i.e. the pending word is empty and the `2` is immediately followed by `>`); a `2` inside a longer word does not form a stderr redirection. `echo a2>f` therefore tokenizes as `a2`, `>`, `f` — matching the execution lexer (lexer.md §3.3.1). A single `&` is not an operator.

### 4.5 Word Type Determination

After parsing a word, its type is determined by:

1. **Error Check:** If any quote count is odd or the substitution depth is unbalanced, return TypeError
2. **Redirection Target:** If `afterRedirection` is true, return TypeRedirectionTarget
3. **Quoted String:** If the ENTIRE word is wrapped in one matching quote pair, return the appropriate string type. A word that mixes quote styles or has unquoted parts (e.g. `'a'"b"`) is NOT a string type and falls through
4. **Command Substitution:** If the word matches the `$(...)` pattern, return TypeCommandSubst
5. **Variable:** If the word starts with `$` followed by a letter, underscore, or `{` (§3.10), return TypeVariable
6. **Command vs Argument:** If `isFirstInCommand`, return TypeCommand; else TypeArgument

---

## 5. Theme System

### 5.1 Theme Definition

A Theme is a mapping from SemanticType to ANSI escape codes:

```go
type Theme map[SemanticType]string
```

### 5.2 Default Theme

The default theme provides sensible colors for terminal display:

| SemanticType | ANSI Code | Color | Description |
|--------------|-----------|-------|-------------|
| TypeCommand | `\033[1;36m` | Bold Cyan | Commands stand out |
| TypeArgument | `\033[0m` | Default | Arguments blend in |
| TypeOperator | `\033[1;33m` | Bold Yellow | Operators are prominent |
| TypeRedirection | `\033[35m` | Magenta | Redirections are distinct |
| TypeRedirectionTarget | `\033[0m` | Default | Targets blend in |
| TypeSingleQuotedString | `\033[32m` | Green | Single quotes |
| TypeDoubleQuotedString | `\033[33m` | Yellow | Double quotes (different) |
| TypeBacktick | `\033[36m` | Cyan | Command substitution |
| TypeCommandSubst | `\033[36m` | Cyan | Command substitution |
| TypeVariable | `\033[34m` | Blue | Variables |
| TypeParenGroup | `\033[35m` | Magenta | Grouping |
| TypeError | `\033[4;31m` | Underline Red | Errors are very visible |
| TypeWhitespace | `\033[0m` | Reset | Invisible |
| TypeUnknown | `\033[0m` | Reset | Fallback |

### 5.3 ANSI Escape Code Format

ANSI codes follow the format: `\033[<params>m`

Common parameters:
- `0` - Reset all attributes
- `1` - Bold
- `4` - Underline
- `30-37` - Foreground colors (black, red, green, yellow, blue, magenta, cyan, white)
- `38;5;N` - 256-color foreground
- `40-47` - Background colors
- `48;5;N` - 256-color background

### 5.4 Theme Customization

Create a custom theme by providing a Theme map:

```go
customTheme := Theme{
    TypeCommand:            "\033[38;5;196m", // Bright red
    TypeArgument:           "\033[38;5;226m", // Yellow
    TypeOperator:           "\033[38;5;21m",  // Blue
    // ... etc
}

h := NewHighlighter(customTheme)
```

**Partial Themes:** If a SemanticType is not defined in the theme, the highlighter falls back to ANSI reset (`\033[0m`).

### 5.5 Highlighting Application

The highlighter applies colors as follows:

```go
func (h *Highlighter) applyTheme(tokens []AnalyzedToken) string {
    var builder strings.Builder
    for _, token := range tokens {
        color := h.theme[token.Type]  // Get color for type
        if color == "" {
            color = ansiReset
        }
        builder.WriteString(color)      // Apply color
        builder.WriteString(token.Value) // Write token
        builder.WriteString(ansiReset)  // Reset after token
    }
    return builder.String()
}
```

**Key Behavior:** Each token is followed by an ANSI reset to prevent color bleeding.

---

## 6. REPL Integration

### 6.1 readline Painter Interface

Foundation Shell integrates with the `github.com/chzyer/readline` library using its `Painter` interface:

```go
type Painter interface {
    Paint(line []rune, pos int) []rune
}
```

### 6.2 syntaxPainter Implementation

```go
type syntaxPainter struct {
    highlighter *Highlighter
}

func (p *syntaxPainter) Paint(line []rune, pos int) []rune {
    return []rune(p.highlighter.Highlight(string(line)))
}
```

### 6.3 REPL Configuration

```go
painter := &syntaxPainter{highlighter: syntax.NewHighlighter(syntax.DefaultTheme)}

cfg := &readline.Config{
    Prompt:          "$ ",
    InterruptPrompt: "^C",
    EOFPrompt:       "exit",
    Painter:         painter,  // Enable syntax highlighting
    Stdout:          stdout,
    Stderr:          stderr,
}

rl, _ := readline.NewEx(cfg)
```

### 6.4 Real-Time Highlighting

The `Paint` method is called by readline on every keystroke, providing:
- Immediate visual feedback
- Real-time error highlighting
- Consistent highlighting as the user types

### 6.5 Performance Considerations

The analyzer is designed to be fast enough for real-time use:
- Single-pass analysis
- No backtracking
- O(n) time complexity where n is input length
- Minimal allocations

---

## 7. Error Highlighting

### 7.1 Unified Analysis

The same `Analyze()` function is used for BOTH:
1. Syntax highlighting (via Highlighter)
2. Syntax validation (via AnalysisResult.Errors)

This ensures that any syntax error visible in highlighting is also reported as a diagnostic error, and vice versa.

The promise extends across components: the analyzer and the execution lexer MUST accept exactly the same inputs — an input the analyzer marks valid must tokenize without error, and an input it rejects must fail tokenization. The conformance suite MUST include a lexer/analyzer validity cross-check (for every corpus input, `Tokenize` errors if and only if `Analyze().Valid` is false).

### 7.2 Error Detection

The analyzer detects these error conditions (canonical strings: diagnostics.md §5):

| Error | Detection | Message |
|-------|-----------|---------|
| Unclosed single quote | `singleQuoteCount % 2 != 0` | `unclosed single quote (odd count)` |
| Unclosed double quote | `doubleQuoteCount % 2 != 0` | `unclosed double quote (odd count)` |
| Unclosed backtick | `backtickCount % 2 != 0` | `unclosed backtick (odd count)` |
| Unclosed command substitution | `parenDepth > 0` | `unclosed command substitution $(...)` |
| Leading operator | First non-whitespace token is a chain operator | `unexpected operator at start: <op>` |
| Trailing operator | Last non-whitespace token is `\|`, `&&`, or `\|\|` | `unexpected operator at end` |
| Missing redirect target | Last non-whitespace token is a redirection | `missing redirection target` |

The leading- and trailing-token checks look at the last/first NON-WHITESPACE token — trailing blanks do not defeat them.

### 7.3 Error Token Marking

When an error is detected within a word:
1. The token is assigned `TypeError` semantic type
2. A `SyntaxError` is added to the result
3. The error token is still included in the token stream (for highlighting)

### 7.4 Error Position Tracking

Each error includes precise position information:

```go
type SyntaxError struct {
    Start   int     // Start position (0-indexed, inclusive)
    End     int     // End position (exclusive)
    Message string  // Human-readable description
}
```

### 7.5 Highlighting Errors

Tokens with `TypeError` type are highlighted using the theme's error color:
- Default: `\033[4;31m` (underline red)
- Provides clear visual indication of the error region
- Helps users identify exactly what needs to be fixed

---

## 8. Diagnostics Formatting

### 8.1 FormatDiagnostics Function

```go
func FormatDiagnostics(input string, errors []SyntaxError) string
```

Formats syntax errors for display with:
1. The original input line
2. A caret line (`^^^^^`) pointing to the error
3. The error message

### 8.2 Output Format

```
echo "unclosed
     ^^^^^^^^^
error: unclosed double quote (odd count)
```

### 8.3 Caret Line Construction

```go
func buildCaretLine(start, end int) string
```

- Leading spaces align carets with the error position
- Carets (`^`) span from `start` to `end`
- Minimum of 1 caret even for zero-length errors

### 8.4 Multi-Error Handling

Multiple errors are displayed sequentially, with a blank line between blocks (diagnostics.md §7). Both messages use the canonical strings. Note that one unclosed construct usually swallows the rest of the line (a `'` inside open double quotes is literal and cannot produce a second error); two errors require two independently unclosed constructs, e.g. an unclosed substitution containing an unclosed quote:

```
echo $(foo "bar
     ^^^^^^^^^^
error: unclosed double quote (odd count)

echo $(foo "bar
     ^^^^^^^^^^
error: unclosed command substitution $(...)
```

### 8.5 Edge Case Handling

The diagnostics system handles:
- Empty input
- Errors at start of input (position 0)
- Errors at end of input
- Errors beyond input bounds (graceful clamping)
- Zero-length errors (minimum 1 caret)
- Negative positions (graceful handling)
- Reversed positions (Start > End)
- Unicode characters
- Tab characters
- Special characters in strings

---

## 9. Quote and Substitution State Tracking

### 9.1 Purpose

The analyzer tracks quote state and command-substitution depth to validate syntax and classify tokens. The canonical model is defined in quoting.md §5: quote characters are COUNTED and parity (even/odd) gives the state — which is exactly POSIX quote pairing. True nesting exists only for `$(...)` parentheses.

### 9.2 Counting Rules

For quotes (single, double, backtick):
- Each active quote character increments its counter (counters never decrement)
- Even count = outside that quote type; odd count = inside
- Valid input has all quote counts EVEN at end
- An odd count at end of input indicates an unclosed quote

For command substitutions:
- `$(` increments paren depth (a true depth counter)
- `)` decrements paren depth (when it closes a substitution — quoting.md §6.5)
- Positive depth at end indicates an unclosed command substitution

### 9.3 Context Rules

Whether a character is ACTIVE (affects state) or LITERAL depends on the current context (quoting.md §5.3). Contexts compose: the innermost open construct decides.

| Context | Single Quotes | Double Quotes | Backticks | `$(` opens | `)` closes `$(` |
|---------|---------------|---------------|-----------|------------|-----------------|
| Top-level | Active | Active | Active | Active | Active (if depth > 0) |
| Inside `'...'` | Closing `'` only | Literal | Literal | Literal | Literal |
| Inside `"..."` | Literal | Active (closing) | Active | Active | Literal (quoted `)` is content) |
| Inside `` `...` `` | Literal | Active | Active (closing — pure toggle) | Active | Active |
| Inside `$(...)` | Active | Active | Active | Active (nests) | Active |

Each `$(` starts a FRESH quote context for its body; the enclosing state is saved and restored at the matching `)`. This is why `echo "$(date)"` is valid while the `)` in `echo $(echo ")")` is body content (quoting.md §6.5).

### 9.4 No Depth Output

`AnalyzedToken` does not expose the counters: they are internal scanning state. (An earlier draft specified a `Depth` field; it was removed — it was unused by the highlighter and its definition was contradictory.)

### 9.5 Examples

**Repeated Single-Quote Pairs (counts, parity):**
```sh
echo 'a 'b' c'
     ^1 ^2 ^3 ^4    # Count transitions: in, out, in, out - two pairs, valid
```

**Repeated Double-Quote Pairs:**
```sh
echo "outer "inner" outer"
     ^1     ^2    ^3     ^4    # Two pairs concatenated, valid
```

**Nested Command Substitutions (true depth):**
```sh
echo $(echo $(pwd))
     ^1     ^2   ^1^0    # Depth transitions
```

---

## 10. Edge Cases

### 10.1 Empty Input

- Analysis returns empty token slice
- No errors
- `Valid = true`

### 10.2 Whitespace-Only Input

- Single TypeWhitespace token
- No errors
- `Valid = true`

### 10.3 Escaped Characters

Outside single quotes, backslash escapes the next character:
- `\"` - Literal double quote (does not count toward quote state)
- `\'` - Literal single quote (does not count toward quote state)
- `` \` `` - Literal backtick (does not count toward backtick state)
- `\\` - Literal backslash
- `\$` - Literal dollar sign (not variable)

Inside single quotes, NO escape processing occurs.

### 10.4 Trailing Semicolon

A trailing `;` is VALID (like bash):
```sh
echo hello ;   # Valid, no error
```

Other trailing operators are INVALID:
```sh
echo hello |   # Error: unexpected operator at end
echo hello &&  # Error: unexpected operator at end
echo hello ||  # Error: unexpected operator at end
```

### 10.5 Variable in Path

A variable followed by path characters is a single token:
```sh
cd $HOME/projects    # "$HOME/projects" is TypeVariable (single token)
```

### 10.6 Adjacent Quotes

Adjacent quoted segments form ONE word token (a quote character is not a word boundary — §4.3):
```sh
echo 'a'"b"    # One token: "'a'\"b\"" - TypeArgument
```

The whole-word type rules apply (§4.5): `'a'"b"` mixes quote styles, so it is TypeArgument, not a string type. The execution lexer concatenates the same word into the single token `ab` (lexer.md §4.4).

### 10.7 Redirection Without Space

Redirection operators can be adjacent to arguments:
```sh
echo>file      # "echo" (Command), ">" (Redirection), "file" (RedirectionTarget)
```

The execution lexer tokenizes this identically (lexer.md §3.3) — `echo>file` is valid end-to-end.

### 10.8 Multiple Redirections

Multiple redirections in one command are each tokenized:
```sh
cmd < in > out 2>> err
```
Tokens: `cmd`, `<`, `in`, `>`, `out`, `2>>`, `err`

---

## 11. Future Enhancements

### 11.1 Depth Tracking Visualization

A future version may expose the substitution nesting depth on tokens (as a high-water mark recorded during scanning) to enable visual features:

- **Nested Highlighting:** Different shades or styles based on depth
- **Rainbow Brackets:** Different colors for each nesting level
- **Depth Indicators:** Visual markers showing nesting level

(No such field exists today — see §9.4.)

### 11.2 Potential Token Type Extensions

Future versions may add:
- `TypeBuiltin` - Shell built-in commands
- `TypeAlias` - User-defined aliases
- `TypeFunction` - Shell function names
- `TypeGlob` - Glob patterns (`*.txt`, `**/*.go`)
- `TypeBraceExpansion` - Brace expansions (`{a,b,c}`)
- `TypeArithmetic` - Arithmetic expressions (`$((1+2))`)
- `TypeConditional` - Conditional expressions (`[[ ... ]]`)

### 11.3 LSP Integration

The semantic token system is designed to be compatible with Language Server Protocol (LSP) semantic tokens, enabling:
- IDE integration
- Remote highlighting
- Cross-editor consistency

### 11.4 Configurable Error Behavior

Future enhancements may include:
- Warning vs error distinction
- Configurable strictness levels
- Custom error messages
- Error recovery suggestions

---

## Appendix A: ANSI Escape Code Reference

### Basic Colors (Foreground)
| Code | Color |
|------|-------|
| 30 | Black |
| 31 | Red |
| 32 | Green |
| 33 | Yellow |
| 34 | Blue |
| 35 | Magenta |
| 36 | Cyan |
| 37 | White |

### Bright Colors (Foreground)
| Code | Color |
|------|-------|
| 90 | Bright Black (Gray) |
| 91 | Bright Red |
| 92 | Bright Green |
| 93 | Bright Yellow |
| 94 | Bright Blue |
| 95 | Bright Magenta |
| 96 | Bright Cyan |
| 97 | Bright White |

### Attributes
| Code | Attribute |
|------|-----------|
| 0 | Reset |
| 1 | Bold |
| 2 | Dim |
| 3 | Italic |
| 4 | Underline |
| 7 | Reverse |
| 9 | Strikethrough |

### 256-Color Mode
- Foreground: `38;5;N` where N is 0-255
- Background: `48;5;N` where N is 0-255

---

## Appendix B: Test Coverage Summary

The implementation includes comprehensive tests covering:

### Analyzer Tests (`analyzer_test.go`)
- Basic command parsing
- Token position accuracy
- All operator types
- Single and double quotes
- Repeated quote pairs (parity)
- Unclosed quote errors
- Variables (simple and braced)
- Command substitutions (`$(...)`)
- Backticks
- Escaped backticks (nesting workaround)
- Trailing operator errors
- Missing redirection target errors
- Escaped characters
- Error position accuracy
- Empty input
- Whitespace-only input
- Command after pipe
- Command after semicolon
- Lexer/analyzer validity cross-check (§7.1)

### Highlighter Tests (`highlighter_test.go`)
- Basic command highlighting
- All operator highlighting
- Redirection highlighting
- Quote highlighting (single/double)
- Variable highlighting
- Command substitution highlighting
- Backtick highlighting
- Error highlighting (unclosed constructs)
- Input preservation (stripped ANSI equals original)
- Custom theme application
- Partial theme handling
- Complex pipeline highlighting
- Chained operator highlighting
- Multiple redirections
- Empty input
- Whitespace handling
- ANSI reset after each token

### Diagnostics Tests (`diagnostics_test.go`)
- Single error formatting
- Multiple error formatting
- Caret alignment
- Empty errors
- Nil errors
- Error at start
- Error at end
- Single character errors
- Long error spans
- Edge cases (negative positions, reversed positions, etc.)
- Unicode handling
- Real-world integration tests

---

*End of Specification*
