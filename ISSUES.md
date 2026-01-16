# Specification Issues Tracker

Tracking issues identified during spec review.

## Issue 1: Trailing Semicolon Contradiction
**Status:** RESOLVED
**Severity:** High (direct contradiction)

operators.md said trailing `;` is invalid, but highlighting.md and diagnostics.md said it's valid.

**Resolution:** Updated operators.md to say trailing `;` is valid (matching bash behavior).

## Issue 2: Content Duplication
**Status:** RESOLVED
**Severity:** Medium

Multiple topics duplicated across files (quotes, escapes, operators, redirections, command substitution, CommandSpec).

**Resolution:** Created `_partials/` directory with shared content files. Updated lexer.md, quoting.md, and expansion.md to use includes for escape sequences table. Generator updated to skip `_partials/` directory during normal processing but expand includes in output.

## Issue 3: Depth-Tracked Quote Nesting
**Status:** PENDING
**Severity:** Medium

Non-POSIX behavior not prominently warned about.

## Issue 4: Missing Builtins Specification
**Status:** PENDING
**Severity:** Medium

No spec for builtin commands referenced in execution.md.

## Issue 5: Analyzer vs Lexer Relationship
**Status:** PENDING
**Severity:** Low

Two tokenization systems not clearly distinguished.

## Issue 6: Go Code in Spec
**Status:** RESOLVED
**Severity:** Low

Specs contain Go-specific implementation code.

**Resolution:** No change needed. Go code serves as reference implementation examples and doesn't need to be converted to pseudocode.

## Issue 7: Missing Reading Order
**Status:** RESOLVED
**Severity:** Low

No guidance on spec reading order or dependencies.

**Resolution:** README.md is now generated from a template with recommended reading order derived from frontmatter. Reading order appears in both llms.txt and README.md.

## Issue 8: Inconsistent Version History
**Status:** RESOLVED
**Severity:** Low

Some specs have version history, others don't.

**Resolution:** Removed version history sections. Git tracks history; version sections in docs add maintenance burden.
