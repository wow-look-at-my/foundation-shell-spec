---
title: Quoting Specification
description: Quote semantics for single, double, and backtick quotes, including the depth-tracked nesting rule — Foundation Shell's flagship non-POSIX feature.
recommend_after: parser.md
---

# Quoting Specification

> **Canonical for:** quote semantics (what quoting means for tokens and expansion), the quote nesting rule and depth tracking, and the quote-isolation rules. For tokenization mechanics see lexer.md; the authority map lives in the README.

> **⚠️ NON-POSIX BEHAVIOR.** Foundation Shell quoting deliberately diverges from POSIX and from every sh-family shell: same-type quote characters can NEST inside an open quoted region (§5.2, §6). Some valid POSIX inputs are errors here, and some produce different arguments than a POSIX shell would (§13). Do not carry POSIX quoting idioms over unreviewed — in particular, `"label: "$VAR`-style concatenation is an unclosed-quote error here (§13.3).

## 1. Overview

Foundation Shell defines a quoting system that controls how text is interpreted, whether expansions occur, and how special characters are treated.

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

### 1.3 One Scanner, Two Views

Foundation Shell has ONE scanner. The scan (lexer.md) walks the input once and returns tokens. Each token carries everything any consumer needs. That is the text as written with its rune span, the expansion-ready content with its quoting flags, the structural role, and the nesting depth. Whitespace and comments are tokens too. The stream therefore accounts for every rune of the input.

Two views project from it, and neither re-scans:

1. **Execution** (lexer.md): it drops whitespace and comments, and it materializes the `;` an unquoted newline stands for. It hands the parser `TokenContext` values whose outermost quote delimiters are already removed.

2. **Analysis** (highlighting.md): classifies the same tokens for the theme and renders positioned diagnostics. It keeps every character, because it must paint the line the user typed.

Because both read one scan, an input cannot be valid to one and invalid to the other — not by agreement, but by construction. The quote-state semantics below (the open/nest/close rule of §5.2 with the context rules of §5.3) are therefore stated once and implemented once.

The alternative is a second scanner behind the analysis view. That design is forbidden here. Two scanners over one grammar drift apart, and the drift is invisible: a line gets highlighted as one thing and runs as another.

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
| **Same-Type Nesting** | A `'` inside the region nests or closes by the §5.2 rule (§6.1) |
| **Quote Removal** | The outermost quote pair is removed; nested quote characters remain literally (§6) |

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

### 2.3 Single Quotes Inside Single Quotes

Two mechanisms put a literal `'` inside a single-quoted argument:

**1. Whitespace-delimited nesting (Foundation Shell extension — §6).** A `'` preceded by whitespace and followed by a non-whitespace, non-quote rune NESTS instead of closing (§5.2). Nested quote characters stay in the argument literally:

```
Input:  echo 'it 'really' works'
Output: it 'really' works
```

**2. The close–escape–reopen dance (POSIX-compatible — for attached apostrophes).** A `'` attached to non-whitespace text CLOSES the region, so an apostrophe inside a word cannot nest:

```
# ERROR - the ' in it's closes the region
echo 'it's broken'    # error: unclosed single quote
# Trace: OPEN at 'it, CLOSE at t' (previous rune not whitespace), "broken"
# is then unquoted, and the final ' OPENs at depth 0 and never closes.

# WORKAROUND - End quotes, add escaped quote, restart quotes
echo 'it'\''s working'
# Produces: it's working
```

The workaround works by:
1. `'it'` - single-quoted "it" (the `'` after `t` closes: previous rune is not whitespace)
2. `\'` - escaped single quote, outside single quotes (escapes never touch depth — §5.2)
3. `'s working'` - single-quoted "s working"

These concatenate into a single token: `it's working`

### 2.4 WasSingleQuoted Flag

The lexer records whether any part of a token was single-quoted. The `WasSingleQuoted` flag in `TokenContext` carries that fact (lexer.md §2.1). Single-quoted content also sets the broader `WasQuoted` flag:

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
| **Same-Type Nesting** | A `"` inside the region nests or closes by the §5.2 rule (§6.2) |
| **Quote Removal** | The outermost quote pair is removed; nested quote characters remain literally (§6) |

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

