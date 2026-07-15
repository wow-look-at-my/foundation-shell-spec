### Command Substitution Syntax

Foundation Shell supports two syntaxes for command substitution:

| Syntax | Example | Description |
|--------|---------|-------------|
| `$(...)` | `$(date)`, `$(echo hello)` | Modern syntax (preferred, nestable) |
| `` `...` `` | `` `date` ``, `` `echo hello` `` | Legacy backtick syntax (cannot nest) |

### Rules

1. **Execution**: The substitution body is re-parsed and executed as a command when the substitution is expanded
2. **Output substitution**: Command stdout replaces the substitution
3. **Trailing newline trimming**: Trailing newlines are removed from the captured output
4. **Nesting**: `$(...)` nests naturally; innermost substitutions are processed first. Backticks cannot nest: an unescaped backtick always closes an open backtick. Escape the inner backticks (`` \` ``) to nest
5. **Single quotes suppress**: Command substitution inside single quotes is NOT executed
6. **Body preserved verbatim**: Between the opening and closing delimiters, whitespace, quote characters, and escape sequences are preserved exactly as written and take effect only when the body is re-parsed. A quoted `)` does not close a `$(...)` substitution
7. **Recognition**: `$` followed by `(` opens a command substitution; `$` followed by a letter, underscore, or `{` is a variable reference; any other `$` is a literal character

### Example

```bash
echo $(date)           # Modern syntax - executes date command
echo `date`            # Legacy syntax - same behavior
echo '$(date)'         # Literal text - NOT executed (single quotes)
echo $(echo $(pwd))    # Nested - inner $(pwd) executes first
echo $(echo "a  b")    # Quotes kept in the body - prints: a  b (two spaces)
echo $(echo ")")       # Quoted ) does not close the substitution - prints: )
```
