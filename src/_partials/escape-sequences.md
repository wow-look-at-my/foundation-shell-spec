### Escape Sequence Table

| Sequence | Result | Description |
|----------|--------|-------------|
| `\\` | `\` | Literal backslash |
| `\ ` (backslash-space) | ` ` | Literal space (prevents word splitting) |
| `\$` | `$` (marked) | Escaped dollar (marked to prevent expansion) |
| `\"` | `"` | Literal double quote |
| `\'` | `'` | Literal single quote |
| `` \` `` | `` ` `` (marked) | Escaped backtick (marked to prevent command substitution) |
| `\n` | newline (0x0A) | Newline character |
| `\t` | tab (0x09) | Tab character |
| `\X` (any other) | `X` | The character itself (backslash removed) |

### Escape Processing Context

| Context | Escape Processing |
|---------|-------------------|
| Outside all quotes | YES - all escapes processed |
| Inside double quotes | YES - all escapes processed |
| Inside single quotes | NO - backslash is literal |