Whitespace-delimited nesting is the escape-free alternative (§6.2): `echo "path is "$HOME" here"` keeps the nested pair literally.

### 3.5 Preventing Expansion

To include a literal `$` that must not trigger expansion:

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
| **Same-Type Nesting** | A backtick inside a backtick body nests or closes by the §5.2 rule; nested backticks execute recursively (§6.3) |

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

# Both nest without escaping: $() by explicit delimiters,
# backticks by the nesting rule (§5.2, §6.3)
echo $(echo $(date))
echo `echo `date``
```

`$(...)` remains the recommended syntax: its delimiters are visually unambiguous at any depth, while backtick nesting depends on the §5.2 neighbor conditions.

### 4.4 Backtick Depth Tracking

Backtick state uses the same open/nest/close rule as the quote types (§5.2), applied when backticks are active (not inside single quotes — §5.3):

- **Depth 0:** a backtick always opens a substitution.
- **Depth ≥ 1:** a backtick NESTS one level, with whitespace before it and a non-whitespace, non-quote rune after it. Otherwise it CLOSES one level.
- Nested backticks stay in the body literally. They execute recursively when the body is re-parsed (§6.3, lexer.md §7.1).

A backtick region still open at end of input is the canonical error `unclosed backtick` (§5.5). With nesting, even an EVEN number of backticks can be unclosed:

```
Input:  echo `date
Error:  unclosed backtick

Input:  echo `a `b
Error:  unclosed backtick    (2 backticks: the 2nd NESTS — space before, b after)
```

For a literal backtick *character*, escape it: `` \` `` is marked by the lexer and never treated as a delimiter (lexer.md §5.1). In Foundation Shell an escaped backtick is ALWAYS a literal character — it is never the POSIX-style nesting device (§6.3).

---

## 5. Quote State Tracking

Foundation Shell tracks quote state with per-type depth counters driven by a single open/nest/close rule. This section is the **normative statement of the rule**. The scanner (§1.3) applies it once, and both views inherit it.

### 5.1 Per-Type Depth Counters

Each quote type has an independent depth counter that counts how many regions of that type are currently open:

| Counter | Driven by | Meaning when > 0 |
|---------|-----------|------------------|
| `singleQuoteDepth` | `'` | Inside (possibly nested) single-quote region(s) |
| `doubleQuoteDepth` | `"` | Inside (possibly nested) double-quote region(s) |
| `backtickDepth` | `` ` `` | Inside (possibly nested) backtick substitution(s) |
| `parenDepth` | `$(` / `)` | Inside (possibly nested) `$(...)` substitution(s) |

Depth 0 means no region of that type is open. Depths never go negative (rule 1 below). A quote character drives its counter only when it is unescaped and **active** in the current context (§5.3). Each command-substitution body begins a fresh quote context (§5.3).

`parenDepth` needs no special rule: `$(` and `)` are distinct delimiters, so `$(` always increments and a closing `)` (one not inside an open body quote region) always decrements. The rule below exists because the three quote types use the SAME character to open and close.

### 5.2 The Open / Nest / Close Rule

For an **unescaped, active** quote character of a given type, with `depth` = that type's current counter:

1. **`depth == 0` → OPEN.** The character always opens a region of its type: depth becomes 1. Neighboring characters are irrelevant.
2. **`depth >= 1` → NEST or CLOSE.** The character **nests** one level deeper (`depth+1`) **iff ALL of**:
   - **(a)** the previous input rune is whitespace
   - **(b)** a next input rune exists and is not whitespace
   - **(c)** the next input rune is not any quote character (`'`, `"`, or `` ` ``).

   Otherwise it **closes** one level (`depth-1`). The region ends when depth returns to 0.
3. **Literal retention.** Nested quote characters — every transition that does not touch depth 0 — remain **literally** in the argument. Only the outermost pair (the 0→1 opener and the 1→0 closer) is removed as delimiters (§8.1). Inside command-substitution bodies, even the outermost pair is preserved verbatim for the recursive parse (lexer.md §7.1).
4. **Textual neighbor test.** Conditions (a)–(c) examine the raw adjacent runes without interpreting them. A backslash counts as the rune `\`, and an already-processed delimiter counts as its quote rune. "Whitespace" is the same class used for word splitting (lexer.md §3.1.1). The previous rune always exists at depth ≥ 1 (at minimum it is the opener).
5. **Escapes are invisible to the rule.** Escaped quote characters (backslash escape or escape marker, lexer.md §5) are consumed by escape processing and never reach this rule (§7.5). They neither open, nest, nor close.

Intuition: a quote character nests only when it *looks like it is opening an interior quoted phrase*. A space precedes it, and visible content that is not itself a quote follows it. Every other position closes, exactly as in POSIX. Those positions are: attached to text (`it's`), at a word end or input end (`'a '`), and adjacent to another quote (`''`, the §12.6 interleave).

