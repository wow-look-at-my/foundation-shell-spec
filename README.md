# Foundation Shell Specification

Foundation Shell is a shell built from scratch against a rigorous, exhaustive specification. **This repository is that specification — the authoritative source of truth for all shell behavior.** The implementation (Go) lives in [wow-look-at-my/foundation-shell](https://github.com/wow-look-at-my/foundation-shell); where implementation and spec disagree, the spec wins.

## Flagship Feature: Depth-Tracked Quote Nesting

Unlike every sh-family shell, where a same-type quote character always *closes* the open quote, Foundation Shell lets quote characters **nest**: `echo 'outer 'inner' end'` is one argument with a nested quoted region, tracked by per-type depth counters. It is a deliberate non-POSIX design — some valid POSIX inputs are errors here, and vice versa. The precise open/nest/close rule, the depth model, and the POSIX divergence table live in [src/quoting.md](src/quoting.md).

## Published Documentation

The rendered spec is published at **<https://wow-look-at-my.github.io/foundation-shell-spec/>** — the entry point is [`llms.txt`](https://wow-look-at-my.github.io/foundation-shell-spec/llms.txt) (llmstxt.org format), which lists every page with its description in recommended reading order.

## Canonical Authority Map

Every topic has exactly one owning file. When files overlap, the owner listed here is normative and the others defer to it.

| Topic | Canonical file |
|-------|----------------|
| Tokenization, word/token vocabulary, word boundaries, escapes | [src/lexer.md](src/lexer.md) |
| Parsing, token classification, Chain/CommandSpec structures, parse-time validation | [src/parser.md](src/parser.md) |
| Quote semantics, the nesting rule and depth tracking, quote isolation | [src/quoting.md](src/quoting.md) |
| Expansion pipeline: tilde, variables, `$?`, command substitution | [src/expansion.md](src/expansion.md) |
| Chain operators `\|`, `&&`, `\|\|`, `;` — semantics and precedence | [src/operators.md](src/operators.md) |
| I/O redirection operators and file handling | [src/redirection.md](src/redirection.md) |
| Execution: run modes, exit codes, builtins, signals | [src/execution.md](src/execution.md) |
| Syntax highlighting, the analyzer, semantic token types, themes | [src/highlighting.md](src/highlighting.md) |
| Diagnostic output formatting and canonical error strings | [src/diagnostics.md](src/diagnostics.md) |

## Recommended Reading Order

1. [src/lexer.md](src/lexer.md)
2. [src/parser.md](src/parser.md)
3. [src/quoting.md](src/quoting.md)
4. [src/expansion.md](src/expansion.md)
5. [src/operators.md](src/operators.md)
6. [src/redirection.md](src/redirection.md)
7. [src/execution.md](src/execution.md)
8. [src/highlighting.md](src/highlighting.md)
9. [src/diagnostics.md](src/diagnostics.md)

The order is encoded in each file's `recommend_after` frontmatter field; the generator derives the published reading order from it.

## Architecture Note: Two Scanners, One Rule

The spec deliberately defines **two scanners** over the same input language:

- The **lexer** ([src/lexer.md](src/lexer.md)) is the execution tokenizer: it removes outermost quote delimiters, processes escapes, and emits the `TokenContext` values the parser and expander consume. It stops at the first error, because execution cannot proceed past one.
- The **analyzer** ([src/highlighting.md](src/highlighting.md)) is the error-tolerant scanner behind syntax highlighting and diagnostics: it preserves quote characters, records positions and depths for every token, and keeps scanning after errors so the REPL can highlight and caret-annotate incomplete input as you type.

Both exist because their jobs pull in opposite directions — execution wants clean expanded tokens and fail-fast errors; highlighting wants raw text spans and maximal error recovery. They MUST implement the same rules: quoting.md §1.3 ("Two Scanners, One Rule") binds both to the same quote-state semantics, and highlighting.md §7.1 states the consistency promise — an input is valid to one scanner iff it is valid to the other. The implementation enforces this with a lexer/analyzer validity cross-check in its conformance suite (for every corpus input, `Tokenize` errors iff `Analyze().Valid` is false).

## Repository Layout

| Path | Purpose |
|------|---------|
| [src/](src/) | The nine specification source files (`*.md`, with YAML frontmatter) |
| [src/_partials/](src/_partials/) | Shared content included by multiple spec files via `[include:REL/PATH](REL/PATH)` directives |
| [src/README.md.tmpl](src/README.md.tmpl) | Template for the generated `dist/README.md` (`{{READING_ORDER}}`, `{{FILE_TABLE}}`) |
| [generator/](generator/) | Go tool that expands includes, strips frontmatter, and emits `dist/`, `llms.txt`, and the generated README |
| [justfile](justfile) | `just generate` (build docs into `dist/`), `just clean`, `just dev` |
| [index.html](index.html) | Pages root redirect to `llms.txt` |
| [.github/workflows/](.github/workflows/) | `validate.yml` (build + GitHub Pages deploy from master), Claude review workflows |

Executable conformance tests (BATS) live in the implementation repository at `tests/`, where CI runs them against the built shell. The implementation consumes this repository as a pinned git submodule at `spec/` — the pin records exactly which spec commit the implementation targets.

## Building

```bash
just generate    # renders src/ into dist/ (llms.txt, README.md, spec pages)
```

Spec issues and open questions are tracked as [GitHub issues](https://github.com/wow-look-at-my/foundation-shell-spec/issues).
