---
title: Expansion Specification
description: The expansion pipeline - tilde expansion, variable expansion including $?, and single-pass command substitution.
recommend_after: quoting.md
---

# Foundation Shell Expansion Specification

> **Canonical for:** the expansion pipeline — tilde expansion, variable expansion (including the special parameter `$?`), command substitution semantics, and expansion suppression. Quote *semantics* live in quoting.md; tokenization lives in lexer.md. The authority map lives in the README.

## Table of Contents

1. [Overview](#overview)
2. [Expansion Order](#expansion-order)
3. [Tilde Expansion](#tilde-expansion)
4. [Variable Expansion](#variable-expansion)
5. [Command Substitution](#command-substitution)
6. [Quoting and Expansion](#quoting-and-expansion)
7. [Escape Sequences](#escape-sequences)
8. [Word Splitting](#word-splitting)
9. [Special Parameters](#special-parameters)
10. [Edge Cases and Error Handling](#edge-cases-and-error-handling)

---

## Overview

Foundation Shell's expansion system transforms tokens through a series of ordered transformations. The expansion phase occurs **after** tokenization and **before** command execution: the parser expands every value token while building the command chain (parser.md §Step 2). Expansion never changes token boundaries.

### Key Principles

1. **Single-quoted tokens are never expanded** — a token with `WasSingleQuoted: true` skips tilde, variable, and command substitution expansion entirely
2. **Quoted tokens never tilde-expand** — a token with `WasQuoted: true` (any quote type, any part of the token) skips tilde expansion; variable expansion and command substitution still apply unless the token was single-quoted
3. **Expansion order is deterministic** — tilde, then variables, then command substitution, then escape-marker stripping
4. **Escape-marker stripping is unconditional** — it runs for EVERY value token, including single-quoted ones
5. **Expansion results are data, never code** — text produced by an expansion (a variable's value, a substitution's output) is never re-scanned for further expansions
6. **Non-existent variables expand to empty string** — no error is raised, and the empty result remains an (empty) argument
7. **No post-expansion word splitting** — an expanded value containing whitespace remains a single token
8. **Trailing newlines are trimmed from command substitution output** — standard shell behavior

---

## Expansion Order

Foundation Shell performs expansions in a strict, well-defined order.

### Order of Operations

```
1. Tilde Expansion       (~, ~/path)        - skipped if WasQuoted
2. Variable Expansion    ($VAR, ${VAR}, $?)
3. Command Substitution  ($(...), `...`)    - only if an executor is provided
   (steps 1-3 are all skipped if WasSingleQuoted)
4. Escape Marker Stripping                  - UNCONDITIONAL, every value token
```

### Processing Flow

```
Input Token (TokenContext)
    |
    v
[WasSingleQuoted?] ----yes----> (skip steps 1-3)
    |                                 |
    no                                |
    v                                 |
[WasQuoted?] --no--> [Tilde Expansion]|
    |                     |           |
    yes (skip tilde)      |           |
    +---------------------+           |
    v                                 |
[Variable Expansion]  $VAR ${VAR} $?  |
    |                                 |
    v                                 |
[Command Substitution]  $(...) `...`  |
    |                                 |
    +<--------------------------------+
    v
[Strip Escape Markers]  (unconditional)
    |
    v
Expanded Token
```

### Reference Algorithm

The parser applies the pipeline per token. `lastStatus` is the shell's last recorded command-line status (see [Special Parameters](#special-parameters) and execution.md §Last Exit Code). The shell supplies it for each parse:

```
# For each value token tc:
value = tc.Content

if not tc.WasSingleQuoted:
    if not tc.WasQuoted:
        value = EXPAND_TILDE(value)
    value = EXPAND_VARIABLES(value, lastStatus)
    if a substitution executor is available:
        value = EXPAND_COMMAND_SUBSTITUTION(value, executor)
        # A failure here fails the parse, reported as
        #   command substitution error: <reason>

value = STRIP_ESCAPE_MARKERS(value)   # unconditional (parser.md §Step 3)
```

Marker stripping is deliberately OUTSIDE the suppression block: a concatenation such as `'a'\$HOME` produces a `WasSingleQuoted` token that still carries a marker (lexer.md §11.3).

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
2. **Unquoted requirement**: Tilde expansion ONLY occurs when the token is fully unquoted (`WasQuoted: false`). Quoting ANY part of the token suppresses it
3. **Home directory source**: The value is read from the `HOME` environment variable
4. **Missing HOME**: If `HOME` is not set, the tilde is left unexpanded
5. **User expansion**: The `~username` syntax is NOT supported and is left as-is
6. **Shape requirement**: Only bare `~` and `~/...` expand. A tilde followed by anything other than `/` (or end of token) is left unchanged

### Whole-Token Granularity (Documented Deviation)

The `WasQuoted` flag applies to the WHOLE token. Quoting any segment of a concatenated token therefore suppresses tilde expansion for the entire token, even when the tilde itself is unquoted:

```bash
echo ~/"docs"     # Outputs: ~/docs      (WasQuoted suppresses the tilde)
                  # POSIX shells output: /home/user/docs
```

This whole-token granularity is a deliberate deviation from POSIX shells, which track quoting per character.

### Examples

Given `HOME=/home/testuser`:

| Input token | Output | Notes |
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

Quoting examples (the flag, not the content, decides):

```bash
echo ~            # Outputs: /home/testuser
echo "~"          # Outputs: ~          (WasQuoted: true)
echo '~'          # Outputs: ~          (WasSingleQuoted: true)
echo "~/docs"     # Outputs: ~/docs     (WasQuoted: true)
echo ~/"docs"     # Outputs: ~/docs     (whole-token granularity, see above)
```

**Note:** `\~` does NOT produce a literal tilde. The escape `\X` removes the backslash and leaves a plain `~` with no marker (escape table, [Escape Sequences](#escape-sequences)), and the resulting token is unquoted — so it still tilde-expands. To pass a literal `~` as the start of an argument, quote it.

### Reference Algorithm

The caller performs the `WasQuoted` check (see [Expansion Order](#expansion-order)). Tilde expansion itself inspects only the content:

```
EXPAND_TILDE(token) -> string
    if token is empty or its first rune is not ~ :
        return token

    home = the value of the HOME environment variable
    if home is empty:
        return token

    if token is exactly "~" :
        return home

    if the rune after the ~ is / :          # "~/..."
        return home followed by the rest of the token

    return token                            # "~user", "~something"
```

---

## Variable Expansion

Variable expansion replaces references to environment variables with their values, and replaces the special parameter `$?` with the last command-line status.

[include:_partials/variable-syntax.md](_partials/variable-syntax.md)

### Expansion Rules

1. **ASCII names**: Variable names match `[A-Za-z_][A-Za-z0-9_]*`. Name scanning is byte-wise ASCII; non-ASCII characters never extend a name
2. **Greedy matching**: For `$VAR` syntax, the longest valid variable name is matched
3. **Non-existent variables**: Expand to empty string (no error). The empty result remains an empty argument (see [Word Splitting](#word-splitting))
4. **Braced form**: `${NAME}` limits the name explicitly. The braced body is looked up in the environment verbatim and is not validated as a name; a body that is not a settable name (e.g. `${?}`, `${VAR:-default}`) normally resolves to empty
5. **Empty braces**: `${}` is left as literal `${}`
6. **Unclosed brace**: `${VAR` with no closing `}` is treated as literal text
7. **Dollar at end**: A lone `$` at end of the token is kept literal
8. **Dollar followed by a non-starter**: Kept as literal `$` (see the recognition rule above; `?` is the one special-parameter exception)
9. **Values are data**: The spliced value is never re-scanned — not by variable expansion (a value containing `$Y` stays literal) and not by the later command substitution step (a value containing `$(...)` or backticks stays literal). See [Spliced Values Are Protected](#spliced-values-are-protected)

### Examples

Given environment:
```
TEST_VAR=hello
TEST_VAR2=world
A=a
AB=ab
ABC=abc
```
and last command-line status `0`:

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
| `$$` | `$$` | Literal (not a special parameter here) |
| `$123` | `$123` | Dollar followed by digit |
| `$?` | `0` | Special parameter: last command-line status |
| `$?x` | `0x` | `$?` is exactly two characters |
| `${?}` | `` (empty) | Braced body is a verbatim environment lookup, NOT `$?` |
| `${}` | `${}` | Empty braces |
| `${TEST_VAR` | `${TEST_VAR` | Unclosed brace |
| `$ TEST_VAR` | `$ TEST_VAR` | Dollar followed by space |
| `$TEST_VAR$TEST_VAR2` | `helloworld` | Adjacent variables |
| `$ABC` | `abc` | Greedy matching (not `a` + `BC`) |
| `${A}BC` | `aBC` | Braces limit variable name |
| `$A1` | `` (empty) | Expands `A1`, not `A` + `1` |
| `${A}1` | `a1` | Braces limit to `A`, then literal `1` |

### Spliced Values Are Protected

Variable values are spliced in as **data**. To guarantee that the later command substitution step cannot execute text that came from a variable's value, variable expansion escape-marks every `$` and every backtick inside the spliced value (`$` becomes `\x01$`, `` ` `` becomes `` \x01` `` — the same marker the lexer uses, lexer.md §5.1). The markers are removed by the final, unconditional stripping step.

```bash
# Environment: X='$(echo pwned)'  Y='`date`'  Z='$HOME'
echo $X    # Outputs: $(echo pwned)   - NOT executed
echo $Y    # Outputs: `date`          - NOT executed
echo $Z    # Outputs: $HOME           - NOT re-expanded
```

(Environment values containing a literal U+0001 byte fall under the reserved-marker caveat of lexer.md §5.1.4: undefined behavior.)

### Reference Algorithm

The scan is byte-wise. `IS_NAME_START` accepts `_` and the ASCII letters. `IS_NAME_CHAR` accepts those plus the ASCII digits.

```
EXPAND_VARIABLES(token, lastStatus) -> string
    if token holds no $ :
        return token

    result = empty
    i = 0
    while i < LENGTH(token):

        if token[i] is \x01 and token[i+1] is $ :
            append "\x01$"        # still marked, stripped later
            i = i + 2
            continue

        if token[i] is not $ :
            append token[i]
            i = i + 1
            continue

        if no byte follows the $ :
            append "$"            # a lone trailing $ stays literal
            i = i + 1
            continue

        next = token[i+1]

        if next is ? :            # the special parameter
            append lastStatus as decimal text
            i = i + 2
            continue

        if next is { :            # the ${NAME} form
            if no } follows:
                append "$"        # unclosed brace: literal text
                i = i + 1
                continue
            name = the bytes between the { and the }
            if name is empty:
                append "${}"      # empty braces stay literal
                i = i + 3
                continue
            append MARK(LOOKUP(name))
            i = the index just past the }
            continue

        if not IS_NAME_START(next) :
            append "$"            # a $ before anything else is literal
            i = i + 1
            continue

        name = the longest run of IS_NAME_CHAR bytes after the $
        append MARK(LOOKUP(name))
        i = the index just past that run

    return result


MARK(value) -> string
    # Escape-mark every $ and ` in a spliced value, so the command
    # substitution step treats them as literal text. Values are data.
    replace each $ with \x01$
    replace each ` with \x01`
```

`LOOKUP` reads the environment. An unset name yields the empty string.

The name scan is byte-wise ASCII by design. Unicode classification applied to single bytes half-consumes a multi-byte UTF-8 sequence. A reference such as `$Aé` therefore scans the name `A` and leaves `é` as literal text.

---

## Command Substitution

Command substitution executes a command and replaces the substitution with the command's standard output.

[include:_partials/command-substitution-syntax.md](_partials/command-substitution-syntax.md)

### Single-Pass Expansion (No Re-Scan)

Command substitution is **single-pass over the original token**: the scanner walks the token left to right, splices each substitution's output into a result buffer, and NEVER re-scans that buffer. Substitution output containing `$(...)`, backticks, or `$VAR` is literal text.

This is a security property, not an optimization:

```bash
# payload.txt contains the text:  $(echo pwned)
echo $(cat payload.txt)
# Outputs: $(echo pwned)     - the payload is printed, NOT executed
```

A shell that re-scans substitution output executes data as code. Foundation Shell MUST NOT.

### Recursive Execution

The lexer preserves substitution bodies verbatim — embedded whitespace, quotes, escapes, operators, and nested substitutions all stay in the token (lexer.md §7.1). When a substitution is expanded, its body is handed to the executor, which re-parses it **recursively with the same parser and the same executor** (execution.md §Command Substitution Executor).

Consequences:

1. **Nesting works by recursion**: in `echo $(echo $(pwd))`, expanding the outer body `echo $(pwd)` triggers the inner substitution through the same pipeline. Syntactically inner substitutions therefore complete first. Backtick nesting follows the quote rule (quoting.md §6.3)
2. **Quoting inside the body is honored**: the recursive parse applies the full quoting rules to the body, so single quotes inside a body suppress expansion within it:

   ```bash
   echo $(echo '$(pwd)')     # Outputs: $(pwd)     - inner text is single-quoted
   echo $(echo "a  b")       # Outputs: a  b       - two spaces preserved
   ```
3. **Operators inside the body work**: `$(echo a | tr a b)` runs a pipeline inside the substitution

### Locating the Delimiters

The expander re-locates `$(...)` and backtick delimiters inside the token using the SAME body-quote-state rules the lexer used to keep them in one token (lexer.md §7.1 rule 4):

- A `)` closes the innermost open `$(` only when the body's single-quote, double-quote, and backtick depths are all zero
- A backtick opens, nests, or closes per the quote rule (quoting.md §5.2)
- A character preceded by a backslash (bodies preserve escapes verbatim) or by the escape marker `\x01` never opens or closes a substitution

The lexer has already validated the token, so the expander always finds the matching delimiter for an unmarked opener.

### Failure Semantics

| Failure | Behavior |
|---------|----------|
| Body fails to PARSE (e.g. `$(echo \|)`) | The whole command line fails as a parse error: `parse error: command substitution error: <err>`. Nothing on the line executes |
| Substituted command fails at RUNTIME (non-zero exit, command not found, redirection open failure) | The failure is reported on stderr exactly as it would be for a top-level command (execution.md §Error Handling); the substitution yields the captured stdout (possibly empty); the command line CONTINUES |
| Exit status of the substituted command | DISCARDED. It does not become `$?`, does not affect the surrounding command line's status, and never aborts the line |

```bash
echo before $(nosuchcmd) after
# stderr: nosuchcmd: command not found
# stdout: before  after          (substitution expanded to empty)
# Line status: 0 (from echo)
```

`exit` inside a substitution stops only the substitution's own command sequence; the shell survives (execution.md §exit).

### Output Handling

1. **Only stdout is captured**. The substituted command's stderr passes through to the shell's stderr
2. **Trailing newlines are trimmed** from the captured output — all of them, and only trailing ones. Interior newlines are preserved
3. The (trimmed) output replaces the substitution within the token. No word splitting occurs on the result ([Word Splitting](#word-splitting))

### Executor Interface

A substitution executor takes one command-substitution body as a string. It yields three results: the captured output, the body's exit status, and an error indication. The name is deliberate. "Subshell" is reserved for the future `()` grouping construct.

Command substitution occurs only when a parse is given an executor. A parse without one preserves substitutions as literal text (parser.md §Public API). The executor's execution model — same process, captured stdout, shared environment — is specified in execution.md §Command Substitution Executor.

### Reference Algorithm

The scan is rune-based. The output buffer is NEVER re-scanned.

```
EXPAND_COMMAND_SUBSTITUTION(token, executor) -> string
    out = empty
    i = 0
    while i < LENGTH(runes):

        if runes[i] is ESCAPE_MARKER and a rune follows:
            append both runes verbatim     # never an opener
            i = i + 2
            continue

        if runes[i] is $ and the next rune is ( :
            end  = FIND_MATCHING_PAREN(runes, i+2)
            body = the runes between them
            output = executor runs body    # its exit status is discarded
            append output with its trailing newlines removed
            i = end + 1
            continue

        if runes[i] is a backtick:
            end  = FIND_CLOSING_BACKTICK(runes, i+1)
            body = the runes between them
            output = executor runs body    # its exit status is discarded
            append output with its trailing newlines removed
            i = end + 1
            continue

        append runes[i]
        i = i + 1

    return out
```

A failure reported by the executor aborts the expansion and propagates.

`FIND_MATCHING_PAREN` and `FIND_CLOSING_BACKTICK` are [Locating the Delimiters](#locating-the-delimiters). Each tracks the body's own quote state (quoting.md §5.2). Each skips a backslash-escaped and a marker-escaped character, exactly as the scan did (lexer.md §10.2).

### Examples

| Input | Behavior |
|-------|----------|
| `echo $(pwd)` | Prints current working directory |
| `echo $(echo hello world)` | Prints `hello world` (one token, then one argument) |
| `` echo `date` `` | Prints current date |
| `$(echo ls)` | Executes `ls` (the expanded token becomes the command word) |
| `echo $(cat $(echo file))` | Nested: inner substitution names the file, outer reads it |
| `echo $(echo '$(pwd)')` | Prints `$(pwd)` — single quotes inside the body suppress |
| `echo '$(pwd)'` | Prints `$(pwd)` — single-quoted token, never expanded |
| `echo "$(pwd)"` | Prints the working directory — double quotes do not suppress |

---

## Quoting and Expansion

Quoting controls whether expansion occurs. Quote SEMANTICS are canonical in quoting.md (§2.4, §3.5, §10.4); this section defines only the expansion-side effect of the two token flags:

| Token state | Tilde | Variables / `$?` | Command substitution |
|-------------|-------|------------------|----------------------|
| Unquoted | YES | YES | YES |
| `WasQuoted` only (double quotes) | no | YES | YES |
| `WasSingleQuoted` (any single-quoted part) | no | no | no |

```bash
echo $HOME        # Outputs: /home/user   (expanded)
echo "$HOME"      # Outputs: /home/user   (expanded; quotes preserve spaces)
echo '$HOME'      # Outputs: $HOME        (literal)
echo "~"          # Outputs: ~            (tilde suppressed by any quoting)
echo "$(pwd)"     # Outputs: /current/dir (substitution works in double quotes)
echo '$(pwd)'     # Outputs: $(pwd)       (literal)
```

Both flags apply to the WHOLE token. Concatenations propagate conservatively (quoting.md §2.5): `echo 'a'$HOME` is one token with `WasSingleQuoted: true`, so `$HOME` is NOT expanded. Empty quoted strings produce empty tokens that survive as empty arguments (lexer.md §4.5).

---

## Escape Sequences

Escape sequences allow including special characters literally. Escape processing itself happens in the lexer (lexer.md §5); expansion honors the markers it leaves behind.

[include:_partials/escape-sequences.md](_partials/escape-sequences.md)

### Escaped Dollar Signs and Backticks

`\$` and `` \` `` are handled with the escape marker (`\x01`, lexer.md §5.1):

1. Lexer converts `\$` to `\x01$` (and `` \` `` to `` \x01` ``)
2. Variable expansion skips `\x01$` (keeps it marked); the substitution scanner skips any marker-escaped character
3. The final, unconditional stripping step removes every `\x01`, leaving the literal `$` / `` ` ``

The same marker protects spliced expansion results ([Spliced Values Are Protected](#spliced-values-are-protected)).

### Examples

| Input | Arguments | Notes |
|-------|--------|-------|
| `echo hello\ world` | `echo`, `hello world` | Space escaped, single token |
| `echo hello\\world` | `echo`, `hello\world` | Escaped backslash |
| `echo \$HOME` | `echo`, `$HOME` | Escaped dollar, no expansion |
| `` echo \`date\` `` | `echo`, `` `date` `` | Escaped backticks, no substitution |
| `echo \"hello\"` | `echo`, `"hello"` | Escaped quotes |
| `echo hello\nworld` | `echo`, `hello<newline>world` | Newline escape |
| `echo hello\tworld` | `echo`, `hello<tab>world` | Tab escape |

---

## Word Splitting

Word splitting happens ONCE, at tokenization time (lexer.md §3). Expansion never creates or removes token boundaries.

### Splitting Rules (Tokenization Phase)

1. Whitespace separates tokens; unquoted newlines separate commands (lexer.md §3)
2. Inside quotes or substitution bodies, whitespace is content
3. Consecutive whitespace is a single separator
4. Leading/trailing whitespace produces no tokens

### No Post-Expansion Word Splitting (Documented Divergence)

**Foundation Shell does NOT perform word splitting on expansion results.** If a variable's value (or a substitution's output) contains spaces, the expanded value remains ONE argument:

```bash
# Environment: X='a b'
echo $X       # Foundation Shell: 2 arguments  -> echo receives "a b"
              # POSIX (unquoted): 3 arguments  -> echo receives "a", "b"
```

Token boundaries are fixed before expansion runs. Unquoted `$X` and quoted `"$X"` therefore behave identically — as POSIX `"$X"` does.

### Empty Expansion Results Remain Empty Arguments (Documented Divergence)

An unquoted token that expands to the empty string stays in the argument list as an EMPTY argument. POSIX shells drop the empty field during word splitting; Foundation Shell has no post-expansion splitting, and the behavior deliberately matches empty quoted arguments (`""`, lexer.md §4.5):

```bash
echo a $UNSET_VAR b   # echo receives 3 arguments: "a", "", "b"
                      # POSIX: 2 arguments: "a", "b"
echo a "" b           # identical: 3 arguments — consistency is the point
```

---

## Special Parameters

Foundation Shell supports standard environment variables plus exactly ONE special parameter: `$?`.

### Supported: Environment Variables

All environment variables in the shell's environment are accessible (`$HOME`, `$PATH`, `$USER`, ...). Variables are set with `export` or standalone assignment (execution.md §Builtin Commands); all variables are environment variables — there is no local/exported distinction (execution.md).

### Supported: `$?` — Last Command-Line Status

`$?` expands to the decimal exit status of the **previous command line** (the previously executed chain — see execution.md §Last Exit Code for exactly what updates it).

- At shell startup the value is `0`
- After a command line completes, the value is that line's final status
- A line that fails to parse sets the value to `1` (execution.md §Error Handling)
- Command substitutions never change it (their status is discarded)

#### Per-LINE Semantics (Documented Deviation)

POSIX shells update `$?` after every command. Foundation Shell expands the WHOLE command line at parse time, before anything on the line runs, so every `$?` on a line sees the status from BEFORE the line:

```bash
false ; echo $?    # Prints the status from BEFORE this line (e.g. 0),
                   # NOT 1. POSIX shells print 1.
```

Across lines it behaves as expected — in interactive mode each line is a separate parse:

```
$ false
$ echo $?
1
```

**Whole-input consequence:** non-interactive input (piped stdin, script files, `fsh-exec`) is parsed as ONE input (execution.md §Non-Interactive Mode). For `$?` the entire input is a single "line": every `$?` in a script expands to the shell's status from before the script ran (normally `0`). `$?` is therefore only useful across interactive lines.

### Assignment Is Not Visible to the Same Input

Parse-time expansion applies to ORDINARY variables as well, and there the consequence is sharper. A variable assigned in an input still expands to its pre-input value everywhere in that input:

```bash
OUT=$(echo captured) ; echo $OUT     # $OUT expanded BEFORE the assignment ran
```

The assignment itself is real — it mutates the shell's environment, and a child that reads the environment sees the new value. Only expansion inside the same input is stale, which is what makes the shape deceptive:

```bash
X=hi ; printenv X          # prints hi — the child reads the environment
X=hi ; sh -c 'echo $X'     # prints hi — single quotes leave $X for the child
X=hi ; echo $X             # $X is stale: would print an empty line
```

Left unguarded, the last form yields an empty string and reports SUCCESS. Every other unsupported construct in this specification stops the caller; this one would corrupt a result instead. The parser therefore rejects an input that assigns a variable and then expands it (parser.md §Assignment Then Use Is Guarded).

To use a value, run the assignment and the use as SEPARATE inputs — separate interactive lines, or separate `fsh-exec` invocations. Within one input, hand the name to the child instead: `sh -c 'echo $X'`.

### NOT Supported: Other Special Parameters

Every other POSIX special parameter stays LITERAL — by the recognition rule, a `$` followed by anything but a letter, underscore, `{`, `(`, or `?` is a literal character:

| Input | Result | POSIX meaning (not implemented) |
|-------|--------|---------------------------------|
| `$$` | literal `$$` | Shell PID |
| `$!` | literal `$!` | Last background PID |
| `$0` | literal `$0` | Script name |
| `$1`, `$2`, ... | literal | Positional parameters |
| `$#`, `$@`, `$*`, `$-`, `$_` | literal | Various |

### NOT Supported: Parameter Expansion Operators

`${VAR:-default}`, `${VAR:=default}`, `${VAR:+value}`, `${VAR:?error}`, `${#VAR}`, `${VAR%pat}`, `${VAR#pat}`, `${VAR/pat/rep}` are NOT implemented. Under the braced-lookup rule ([Expansion Rules](#expansion-rules) rule 4) such a body is looked up verbatim in the environment and normally expands to EMPTY — it is neither an error nor left literal.

---

## Edge Cases and Error Handling

### Unclosed Quotes and Substitutions

Unclosed quotes and unclosed substitutions are TOKENIZATION errors — expansion never sees them (lexer.md §9, canonical strings in diagnostics.md §5.1):

```bash
echo 'hello     # error: unclosed single quote
echo "hello     # error: unclosed double quote
echo $(pwd      # error: unclosed command substitution $(...)
echo `date      # error: unclosed backtick
```

### Empty Arguments

Empty quoted strings ARE tokens and become empty arguments (lexer.md §4.5), and empty expansion results remain empty arguments ([Word Splitting](#word-splitting)):

```bash
echo ""         # Two tokens: echo + empty argument
echo $UNSET     # Two tokens: echo + empty argument
```

### Trailing Backslash

A backslash at the end of input (with nothing to escape) is kept literal (lexer.md §5.3):

```bash
echo hello\     # Tokens: echo, hello\
```

### Empty Variable Names

Empty braces `${}` are left literal:

```bash
echo ${}        # Outputs: ${}
```

### Non-Existent Variables

Non-existent variables silently expand to empty string:

```bash
echo $NONEXISTENT_VAR    # Outputs: (empty line; echo receives one empty argument)
echo prefix${MISSING}end # Outputs: prefixend
```

### Variable Expansion in Tilde Result

Tilde expansion runs first; its result then undergoes variable expansion:

```bash
# HOME=/home/user  MYVAR=value
echo ~/$MYVAR   # Outputs: /home/user/value
```

(The tilde pass splices `$HOME`'s value directly; only the `$MYVAR` reference existed in the token, so only it is expanded by the variable pass.)

---

## Complete Processing Example

Given:
```
HOME=/home/alice
NAME=Bob
```
and last command-line status `0`.

Input: `echo "Hello, $NAME" ~/docs '$(pwd)' \$HOME`

**Step 1: Tokenization** (lexer.md)
```
Token 1: {Content: "echo"}
Token 2: {Content: "Hello, $NAME", WasQuoted: true}
Token 3: {Content: "~/docs"}
Token 4: {Content: "$(pwd)", WasSingleQuoted: true, WasQuoted: true}
Token 5: {Content: "\x01$HOME"}                    (escape marker added)
```

**Step 2: Expansion of each token**

Token 1 (`echo`):
- No tilde, no `$`, no substitution
- Result: `echo`

Token 2 (`Hello, $NAME`, WasQuoted):
- Tilde: SKIPPED (WasQuoted) — and no leading `~` anyway
- Variable: `$NAME` -> `Bob` (spliced value marked, then stripped)
- Result: `Hello, Bob`

Token 3 (`~/docs`):
- Tilde: `~` -> `/home/alice`
- Variable: no change
- Result: `/home/alice/docs`

Token 4 (`$(pwd)`, WasSingleQuoted):
- Steps 1-3 SKIPPED entirely
- Marker strip: no markers present
- Result: `$(pwd)`

Token 5 (`\x01$HOME`):
- Tilde: no change
- Variable: `\x01$` stays marked, `HOME` is literal text after it
- Substitution: nothing to do
- Marker strip (unconditional): `\x01` removed
- Result: `$HOME`

**Final command**:
```
Args: ["echo", "Hello, Bob", "/home/alice/docs", "$(pwd)", "$HOME"]
```