#### 5.2.1 Decision Table

For a same-type quote character at depth ≥ 1:

| Previous rune | Next rune | Action | Why |
|---------------|-----------|--------|-----|
| whitespace | non-whitespace, non-quote | **NEST** (+1) | Reads as the start of an interior quoted phrase |
| whitespace | whitespace | CLOSE (−1) | `echo 'a ' b` — a terminator |
| whitespace | none (end of input) | CLOSE (−1) | `echo 'a '` — a terminator |
| whitespace | `'`, `"`, or `` ` `` | CLOSE (−1) | Adjacent quotes mean POSIX-style close-and-reopen concatenation (`'a '' b'`, §12.6) |
| non-whitespace | (anything) | CLOSE (−1) | `it's`, `'a'b`, `''` — attached quotes close |

At depth 0 the character always OPENS, whatever its neighbors.

#### 5.2.2 Reference Algorithm

Rune-based pseudocode for the scanner. It records positions and keeps the text as written for the analysis view. In the same pass it builds the delimiter-stripped content for the execution view (lexer.md §10.2):

```go
type Action int

const (
    Open Action = iota
    Nest
    Close
)

// quoteAction decides what one unescaped, ACTIVE quote character at rune
// index i does. depth is the current counter of that quote type in the
// current context.
func quoteAction(input []rune, i, depth int) Action {
    if depth == 0 {
        return Open // rule 1: depth 0 always opens
    }
    prevIsSpace := unicode.IsSpace(input[i-1]) // depth >= 1 implies i >= 1
    nextExists := i+1 < len(input)
    nextIsSpace := nextExists && unicode.IsSpace(input[i+1])
    nextIsQuote := nextExists &&
        (input[i+1] == '\'' || input[i+1] == '"' || input[i+1] == '`')
    if prevIsSpace && nextExists && !nextIsSpace && !nextIsQuote {
        return Nest // rule 2: depth+1; the character stays in the argument
    }
    return Close // rule 2: depth-1; region ends when depth returns to 0
}
```

Applying it. Single quotes are shown here. The other types are identical in shape:

```go
if c == '\'' && doubleQuoteDepth == 0 && backtickDepth == 0 { // active? (§5.3)
    switch quoteAction(input, i, singleQuoteDepth) {
    case Open, Nest:
        singleQuoteDepth++
        openPos = append(openPos, i) // region starts, for unclosed-error spans
    case Close:
        singleQuoteDepth--
        openPos = openPos[:len(openPos)-1]
    }
}
```

The transition decides whether the character is emitted or stripped. The lexer strips only the outermost delimiters, which are an OPEN from 0 and a CLOSE to 0, outside any substitution body. It keeps every nested quote character in the token (lexer.md §10.2).

### 5.3 Quote Isolation and Context Rules

Within an open quote region, only SAME-TYPE quote characters are subject to the §5.2 rule. Different-type quote characters are literal content — with one exception: command substitutions stay active inside double quotes:

| Open region | `'` | `"` | `` ` `` | `$(` |
|-------------|-----|-----|---------|------|
| Single-quote region | same-type rule (§5.2) | literal | literal | literal |
| Double-quote region | literal | same-type rule (§5.2) | active (opens a substitution) | active (opens a substitution) |

**Command-substitution bodies** (`$(...)` and backticks) each begin a **fresh quote context**. The scanner saves the enclosing depths. It restores them when the body closes. The body's quote characters stay verbatim in the token. The scanner still TRACKS them, under this same rule, to find the body's delimiter (lexer.md §7.1):

