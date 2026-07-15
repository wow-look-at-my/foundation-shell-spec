---
title: Diagnostic Output Specification
description: Error message formatting, caret-based position indicators, and multi-error display.
recommend_after: highlighting.md
---

# Foundation Shell Diagnostic Output Specification

> **Canonical for:** diagnostic output formatting (caret blocks) and the canonical error-string table (§5) used by the analyzer, lexer, and parser. The authority map lives in the README.

This document is the exhaustive specification for Foundation Shell's diagnostic output system. Tests MUST validate against this specification. Any behavior not documented here is undefined.

---

## 1. Overview of Diagnostic System

The diagnostic system provides human-readable error output for syntax errors detected during shell input analysis. It is implemented in the `syntax` package and consists of:

1. **SyntaxError** - A struct containing error position and message
2. **FormatDiagnostics** - Formats errors for display to users
3. **FormatDiagnosticsFromResult** - Convenience wrapper for AnalysisResult

### 1.1 SyntaxError Structure

```go
type SyntaxError struct {
    Start   int    // Start position (0-indexed, inclusive)
    End     int    // End position (0-indexed, exclusive)
    Message string // Human-readable error message
}
```

### 1.2 Core Principle

The diagnostic system takes the original input and a list of syntax errors, producing formatted output that:
- Shows the user exactly where the error occurred
- Uses visual markers (carets) to indicate error spans
- Provides clear error messages

---

## 2. Diagnostic Output Format

The diagnostic output for each error consists of EXACTLY three lines:

```
Line 1: <original input line containing the error>
Line 2: <caret markers showing error location>
Line 3: error: <error message>
```

### 2.1 Line 1: Input Line

The first line is the verbatim content of the line from the original input that contains the error. No modifications, prefixes, or suffixes are added.

**Example:**
```
echo "unclosed
```

### 2.2 Line 2: Caret Line

The second line consists of:
1. Leading spaces to align with the error start position
2. Caret characters (`^`) spanning the error region

The number of leading spaces equals the `Start` position of the error (relative to the line start for multi-line input).

The number of carets equals `End - Start` (the span of the error).

**Example:** For an error at positions 5-10:
```
     ^^^^^
```

### 2.3 Line 3: Error Message

The third line begins with the literal string `error: ` followed by the error message.

**Format:** `error: <message>`

**Example:**
```
error: unclosed double quote
```

### 2.4 Complete Single Error Example

Input: `echo "hello`
Error: Start=5, End=11, Message="unclosed double quote"

Output:
```
echo "hello
     ^^^^^^
error: unclosed double quote
```

---

## 3. Error Position Tracking

### 3.1 Position Indexing

- **Start position**: 0-indexed character position marking the beginning of the error region (inclusive)
- **End position**: 0-indexed character position marking the end of the error region (exclusive)
- Positions are measured in characters (runes), not bytes

### 3.2 Position Semantics

| Position | Meaning |
|----------|---------|
| `Start = 0` | Error begins at the first character |
| `End = len(input)` | Error extends to the end of input |
| `End - Start` | Length of the error span in characters |

### 3.3 Multi-Character Error Spans

Errors typically span multiple characters. The span is determined by the nature of the error:

| Error Type | Start Position | End Position |
|------------|----------------|--------------|
| Unclosed quote | First character of the token containing the unclosed quote | Last character position of the token (exclusive) |
| Trailing operator | First character of the operator | Last character of the operator (exclusive) |
| Missing redirection target | First character of the redirection operator | Last character of the redirection operator (exclusive) |

### 3.4 Examples

**Single-character error span:**
```
Input: "echo |"
Error: Start=5, End=6 (the pipe character)
Span: 1 character
```

**Multi-character error span:**
```
Input: "echo &&"
Error: Start=5, End=7 (the && operator)
Span: 2 characters
```

**Token-spanning error:**
```
Input: echo "unclosed string
Error: Start=5, End=21 (the entire unclosed token)
Span: 16 characters
```

