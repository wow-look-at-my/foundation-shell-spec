# Working with the Foundation Shell Specification

## Purpose

This is the authoritative specification for Foundation Shell. The spec should be:
- Minimal duplication
- Clear and obvious
- Unsurprising behavior
- Unambiguous

## Workflow for Spec Review/Iteration

### 1. Analyze First
Read the spec files and identify issues:
- Contradictions between files
- Duplicated content
- Ambiguous language
- Missing information
- Inconsistent formatting

### 2. Track Issues
Create/update `ISSUES.md` to track findings:
```markdown
## Issue N: Brief Title
**Status:** PENDING | RESOLVED
**Severity:** High | Medium | Low

Description of the issue.

**Resolution:** (once resolved)
```

### 3. One Question at a Time
Use `AskUserQuestion` to resolve each issue individually. Provide clear options:
- Always include a recommended option if one is clearly better
- Include an option for the user to specify their own approach
- Keep questions focused and actionable

### 4. One Commit per Issue
After resolving each issue:
1. Make the necessary edits
2. Commit with a clear message explaining what was fixed
3. Update ISSUES.md to mark as RESOLVED
4. Move to the next issue

### 5. AI/LLM Optimization
When making structural decisions, prefer approaches that help AI/LLMs navigate:
- **Canonical locations** - Define which file is authoritative for each topic
- **Cross-references** - Use "See X.md §N" instead of duplicating content
- **Focused files** - Smaller files fit better in context windows
- **Single source of truth** - Prevents inconsistencies that confuse AI readers

## File Structure

| File | Purpose |
|------|---------|
| `README.md` | Overview and canonical source mapping |
| `lexer.md` | Tokenization rules |
| `parser.md` | Parsing and AST |
| `expansion.md` | Variable/command substitution |
| `execution.md` | Command execution semantics |
| `operators.md` | Control flow operators |
| `quoting.md` | Quote handling and depth tracking |
| `redirection.md` | I/O redirection |
| `diagnostics.md` | Error message formatting |
| `highlighting.md` | Syntax highlighting |
| `ISSUES.md` | Issue tracker for spec review |