- Inside a `$(...)` body, `'` and `"` drive the body context's own counters. A `)` closes the innermost `$(` only when the body context has no open quote region and no open backtick.
- Inside a backtick body, single quotes are literal. Double quotes update the body context's counter, but they never prevent the body from closing. An unescaped backtick nests or closes the body by §5.2.
- The body is re-parsed from scratch when the substitution executes. The delimiter the scanner chose is therefore the delimiter the recursive parse sees.

### 5.4 Word Boundaries: Total Depth

The total depth is the sum of all four counters in the current context. Unquoted whitespace splits words **only at total depth 0**:

```go
totalDepth := singleQuoteDepth + doubleQuoteDepth + backtickDepth + parenDepth
// whitespace is a word boundary iff totalDepth == 0
```

The same condition gates operator recognition (lexer.md §3.2, §10.2). Whitespace inside any open region — including nested levels — is content.

### 5.5 Validation at End of Input

At end of input every counter must be 0. Any counter still positive reports the canonical error for its type (diagnostics.md §5):

```go
if singleQuoteDepth > 0 {
    errors = append(errors, "unclosed single quote")
}
if doubleQuoteDepth > 0 {
    errors = append(errors, "unclosed double quote")
}
if backtickDepth > 0 {
    errors = append(errors, "unclosed backtick")
}
if parenDepth > 0 {
    errors = append(errors, "unclosed command substitution $(...)")
}
```

Two consequences of the nesting rule, spelled out because they differ from parity-based models:

- **An EVEN quote count can be unclosed.** `echo 'a 'b` contains two single quotes, yet the second one NESTS (whitespace before, `b` after), so depth ends at 2 → `unclosed single quote`. This is exactly why the canonical error strings carry no "(odd count)" suffix.
- **An odd quote count is always unclosed.** Every quote character changes its counter by ±1, so returning to 0 needs an even number of them. The converse does not hold — validity is a depth check, never a parity check.

---

## 6. Nested Quotes

Depth-tracked quote nesting is Foundation Shell's flagship non-POSIX feature. A same-type quote pair can appear INSIDE an open quoted region, delimited by the neighbor conditions of §5.2. The nested quote characters stay in the argument literally. This section shows the rule in action. The normative statement is §5.2. For what nesting buys and costs against POSIX, see §13.

### 6.1 Nested Single Quotes

```
Input:  echo 'outer 'inner' end'
```

| Quote | Prev rune | Next rune | Action (§5.2) | Depth after |
|-------|-----------|-----------|---------------|-------------|
| 1st `'` | space | `o` | depth 0 → OPEN | 1 |
| 2nd `'` | space | `i` | whitespace before; non-space, non-quote after → NEST | 2 |
| 3rd `'` | `r` | space | previous rune not whitespace → CLOSE | 1 |
| 4th `'` | `d` | (end) | previous rune not whitespace → CLOSE | 0 |

Result: ONE argument. The nested pair stays literally. The outermost pair is stripped:

```
Tokens: [{Content: "echo"},
         {Content: "outer 'inner' end", WasSingleQuoted: true, WasQuoted: true}]
Output: outer 'inner' end
```

The interior whitespace does not split the word: total depth (§5.4) never returns to 0 inside the region.

Nesting is not limited to one level:

```
Input:  echo 'l1 'l2 'l3' l2' l1'
Depths:      1   2   3  2   1   0
Result: one argument: l1 'l2 'l3' l2' l1
```

(The 2nd and 3rd quotes NEST — whitespace before, letters after. The 4th–6th CLOSE — each has a non-whitespace rune before it.)

#### 6.1.1 POSIX-Identical Anchors

The neighbor conditions deliberately make the common POSIX patterns behave exactly as POSIX does — attached closers, adjacent pairs, empty strings:

| Input | Trace (§5.2) | Result |
|-------|--------------|--------|
| `echo 'a' 'b'` | 2nd `'` prev=`a` → CLOSE; 3rd at depth 0 → OPEN | two args: `a`, `b` |
| `echo 'a'b` | 2nd `'` prev=`a` → CLOSE; `b` concatenates | one arg: `ab` |
| `echo 'a''b'` | CLOSE (prev=`a`), OPEN (depth 0), CLOSE (prev=`b`) | one arg: `ab` |
| `echo ''` | 2nd `'` prev=`'`, not whitespace → CLOSE | one empty arg |
| `echo ' '` | 2nd `'` prev=space but no next rune → CLOSE | one arg: one space |
| `echo 'don'\''t'` | CLOSE (prev=`n`); escaped `\'` never reaches the rule; OPEN; CLOSE (prev=`t`) | one arg: `don't` |

