# Working with the Foundation Shell Specification

## Purpose

This is the authoritative specification for Foundation Shell (the implementation lives in `wow-look-at-my/foundation-shell`). The spec must be:
- Minimal duplication
- Clear and obvious
- Unsurprising behavior
- Unambiguous
- **Standalone**: a spec page in `src/` describes required behavior only. It never names the implementation repo, a source path, a test file, a programming language, or that language's standard library. Data shapes go in field tables. Algorithms go in language-neutral pseudocode in an untagged code fence.

The **canonical authority map** — which file owns which topic — lives in [README.md](README.md). When two files overlap, edit the owning file and cross-reference it from the other.

## File Structure

| Path | Purpose |
|------|---------|
| `README.md` | Repo overview, canonical authority map, reading order, architecture notes |
| `src/*.md` | The nine spec source files (lexer, parser, quoting, expansion, operators, redirection, execution, highlighting, diagnostics) |
| `src/_partials/*.md` | Shared content included by multiple spec files (no frontmatter) |
| `src/README.md.tmpl` | Template for the generated `dist/README.md` (`{{READING_ORDER}}`, `{{FILE_TABLE}}` placeholders) |
| `generator/` | Go tool that renders `src/` into `dist/` |
| `justfile` | Build recipes (see below) |
| `index.html` | Pages root redirect to `llms.txt` |
| `.github/workflows/` | `validate.yml` builds every push and deploys Pages from master; Claude review workflows |
| `dist/` | Generated output (gitignored — never edit or commit it) |

The executable conformance suite ([dats](https://github.com/wow-look-at-my/dats)) lives in the **implementation repo** at `src/dats/`, where every build runs it against the built shell. Do not add a copy here. A second copy drifts.

## Source File Conventions

### Frontmatter

Every `src/*.md` file (not partials) starts with YAML frontmatter:

```yaml
---
title: Lexer Specification
description: Tokenization rules, operator recognition, and quote state tracking.
recommend_after: parser.md
---
```

- `title` — required. It is used in llms.txt and the generated README.
- `description` — required, 20–250 characters. It is used in llms.txt and the file table.
- `recommend_after` — optional. It names the file that precedes this one in reading order. The files form a **single linear chain**: lexer → parser → quoting → expansion → operators → redirection → execution → highlighting → diagnostics. Splice a new file into the chain. Point it at its predecessor, and repoint the old successor at it.

### Includes

Shared content lives in `src/_partials/` and is pulled in with:

```markdown
[include:_partials/escape-sequences.md](_partials/escape-sequences.md)
```

- The syntax is `[include:REL/PATH](REL/PATH)` — the label path (after `include:`) and the link target in parentheses MUST be identical (the generator warns on mismatch and resolves the label). The path is relative to the including file's directory. The duplicated path keeps the directive a working link when reading the raw source on GitHub.
- Includes expand recursively. A partial can include another partial.

### Generator behavior (what `just generate` does)

- Expands includes into `dist/*.md`. A **missing include file is a fatal error**. The build fails, and nothing is written. Include **cycles are detected** and reported. All sources are validated before any output is written.
- Include directives inside fenced code blocks (``` or ~~~) are left verbatim — that is how the spec can document the include syntax itself.
- **Frontmatter is stripped** from the published pages. It only feeds llms.txt, the README, and the ordering.
- Emits `llms.txt` in llmstxt.org format (H1 title, blockquote summary, `## Docs` list of absolute-URL links in reading order) and renders `dist/README.md` from `src/README.md.tmpl`.

## Build

```bash
just generate   # render src/ into dist/ (spec pages, llms.txt, README.md)
just clean      # remove dist/
just dev        # generate + list the output
```

Always run `just generate` after editing `src/`, and confirm it exits cleanly. CI (validate.yml) runs the same generation on every push. It deploys `dist/` to GitHub Pages from master.

## Workflow for Spec Review/Iteration

### 1. Analyze First
Read the spec files and identify issues:
- Contradictions between files
- Duplicated content
- Ambiguous language
- Missing information
- Inconsistent formatting

### 2. Track Issues on GitHub
Findings are tracked as **GitHub issues** on this repository (there is no ISSUES.md file). Before filing, search existing issues to avoid duplicates. Include severity (High/Medium/Low), the affected files with line references, and a suggested fix.

### 3. One Question at a Time (interactive sessions)
Use `AskUserQuestion` to resolve each issue individually. Provide clear options:
- Always include a recommended option if one is clearly better
- Include an option for the user to specify their own approach
- Keep questions focused and actionable

### 4. One Commit per Issue
After resolving each issue:
1. Make the necessary edits
2. Run `just generate` to confirm the build is green
3. Commit with a clear message explaining what was fixed. Reference the issue as `Fixes #N`, which closes it on merge
4. Move to the next issue

### 5. AI/LLM Optimization
When making structural decisions, prefer approaches that help AI/LLMs navigate:
- **Canonical locations** - Define which file is authoritative for each topic (the map lives in README.md)
- **Cross-references** - Use "See X.md §N" instead of duplicating content
- **Focused files** - Smaller files fit better in context windows
- **Single source of truth** - Prevents inconsistencies that confuse AI readers