---

## 4. Caret Line Construction

### 4.1 Algorithm

The caret line is constructed as follows:

```
function buildCaretLine(start, end):
    if end <= start:
        end = start + 1  // Ensure at least one caret

    result = ""

    // Add leading spaces
    for i from 0 to start-1:
        result += " "

    // Add carets
    for i from start to end-1:
        result += "^"

    return result
```

### 4.2 Leading Spaces

The number of leading spaces equals the `Start` position of the error relative to the beginning of the line.

For single-line input, this equals `Start`.

For multi-line input, this equals `Start - lineStart`, where `lineStart` is the character position of the first character in the line containing the error.

### 4.3 Caret Count

The number of caret characters (`^`) equals `max(End - Start, 1)`.

A minimum of 1 caret is always displayed, even for zero-length or invalid position errors.

### 4.4 Edge Cases

| Condition | Behavior |
|-----------|----------|
| `Start = End` | Display exactly 1 caret at position `Start` |
| `Start > End` | Treat as `End = Start + 1` (1 caret) |
| `Start < 0` | Clamp to 0 |
| `End > len(line)` | Clamp to `len(line)` |
| `Start > len(line)` | Carets may appear beyond visible content |

### 4.5 Examples

**Error at start of line (Start=0, End=3):**
```
^^^
```

**Error in middle (Start=5, End=10):**
```
     ^^^^^
```

**Error at end (Start=10, End=12):**
```
          ^^
```

**Single character error (Start=5, End=6):**
```
     ^
```

---

## 5. Standard Error Messages (Canonical Table)

This section is the ONE canonical error-string table for Foundation Shell. Every other specification file refers here instead of restating strings. Tests MUST match these strings exactly.

### 5.1 Analyzer and Lexer Errors

The syntax analyzer detects these conditions, and the execution lexer emits the IDENTICAL strings for the same conditions (lexer.md §9) — there is no second message universe:

| Error | Exact Message |
|-------|---------------|
| Unclosed single quote | `unclosed single quote` |
| Unclosed double quote | `unclosed double quote` |
| Unclosed backtick | `unclosed backtick` |
| Unclosed `$(...)` | `unclosed command substitution $(...)` |
| Leading chain operator | `unexpected operator at start: <op>` |
| Trailing pipe, &&, or \|\| | `unexpected operator at end` |
| Missing file after redirect | `missing redirection target` |