### 6.2 Nested Double Quotes

The same rule, same shape:

```
Input:  echo "outer "inner" end"
Depths:      1      2      1    0
Tokens: [{Content: "outer \"inner\" end", WasQuoted: true}]
Output: outer "inner" end
```

Double-quote semantics inside a nested region are unchanged (§3): `$` still expands and escapes are still processed — the nested `"` characters are simply literal content:

```
Input:  echo "path "$HOME" here"
Depths:      1     2      1     0
Output: path "/home/user" here    ($HOME expands; the nested quotes print)
```

### 6.3 Nested Backticks (Recursive Command Substitution)

Backticks follow the same rule. A backtick region is a command substitution, so nesting here has EXECUTION semantics. The body keeps its nested backticks literally. Execution re-parses that body, and the nested backticks therefore execute **recursively**.

```
Input:  echo `outer `inner` end`
Depths:      1      2      1    0
```

The lexer emits one token containing the whole substitution (lexer.md §7). When it executes:

1. The outer body is `` outer `inner` end ``.
2. Re-parsing the body finds `` `inner` `` at depth 0 — an inner substitution.
3. `inner` runs first. Its output substitutes into the body. Then `outer <output> end` runs, and the outer output replaces the original expression.

```
Input:  echo `echo `date``
# 2nd backtick NESTS (space before, d after) -> depth 2
# 3rd backtick CLOSES (prev=e) -> depth 1
# 4th backtick CLOSES (prev is the 3rd backtick, not whitespace) -> depth 0
# Executes date, then echo <date output>.
```

