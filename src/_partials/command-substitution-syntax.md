### Command Substitution Syntax

Foundation Shell supports two syntaxes for command substitution:

| Syntax | Example | Description |
|--------|---------|-------------|
| `$(...)` | `$(date)`, `$(echo hello)` | Modern syntax (preferred, nestable) |
| `` `...` `` | `` `date` ``, `` `echo hello` `` | Legacy backtick syntax |

### Rules

1. **Execution**: Content is executed as a command
2. **Output substitution**: Command stdout replaces the substitution
3. **Trailing newline trimming**: Trailing newlines are removed from output
4. **Nested substitution**: Innermost substitutions are processed first
5. **Single quotes suppress**: Command substitution inside single quotes is NOT executed

### Example

```bash
echo $(date)           # Modern syntax - executes date command
echo `date`            # Legacy syntax - same behavior
echo '$(date)'         # Literal text - NOT executed (single quotes)
echo $(echo $(pwd))    # Nested - inner $(pwd) executes first
```
