### Variable Reference Syntax

| Syntax | Example | Description |
|--------|---------|-------------|
| `$VAR` | `$HOME`, `$PATH` | Simple variable reference |
| `${VAR}` | `${HOME}`, `${USER}` | Braced variable reference |

### Recognition Rule

A `$` begins a variable reference ONLY when the next character is a letter (a-z, A-Z), an underscore (`_`), or `{`. A `$` followed by `(` begins command substitution. A `$` followed by any other character (digit, punctuation, whitespace, or end of input) is a literal character within the word.

### Variable Name Rules

- **First character**: Letter (a-z, A-Z) or underscore (_)
- **Subsequent characters**: Letters, digits (0-9), or underscore
- Names are case-sensitive: `$var` and `$VAR` are different

### Expansion Behavior

| Input | Output | Notes |
|-------|--------|-------|
| `$VAR` | Value of VAR | Simple expansion |
| `${VAR}` | Value of VAR | Braced expansion |
| `$NONEXISTENT` | (empty) | Non-existent variables expand to empty |
| `${}` | `${}` | Empty braces kept literal |
| `$` at end | `$` | Lone dollar kept literal |
| `$$`, `$!`, `$?`, `$1` | Literal | Next character cannot start a variable name |