POSIX backticks cannot nest without escaping. The Foundation Shell difference runs in the other direction too. An ESCAPED backtick (`` \` ``) is always a literal backtick *character* here (lexer.md §5.1–5.2). It is never the POSIX-style nesting mechanism. To nest, just nest, under the rule above. To get a literal backtick character, escape it.

An unclosed backtick region reports `unclosed backtick` (§5.5). This covers the even-count case too. In `` echo `a `b ``, the second backtick nests, and the input ends at depth 2.

### 6.4 Mixed Quote Types

Different-type quote characters inside an open region are literal content (§5.3) — the cross-type rules are unchanged by nesting:

```
Input:  echo 'say "hello"'
Output: say "hello"           (double quotes literal inside single quotes)

Input:  echo "it's fine"
Output: it's fine             (single quote literal inside double quotes)

Input:  echo "run `date` now"
# Backticks stay ACTIVE inside double quotes: the substitution executes.
```

Each type nests only against itself. The depth counters are independent of each other (§5.1).

### 6.5 Command-Substitution Depth (`$(...)`)

`$(...)` nests by explicit delimiters — `$(` always opens, and a `)` at body depth 0 always closes (§5.1, lexer.md §7.1). The §5.2 neighbor rule is not involved:

```go
// Handle $( command substitution
if c == '$' && singleQuoteDepth == 0 && a.pos+1 < len(a.input) && a.input[a.pos+1] == '(' {
    parenDepth++
    // ... push a fresh body quote context (§5.3)
}

// Handle closing ) — only when no body quote region is open
if c == ')' && parenDepth > 0 &&
    singleQuoteDepth == 0 && doubleQuoteDepth == 0 && backtickDepth == 0 {
    parenDepth--
    // ... pop the body context
}
```

Command substitutions can be arbitrarily nested:

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

U+0001 is a reserved internal byte. Input that contains a literal U+0001 has undefined behavior (lexer.md §5.1.4).

### 7.2 Escape Marker Lifecycle

1. **Lexer stage**: `\$VAR` becomes `\x01$VAR` in token content
2. **Expander stage**: Sees `\x01$`, recognizes escaped dollar, keeps `$VAR` literal
3. **Post-expansion**: `StripEscapeMarkers()` removes remaining `\x01`, unconditionally, for every value token (lexer.md §11.3)

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

### 7.5 Escaped Quotes and Depth Tracking

The scanner consumes an escape sequence as a unit BEFORE quote-state tracking sees it. An escaped quote character can therefore never open, nest, or close a region (§5.2 rule 5):

```go
// Handle escape sequences (outside single-quote regions: singleQuoteDepth == 0)
if c == '\\' && singleQuoteDepth == 0 && a.pos+1 < len(a.input) {
    next := a.input[a.pos+1]
    // The escaped character never reaches quoteAction (§5.2.2):
    // it cannot affect any depth counter.
    builder.WriteRune(c)
    builder.WriteRune(next)
    a.pos += 2
    continue
}
```

This keeps `\"` from touching double-quote depth, and `\'` / `` \` `` from touching theirs. The neighbor test stays unchanged in the other direction. As raw runes, a backslash and an already-processed delimiter each take part in conditions (a)–(c) like any other rune (§5.2 rule 4).

---

## 8. Quote Removal During Word Processing

### 8.1 When Quotes Are Removed

Quote characters (single and double) acting as DELIMITERS are **removed** during lexer tokenization. Only the outermost pair of each region is a delimiter — nested quote characters (§6) are argument content and remain. The output token contains only the content:

```
Input:  echo "hello world"
Token:  {Content: "hello world", WasQuoted: true}
# Note: The " characters are not in Content

Input:  echo 'hello world'
Token:  {Content: "hello world", WasSingleQuoted: true, WasQuoted: true}
# Note: The ' characters are not in Content
```

**Exception:** quote characters inside a command-substitution body are NOT delimiters at the outer level. The token keeps them verbatim. They take effect when the body is re-parsed (lexer.md §7.1):

```
Input:  echo $(echo "a  b")
Token:  {Content: "$(echo \"a  b\")", WasQuoted: false}
```

### 8.2 How Quote Removal Works

The lexer applies the §5.2 rule to each active quote character and emits or skips it depending on the transition. The full state machine is normative in lexer.md §10.2. In outline:

| Transition (§5.2) | Inside a substitution body? | Character emitted? | Flags |
|-------------------|-----------------------------|--------------------|-------|
| OPEN (0→1) or CLOSE (1→0) | no | NO — outermost delimiter, removed | `WasSingleQuoted`/`WasQuoted` set (per type) |
| NEST or nested CLOSE (never touching 0) | no | YES — literal argument content | already set by the outermost OPEN |
| any | yes | YES — body preserved verbatim | body quotes set no flags (lexer.md §7.1) |

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

The quoting flags reset at every word boundary. The quotes of an empty token therefore never leak into the next word. In `echo '' $HOME`, the `$HOME` token has `WasSingleQuoted: false`, and the expander expands it.

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
    Depth int  // Max nesting level reached in this token (§5.4 high-water mark)
}
```

`Depth` is the maximum nesting level reached within the token, across quote regions and command substitutions alike. It is the high-water mark of the total depth (§5.4) over the token's span. The canonical definition and its examples live in highlighting.md §9.4. A plain `'a'` has Depth 1. The token `'a 'b' c'` has Depth 2, and unquoted `foo` has Depth 0.

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

### 10.3 Innermost-First Through Recursion

Nested substitutions complete innermost-first as a CONSEQUENCE of recursion. No textual re-scanning is involved. The same parser and executor re-parse the substitution body. Substitutions inside that body therefore expand during the recursive parse (expansion.md §Recursive Execution).

Substitution OUTPUT is never re-scanned for further substitutions — `$(...)` or backticks appearing in a command's output are literal text (expansion.md §Single-Pass Expansion).

### 10.4 Suppression by Single Quotes

Command substitution is NOT performed when the token was single-quoted:

```
Input:  echo '$(date)'
Output: $(date)    (literal text)