Leading-operator detection is REQUIRED of the analyzer (so `| foo` gets a caret diagnostic, not just the parser's plain error line). The unclosed-`$(...)` message was renamed from the earlier `unclosed subshell $(...)` — "subshell" is reserved for future `()` grouping.

### 5.2 Parser Errors

The parser reports these exact strings (the `<...>` placeholders are filled with the offending token text):

| Condition | Exact Message |
|-----------|---------------|
| Empty or whitespace-only input | `empty input` |
| Chain operator at start | `unexpected operator at start: <op>` |
| Trailing chain operator (other than `;`) | `unexpected operator at end: <op>` |
| Consecutive chain operators | `consecutive operators: <op1> followed by <op2>` |
| Redirection-only command (no words) | `empty command` |
| Redirection at end of input | `missing redirection target: <op>` |
| Redirection followed by an operator | `missing redirection target: <op> followed by operator <op2>` |
| Redirection target empty after expansion | `empty redirection target` |
| Unquoted redirection target starting with `&` | `file descriptor duplication is not supported: <word>` |
| Lexer failure (wrapped) | `tokenization error: <lexer error>` |
| Substitution failure (wrapped) | `command substitution error: <error>` |

### 5.3 Message Format Notes

- All messages are lowercase
- Operator symbols in messages use the exact characters: `$(...)`
- No trailing punctuation
- The parser appends `: <op>` context to its operator errors; the analyzer's trailing-operator message carries no suffix (the caret already points at the operator)

---

## 6. Multi-Line Input Handling

### 6.1 Line Detection

For multi-line input, the diagnostic system:
1. Splits input on newline characters (`\n`)
2. Determines which line contains the error based on `Start` position
3. Calculates error positions relative to the line start

### 6.2 Line Number Calculation

```
function findLineForPosition(input, pos):
    lineNum = 0
    lineStart = 0

    for i, char in enumerate(input):
        if i >= pos:
            break
        if char == '\n':
            lineNum++
            lineStart = i + 1

    return lineNum, lineStart
```

### 6.3 Position Adjustment

For errors in multi-line input:
- `startInLine = error.Start - lineStart`
- `endInLine = error.End - lineStart`

These adjusted positions are clamped to valid line bounds.

### 6.4 Example

Input:
```
echo hello
cat "unclosed
```

Error on line 2 (0-indexed line 1), Start=15, End=24

Output:
```
cat "unclosed
    ^^^^^^^^^
error: unclosed double quote
```

---

## 7. Multiple Errors in Single Input

### 7.1 Error Separation

When multiple errors exist, each error block is separated by a single blank line. The complete output ends with exactly one trailing newline (also for a single error block).

Format:
```
<line 1 of error 1>
<carets for error 1>
error: <message 1>

<line 1 of error 2>
<carets for error 2>
error: <message 2>
```

### 7.2 Processing Order

Errors are processed and displayed in the order they appear in the errors slice. The analyzer typically produces errors in the order they are encountered during parsing.

### 7.3 Example

Two errors require two independently unclosed constructs. (A `'` inside open double quotes is literal — quoting.md §5.3 — so an input like `"unclosed && 'also` produces only ONE error, for the double quote.) An unclosed substitution containing an unclosed quote produces two:

Input: `echo $(foo "bar`

Errors:
1. Start=5, End=15, Message="unclosed double quote"
2. Start=5, End=15, Message="unclosed command substitution $(...)"

Output:
```
echo $(foo "bar
     ^^^^^^^^^^
error: unclosed double quote

echo $(foo "bar
     ^^^^^^^^^^
error: unclosed command substitution $(...)
```

### 7.4 Same Line, Different Errors

Multiple errors on the same line each produce their own three-line block. The input line is repeated for each error.

---

## 8. Example Outputs for Each Error Type

### 8.1 Unexpected Operator at End

**Input:** `echo hello |`

**Analysis:** The pipe operator at position 11-12 has no following command.

**Output:**
```
echo hello |
           ^
error: unexpected operator at end
```

**Additional examples:**
```
echo hello &&
           ^^
error: unexpected operator at end
```

```
echo hello ||
           ^^
error: unexpected operator at end
```

**Note:** Trailing semicolon (`;`) is NOT an error - it is valid syntax.

### 8.2 Missing Redirection Target

**Input:** `echo >`

**Analysis:** The `>` operator at position 5-6 has no target file.

**Output:**
```
echo >
     ^
error: missing redirection target
```

**Additional examples:**
```
echo >>
     ^^
error: missing redirection target
```

```
echo <
     ^
error: missing redirection target
```

```
echo 2>
     ^^
error: missing redirection target
```

```
echo 2>>
     ^^^
error: missing redirection target
```

### 8.3 Unclosed Single Quote

**Input:** `echo 'hello`

**Analysis:** The token starting at position 5 contains a single-quote region that is still open at end of input (quoting.md §5.5).

**Output:**
```
echo 'hello
     ^^^^^^
error: unclosed single quote
```

**Additional example:**

**Input:** `echo 'it` (a single unclosed quote)

**Output:**
```
echo 'it
     ^^^
error: unclosed single quote
```

(Note: `echo 'it's` would be VALID — the `'` in `it's` is attached to text on both sides, so it CLOSES the region (quoting.md §5.2); the command prints `its`. An even quote count alone proves nothing: `echo 'a 'b` has two quotes and is still unclosed, because the second one NESTS.)

### 8.4 Unclosed Double Quote

**Input:** `echo "hello`

**Analysis:** The token starting at position 5 contains a double-quote region that is still open at end of input (quoting.md §5.5).

**Output:**
```
echo "hello
     ^^^^^^
error: unclosed double quote
```

**Additional example:**

**Input:** `echo "a"b"`

**Output:**
```
echo "a"b"
     ^^^^^
error: unclosed double quote
```

### 8.5 Unclosed Backtick

**Input:** ``echo `hello`` (the input is `echo`, a space, one backtick, `hello` — no backslash)

**Analysis:** The token starting at position 5 contains a backtick substitution that is still open at end of input (quoting.md §5.5).

**Output:**
~~~
echo `hello
     ^^^^^^
error: unclosed backtick
~~~

### 8.6 Unclosed Command Substitution $(...)

**Input:** `echo $(date`

**Analysis:** The `$(` at position 5 has no matching `)`.

**Output:**
```
echo $(date
     ^^^^^^
error: unclosed command substitution $(...)
```

**Additional example with content:**

**Input:** `echo $(cat /etc/passwd`

**Output:**
```
echo $(cat /etc/passwd
     ^^^^^^^^^^^^^^^^^
error: unclosed command substitution $(...)
```

---

## 9. Edge Cases and Special Handling

### 9.1 Empty Input

When input is empty but an error is provided:
- Line 1 will be empty
- Line 2 may contain carets (implementation-dependent for edge positions)
- Line 3 contains the error message

### 9.2 Error Position Beyond Input

When error positions exceed input length:
- Positions are clamped to valid bounds
- The diagnostic still displays the error message
- Carets may not visually align with any character

### 9.3 Zero-Length Errors

When `Start == End`:
- At least one caret is displayed
- The caret appears at position `Start`

### 9.4 Reversed Positions

When `Start > End`:
- Treated as a single-character error at position `Start`
- Equivalent to `End = Start + 1`

### 9.5 Negative Positions

When `Start < 0`:
- Clamped to 0 for display purposes
- Diagnostic is still produced

### 9.6 Unicode Characters

The diagnostic system operates on runes (Unicode code points), not bytes:
- Positions refer to character positions
- Multi-byte UTF-8 characters count as single positions
- Caret alignment is character-based

### 9.7 Tab Characters

Tab characters in input:
- Are preserved as-is in Line 1
- Count as single characters for position calculation
- May cause visual misalignment of carets (tabs render as variable-width)

### 9.8 No Errors

When the error slice is empty or nil:
- `FormatDiagnostics` returns an empty string (`""`)
- No output is produced

---

## 10. API Reference

### 10.1 FormatDiagnostics

```go
func FormatDiagnostics(input string, errors []SyntaxError) string
```

**Parameters:**
- `input`: The original input string that was analyzed
- `errors`: A slice of SyntaxError structs, or nil

**Returns:**
- Formatted diagnostic string, or empty string if no errors

**Behavior:**
- Returns `""` if `errors` is nil or empty
- Processes each error sequentially
- Separates multiple error blocks with a blank line (§7.1)
- Non-empty output ends with exactly one trailing newline

### 10.2 FormatDiagnosticsFromResult

```go
func FormatDiagnosticsFromResult(result *AnalysisResult) string
```

**Parameters:**
- `result`: An AnalysisResult from the Analyze function, or nil

**Returns:**
- Formatted diagnostic string, or empty string if no errors

**Behavior:**
- Returns `""` if `result` is nil
- Reconstructs input from tokens if needed
- Delegates to FormatDiagnostics

---

## 11. Integration with Analyzer

### 11.1 Error Generation

The `Analyze` function populates the `Errors` slice in `AnalysisResult` with `SyntaxError` structs. Error detection occurs:

1. **During token parsing**: Unclosed quotes, backticks, and command substitutions
2. **After parsing completes**: Trailing operators and missing redirection targets

### 11.2 Error Position Accuracy

Error positions correspond to:
- **Token-based errors** (unclosed quotes): The entire token span
- **Operator errors** (trailing operator): The operator's exact character positions

### 11.3 Valid Input

For valid input:
- `AnalysisResult.Valid == true`
- `AnalysisResult.Errors` is an empty slice
- `FormatDiagnostics` returns `""`

---

## 12. Test Validation Requirements

Tests validating against this specification MUST:

1. Match error messages exactly (case-sensitive) against the canonical table (§5)
2. Verify caret count equals `End - Start` (minimum 1)
3. Verify caret alignment starts at `Start` position
4. Verify the three-line format for each error
5. Verify blank-line separation between multiple error blocks and the single trailing newline (§7.1)
6. Handle edge cases per Section 9

### 12.1 Test Template

~~~go
func TestDiagnostic_ErrorName(t *testing.T) {
    input := `<input with error>`
    result := Analyze(input)

    // Verify error detected
    if result.Valid {
        t.Fatal("expected invalid result")
    }

    // Verify error message
    found := false
    for _, err := range result.Errors {
        if err.Message == "<exact message>" {
            found = true
            // Verify positions
            if err.Start != <expected_start> {
                t.Errorf("wrong start position")
            }
            if err.End != <expected_end> {
                t.Errorf("wrong end position")
            }
        }
    }
    if !found {
        t.Errorf("expected error message not found")
    }

    // Verify formatted output
    output := FormatDiagnostics(input, result.Errors)

    // Check contains input line
    if !strings.Contains(output, input) {
        t.Error("missing input line")
    }

    // Check caret count
    caretCount := strings.Count(output, "^")
    expectedCarets := <expected_end> - <expected_start>
    if caretCount != expectedCarets {
        t.Errorf("expected %d carets, got %d", expectedCarets, caretCount)
    }

    // Check error message present
    if !strings.Contains(output, "error: <exact message>") {
        t.Error("missing error message")
    }
}
~~~

---

## Appendix A: Complete Error Catalog

| Condition | Message | Typical Start | Typical End |
|-----------|---------|---------------|-------------|
| Leading chain operator | `unexpected operator at start: <op>` | Operator position | Operator position + len(op) |
| Trailing `\|` | `unexpected operator at end` | Operator position | Operator position + 1 |
| Trailing `&&` | `unexpected operator at end` | Operator position | Operator position + 2 |
| Trailing `\|\|` | `unexpected operator at end` | Operator position | Operator position + 2 |
| Missing redirect target `>` | `missing redirection target` | Operator position | Operator position + 1 |
| Missing redirect target `>>` | `missing redirection target` | Operator position | Operator position + 2 |
| Missing redirect target `<` | `missing redirection target` | Operator position | Operator position + 1 |
| Missing redirect target `2>` | `missing redirection target` | Operator position | Operator position + 2 |
| Missing redirect target `2>>` | `missing redirection target` | Operator position | Operator position + 3 |
| Unclosed single quote | `unclosed single quote` | Token start | Token end |
| Unclosed double quote | `unclosed double quote` | Token start | Token end |
| Unclosed backtick | `unclosed backtick` | Token start | Token end |
| Unclosed `$(` | `unclosed command substitution $(...)` | Token start | Token end |

---

## Appendix B: ABNF Grammar for Diagnostic Output

```abnf
diagnostic-output = error-block *(2LF error-block) LF
                  ; blocks separated by a blank line (two LFs);
                  ; output ends with exactly one trailing LF.
                  ; With no errors the output is the empty string
                  ; (this grammar covers one or more errors).

error-block       = input-line LF caret-line LF error-line

input-line        = *CHAR  ; Original input line content

caret-line        = *SP 1*CARET
SP                = %x20   ; Space character
CARET             = "^"

error-line        = "error: " message
message           = 1*CHAR ; Error message text

LF                = %x0A   ; Line feed
CHAR              = %x02-09 / %x0B-0C / %x0E-7F  ; Any character except LF
                  ; (%x01 is excluded: U+0001 is the reserved escape
                  ;  marker byte - see lexer.md 5.1.4)
```
