### Command Substitution Syntax

Foundation Shell supports two syntaxes for command substitution:

| Syntax | Example | Description |
|--------|---------|-------------|
| `$(...)` | `$(date)`, `$(echo hello)` | Modern syntax (preferred, nestable) |
| `` `...` `` | `` `date` ``, `` `echo hello` `` | Legacy backtick syntax |

### Rules

1. **Execution**: The substitution body is re-parsed recursively and executed as a command when the substitution is expanded
2. **Output substitution**: Command stdout replaces the substitution
3. **No re-scan of output**: The substituted output is never re-scanned — `$(...)`, backticks, or `$VAR` appearing in a command's OUTPUT are literal text, never executed or expanded
4. **Exit status discarded**: The substituted command's exit status is discarded — it does not become `$?` and does not affect the surrounding command line
5. **Trailing newline trimming**: Trailing newlines are removed from the captured output
6. **Nested substitution**: Nesting works through the recursive re-parse — syntactically inner substitutions complete first; `$(...)` nests naturally (see quoting.md for backtick nesting)
7. **Single quotes suppress**: Command substitution inside single quotes is NOT executed
8. **Body preserved verbatim**: Between the opening and closing delimiters, whitespace, quote characters, and escape sequences are preserved exactly as written and take effect only when the body is re-parsed. A quoted `)` does not close a `$(...)` substitution
9. **Recognition**: `$` followed by `(` opens a command substitution; `$` followed by a letter, underscore, `{`, or `?` is a variable/parameter reference; any other `$` is a literal character

### Example

```bash
echo $(date)           # Modern syntax - executes date command
echo `date`            # Legacy syntax - same behavior
echo '$(date)'         # Literal text - NOT executed (single quotes)
echo $(echo $(pwd))    # Nested - inner $(pwd) executes first
echo $(echo "a  b")    # Quotes kept in the body - prints: a  b (two spaces)
echo $(echo ")")       # Quoted ) does not close the substitution - prints: )
```