Input:  echo "$(date)"
Output: Mon Jan 12 10:30:00 UTC 2026
```

### 10.5 Balanced Parentheses

The `$()` syntax requires balanced parentheses. The count uses the BODY's own quote state (lexer.md §7.1 rule 4). A `(` or `)` does not count when it sits inside an open single-quote, double-quote, or backtick region of the body. A backslash-escaped one does not count either. The scan operates on runes:

```go
// Quote-state aware: parens inside quoted body regions and escaped
// parens are skipped (lexer.md §7.1 rule 4)
func findMatchingParen(runes []rune, startIdx int) int {
    depth := 1
    for i := startIdx; i < len(runes); i++ {
        // ... skip escaped runes and track the body's quote depths ...
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

This is what makes `echo $(echo ")")` valid: the quoted `)` is body content, not a closing delimiter (§10.1 rule 8).

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
# The 2nd quote NESTS (space before, i after); the 3rd and 4th CLOSE
Input:  echo 'outer 'inner' outer'
Depths: 0    1      2      1      0
Valid:  YES -> one argument: outer 'inner' outer

# Same input truncated: depth never returns to 0
Input:  echo 'outer 'inner'
Depths: 0    1      2      1
Valid:  NO - unclosed single quote

# EVEN quote count, still unclosed: the 2nd quote NESTS (§5.5)
Input:  echo 'a 'b
Depths: 0    1   2
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

# Interleaved quoting (POSIX-style concatenation - unchanged by nesting)
Input:  echo "Hello, "'"'"$USER"'"'"!"
Token:  {Content: "Hello, \"$USER\"!", WasSingleQuoted: true, WasQuoted: true}
Output: Hello, "$USER"!    (NOT expanded - WasSingleQuoted suppresses expansion)
# Every interior quote character either follows a non-whitespace rune or is
# followed by another quote character, so each one CLOSES (§5.2 exclusion (c)) -
# the input concatenates exactly as in POSIX. Matches lexer.md §12.6.

# Nesting showcase (Foundation Shell divergence - §6, §13)
Input:  echo 'a 'b' c' 'd'
Tokens: [echo, a 'b' c, d]
# POSIX produces [echo, a b c, d]; the nesting rule keeps the interior pair.
```

---

## 13. Differences from POSIX Shells

Quoting is where Foundation Shell most visibly diverges from POSIX. The divergences are deliberate. This section is the honest inventory. (See also the warning at the top of this file, and lexer.md §13 for tokenization-level differences.)

### 13.1 The Nesting Rule (Flagship)

POSIX quote characters strictly toggle. The first `'` opens, and the next `'` ALWAYS closes. Foundation Shell applies the open/nest/close rule instead (§5.2). A same-type quote character inside an open region NESTS when whitespace precedes it and a non-whitespace, non-quote rune follows it.

What it buys:

- **Literal same-type quotes inside a quoted argument, without escaping**: `echo 'it 'really' works'` → `it 'really' works`. POSIX needs `"it 'really' works"` (switching quote types) or escape dances.
- **Nested backticks that execute recursively, without escaping**: `` echo `outer `inner` end` `` runs `inner` inside the outer substitution (§6.3). POSIX backticks cannot nest unescaped.
- **No `'\''` dance for the whitespace-delimited case.** (Attached apostrophes still use it — `'don'\''t'`, §2.3.)

### 13.2 Same Input, Both Valid, Different Results

```
echo 'a 'b' c' 'd'
# POSIX:            args = [a b c, d]     (four delimiters + concatenation)
# Foundation Shell: args = [a 'b' c, d]   (outer pair + nested pair, §6.1)
```

Both shells accept this input. They disagree about what it means. The divergence is self-announcing. Whenever the nesting reading wins, the interior quote characters stay visibly in the argument (§5.2 rule 3). The output never silently *loses* structure against POSIX. It visibly keeps the quotes POSIX consumes. Expansion behaves the same way. POSIX expands the `$X` in `'a '$X' c'`, because that `$X` falls outside its quotes. Here it stays literal, wrapped in visible quotes.

### 13.3 The Costs: Valid POSIX Inputs That Are Errors Here

When the rule reads "nest" but the input never brings the depth back to 0, Foundation Shell rejects input POSIX accepts. Every such mis-guess is LOUD — an unclosed-quote error, never a silently different word:

| Input | POSIX result | Foundation Shell | Write instead |
|-------|--------------|------------------|---------------|
| `echo 'hello 'world` | `hello world` | error: `unclosed single quote` | `echo 'hello world'` |
| `echo ' 'x` | ` x` (space + x) | error: `unclosed single quote` | `echo ' x'` |
| `echo "Total: "$N` | `Total: ` + value of `$N` | error: `unclosed double quote` | `echo "Total: $N"` |
| `echo "count: "$(date)` | `count: ` + date output | error: `unclosed double quote` | `echo "count: $(date)"` |

In each row, whitespace precedes the quote character before the trailing text, and ordinary text follows it. That character therefore NESTS (§5.2). Depth ends above 0, and the input fails with the canonical unclosed error (§5.5). The rewrite is always the simpler one-region form. The close-then-concatenate idiom buys nothing here, because expansions already run inside double quotes.

### 13.4 POSIX-Identical Anchors

The neighbor conditions deliberately reduce to POSIX behavior for the common patterns. Each of `'a' 'b'`, `'a'b`, `'a''b'`, `''`, `' '`, `'don'\''t'`, and `"say \"hi\""` tokenizes exactly as POSIX tokenizes it (§6.1.1). So does an interleave such as §12.6's `echo "Hello, "'"'"$USER"'"'"!"`.

### 13.5 Other Deliberate Divergences

| Feature | POSIX Shell | Foundation Shell |
|---------|-------------|------------------|
| Expansion suppression granularity | Per character span: in `'a'$HOME`, `$HOME` expands | Whole token: any single-quoted part suppresses ALL expansion for the token (§2.5) |
| Tilde suppression granularity | `~/"docs"` expands the tilde | Any quoted part suppresses tilde expansion for the whole token (`WasQuoted`, §2.4) |
| Escaped backtick inside backticks | The nesting mechanism | Always a literal backtick character (§6.3; lexer.md §5.1) |
| `$'...'` ANSI-C quoting | Supported (Bash) | Not supported |
| `$"..."` locale translation | Supported (Bash) | Not supported |
| Line continuation `\<newline>` | Joins lines | Not supported |
| Here-documents `<<` / here-strings `<<<` | Supported | Not supported — guarded parse error (redirection.md §9.6) |
| `\` at EOL in double quotes | Line continuation | Not supported |
| U+0001 in input | Ordinary data | Reserved internal byte; behavior undefined (lexer.md §5.1.4) |

Two behaviors are POSIX-identical by design. They are stated here to prevent doubt. First, an empty quoted string produces an empty argument (lexer.md §4.5). Second, quoting or escaping an operator character makes it literal, and operators are recognized only outside quotes and substitutions (lexer.md §3.3.4).

---

## 14. Reference

### 14.1 Key Functions

```go
// Syntax Analysis
func Analyze(input string) *AnalysisResult

// Tokenization
func Tokenize(input string) ([]TokenContext, error)

// Expansion (pipeline and suppression flags: expansion.md §Expansion Order)
func ExpandTilde(token string) string
func ExpandEnvironment(token string, lastStatus int) string
func ExpandCommandSubstitution(token string, executor SubstitutionExecutor) (string, error)

// Utilities
func StripEscapeMarkers(s string) string
```

The name `SubstitutionExecutor` is deliberate (expansion.md). "Subshell" is reserved for the future `()` grouping construct.

### 14.2 Key Constants

```go
const EscapeMarker = '\x01'  // Marks escaped dollar signs and backticks
```

### 14.3 Key Data Structures

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

1. **Nesting decisions (§5.2)**: an open at depth 0. Each neighbor condition (a)–(c) flipping NEST to CLOSE on its own. An even-count unclosed input, such as `'a 'b`. An odd count, which is always unclosed
2. **Quote isolation**: Verify different-type quotes inside open regions are literal (§5.3)
3. **Escape processing**: Test all escape sequences in all contexts
4. **Concatenation**: Test adjacent quoted segments
5. **Empty strings**: Test that `""` and `''` produce empty tokens
6. **Nested structures**: Test deeply nested quotes and substitutions
7. **Substitution bodies**: quotes and escapes inside `$()` and backticks stay verbatim. A quoted `)` does not close the body
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

# Multi-level same-type nesting (§6.1)
echo 'l1 'l2 'l3' l2' l1'

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
5. **Depth-tracked nesting** (§5, §6). Same-type quotes can nest. This is the flagship non-POSIX feature
6. **Quote removal** during tokenization, on the outermost pairs only. Nested quotes and substitution bodies stay
7. **WasSingleQuoted / WasQuoted flags** for expansion control

The system prioritizes:
- Safety (conservative single-quote propagation)
- Robustness (depth-based validation)
- Compatibility (standard shell conventions where practical)
- Clarity (explicit error messages)
