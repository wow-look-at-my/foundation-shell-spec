### Variable Reference Syntax

| Syntax | Example | Description |
|--------|---------|-------------|
| `$VAR` | `$HOME`, `$PATH` | Simple variable reference |
| `${VAR}` | `${HOME}`, `${USER}` | Braced variable reference |

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
