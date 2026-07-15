// Command generator renders the Foundation Shell specification sources
// (src/*.md) into publishable output (dist/):
//
//   - expands [include:PATH](PATH) directives (outside fenced code blocks)
//   - strips YAML frontmatter from the published pages
//   - emits llms.txt (llmstxt.org format) and README.md in reading order
//
// All sources are read and validated BEFORE anything is written, so a failed
// run never leaves partial output behind.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// includePattern matches [include:LABEL](TARGET) directives. The LABEL is the
// path that is resolved (relative to the including file's directory); LABEL
// and TARGET must be identical so that the GitHub-rendered source link and the
// generated output cannot diverge.
var includePattern = regexp.MustCompile(`\[include:([^\]]+)\]\(([^)]+)\)`)

// fenceOpenPattern matches the opening of a fenced code block: up to three
// spaces of indentation followed by at least three backticks or tildes.
var fenceOpenPattern = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

const (
	baseURL     = "https://wow-look-at-my.github.io/foundation-shell-spec/"
	siteTitle   = "Foundation Shell Specification"
	siteSummary = "The authoritative specification for Foundation Shell, a from-scratch shell with depth-tracked quote nesting, pipelines, redirection, command substitution, and built-in syntax highlighting."
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <source-dir> <output-dir>\n", os.Args[0])
		os.Exit(1)
	}

	srcDir := os.Args[1]
	outDir := os.Args[2]

	if err := run(srcDir, outDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// specFile is a fully read, validated, and include-expanded source file,
// ready to be written out.
type specFile struct {
	relPath string // path relative to srcDir (and inside outDir)
	meta    *fileMeta
	body    string // frontmatter stripped, includes expanded
}

func run(srcDir, outDir string) error {
	// Find all .md files
	var mdFiles []string
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip the generator directory, hidden files, and partial files (starting with _)
		if info.IsDir() && (info.Name() == "generator" || strings.HasPrefix(info.Name(), ".") || strings.HasPrefix(info.Name(), "_")) {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			mdFiles = append(mdFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking source directory: %w", err)
	}

	// Phase 1: read, validate, and expand everything in memory. No output is
	// written until every file has passed, so a failure cannot leave a
	// partially generated output directory behind.
	var files []*specFile
	var metas []*fileMeta
	for _, srcPath := range mdFiles {
		relPath, err := filepath.Rel(srcDir, srcPath)
		if err != nil {
			return fmt.Errorf("getting relative path for %s: %w", srcPath, err)
		}

		content, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", srcPath, err)
		}

		fmLines, body, err := splitFrontmatter(string(content))
		if err != nil {
			return fmt.Errorf("%s: %w", relPath, err)
		}

		meta, err := parseFrontmatter(filepath.Base(srcPath), fmLines)
		if err != nil {
			return err
		}

		expanded, err := processIncludes(filepath.Dir(srcPath), body, []string{srcPath})
		if err != nil {
			return fmt.Errorf("processing %s: %w", relPath, err)
		}

		files = append(files, &specFile{relPath: relPath, meta: meta, body: expanded})
		metas = append(metas, meta)
	}

	// Sort by recommend_after
	sortedMetas, err := topoSort(metas)
	if err != nil {
		return err
	}

	// Phase 2: everything validated — write the output.
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	for _, f := range files {
		outPath := filepath.Join(outDir, f.relPath)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return fmt.Errorf("creating output directory for %s: %w", f.relPath, err)
		}
		if err := os.WriteFile(outPath, []byte(f.body), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}
		fmt.Printf("Processed: %s\n", f.relPath)
	}

	// Generate llms.txt
	if err := generateLLMsTxt(outDir, sortedMetas); err != nil {
		return fmt.Errorf("generating llms.txt: %w", err)
	}

	// Generate README.md from template if it exists
	readmeGenerated := false
	readmeTmpl := filepath.Join(srcDir, "README.md.tmpl")
	if _, err := os.Stat(readmeTmpl); err == nil {
		if err := generateREADME(srcDir, outDir, sortedMetas); err != nil {
			return fmt.Errorf("generating README.md: %w", err)
		}
		readmeGenerated = true
	}

	summary := fmt.Sprintf("\nGenerated %d files + llms.txt", len(sortedMetas))
	if readmeGenerated {
		summary += " + README.md"
	}
	fmt.Println(summary)
	return nil
}

// splitFrontmatter separates the leading YAML frontmatter block from the body.
// The file must start with a `---` line and contain a closing `---` line;
// the returned body has the frontmatter (and its delimiters) removed.
func splitFrontmatter(content string) (fmLines []string, body string, err error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return nil, "", fmt.Errorf("missing frontmatter (must start with ---)")
	}
	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			closing = i
			break
		}
	}
	if closing == -1 {
		return nil, "", fmt.Errorf("unterminated frontmatter (missing closing ---)")
	}
	body = strings.Join(lines[closing+1:], "\n")
	return lines[1:closing], strings.TrimLeft(body, "\n"), nil
}

// processIncludes expands [include:PATH](PATH) directives in content. Include
// directives inside fenced code blocks (``` or ~~~) are left verbatim so the
// spec can document the include syntax itself. stack carries the chain of
// files currently being expanded, for cycle detection; its first element is
// the file content came from.
func processIncludes(currentDir, content string, stack []string) (string, error) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))

	inFence := false
	var fenceChar byte
	var fenceLen int

	for _, line := range lines {
		if inFence {
			out = append(out, line)
			if isFenceClose(line, fenceChar, fenceLen) {
				inFence = false
			}
			continue
		}
		if m := fenceOpenPattern.FindStringSubmatch(line); m != nil {
			inFence = true
			fenceChar = m[1][0]
			fenceLen = len(m[1])
			out = append(out, line)
			continue
		}

		expanded, err := expandIncludesInLine(currentDir, line, stack)
		if err != nil {
			return "", err
		}
		out = append(out, expanded)
	}

	return strings.Join(out, "\n"), nil
}

// isFenceClose reports whether line closes a fence opened by fenceLen
// repetitions of fenceChar: up to three spaces of indentation, at least
// fenceLen fence characters, and nothing else but whitespace.
func isFenceClose(line string, fenceChar byte, fenceLen int) bool {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 {
		return false
	}
	n := 0
	for n < len(s) && s[n] == fenceChar {
		n++
	}
	if n < fenceLen {
		return false
	}
	return strings.TrimSpace(s[n:]) == ""
}

// expandIncludesInLine replaces every include directive in a single line with
// the (recursively expanded) content of the referenced file. A missing file
// or an include cycle is a fatal error; a label/target mismatch is a warning
// (the label path is authoritative).
func expandIncludesInLine(currentDir, line string, stack []string) (string, error) {
	var expandErr error
	result := includePattern.ReplaceAllStringFunc(line, func(match string) string {
		if expandErr != nil {
			return match
		}
		sub := includePattern.FindStringSubmatch(match)
		label, target := sub[1], sub[2]
		if label != target {
			fmt.Fprintf(os.Stderr, "Warning: %s: include label %q does not match link target %q (they must match; the label path is used)\n",
				stack[len(stack)-1], label, target)
		}

		fullPath := filepath.Clean(filepath.Join(currentDir, label))

		for i, s := range stack {
			if s == fullPath {
				expandErr = fmt.Errorf("include cycle: %s", strings.Join(append(stack[i:], fullPath), " -> "))
				return match
			}
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			expandErr = fmt.Errorf("include %s (from %s): %w", label, stack[len(stack)-1], err)
			return match
		}

		processed, err := processIncludes(filepath.Dir(fullPath), string(data), append(stack, fullPath))
		if err != nil {
			expandErr = err
			return match
		}
		return processed
	})
	return result, expandErr
}

// fileMeta holds frontmatter metadata for a file
type fileMeta struct {
	file           string
	title          string
	description    string
	recommendAfter string // filename this should come after
}

// parseFrontmatter extracts title, description, and recommend_after from the
// frontmatter lines of a file and validates the required fields.
func parseFrontmatter(filename string, fmLines []string) (*fileMeta, error) {
	meta := &fileMeta{file: filename}

	for _, line := range fmLines {
		if strings.HasPrefix(line, "title:") {
			meta.title = strings.TrimSpace(strings.TrimPrefix(line, "title:"))
		}
		if strings.HasPrefix(line, "description:") {
			meta.description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
		if strings.HasPrefix(line, "recommend_after:") {
			meta.recommendAfter = strings.TrimSpace(strings.TrimPrefix(line, "recommend_after:"))
		}
	}

	// Validate required fields
	if meta.title == "" {
		return nil, fmt.Errorf("%s: missing required 'title' in frontmatter", filename)
	}
	if meta.description == "" {
		return nil, fmt.Errorf("%s: missing required 'description' in frontmatter", filename)
	}
	if len(meta.description) < 20 {
		return nil, fmt.Errorf("%s: description must be at least 20 characters (got %d)", filename, len(meta.description))
	}
	if len(meta.description) > 250 {
		return nil, fmt.Errorf("%s: description must be at most 250 characters (got %d)", filename, len(meta.description))
	}

	return meta, nil
}

// topoSort sorts files based on recommend_after dependencies
func topoSort(metas []*fileMeta) ([]*fileMeta, error) {
	// Build adjacency: file -> what comes after it
	afterMap := make(map[string][]string)
	metaMap := make(map[string]*fileMeta)
	inDegree := make(map[string]int)

	for _, m := range metas {
		metaMap[m.file] = m
		inDegree[m.file] = 0
	}

	for _, m := range metas {
		if m.recommendAfter != "" {
			if _, exists := metaMap[m.recommendAfter]; exists {
				afterMap[m.recommendAfter] = append(afterMap[m.recommendAfter], m.file)
				inDegree[m.file]++
			} else {
				fmt.Fprintf(os.Stderr, "Warning: %s: recommend_after %q does not match any generated file (edge ignored)\n",
					m.file, m.recommendAfter)
			}
		}
	}

	// Kahn's algorithm
	var queue []string
	for _, m := range metas {
		if inDegree[m.file] == 0 {
			queue = append(queue, m.file)
		}
	}
	// Sort queue alphabetically for deterministic order among peers
	sort.Strings(queue)

	var result []*fileMeta
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		result = append(result, metaMap[curr])

		var next []string
		for _, after := range afterMap[curr] {
			inDegree[after]--
			if inDegree[after] == 0 {
				next = append(next, after)
			}
		}
		sort.Strings(next)
		queue = append(queue, next...)
	}

	if len(result) != len(metas) {
		return nil, fmt.Errorf("cycle detected in recommend_after dependencies")
	}

	return result, nil
}

// generateLLMsTxt writes llms.txt in the llmstxt.org format: an H1 title, a
// one-line blockquote summary, and a Docs section linking every page (with
// absolute URLs) in recommended reading order.
func generateLLMsTxt(outDir string, metas []*fileMeta) error {
	var builder strings.Builder

	builder.WriteString("# " + siteTitle + "\n\n")
	builder.WriteString("> " + siteSummary + "\n\n")
	builder.WriteString("## Docs\n\n")

	for _, meta := range metas {
		builder.WriteString(fmt.Sprintf("- [%s](%s%s): %s\n", meta.title, baseURL, meta.file, meta.description))
	}

	llmsPath := filepath.Join(outDir, "llms.txt")
	return os.WriteFile(llmsPath, []byte(builder.String()), 0644)
}

func generateREADME(srcDir, outDir string, metas []*fileMeta) error {
	tmplPath := filepath.Join(srcDir, "README.md.tmpl")
	content, err := os.ReadFile(tmplPath)
	if err != nil {
		return err
	}

	// Build reading order list
	var readingOrder strings.Builder
	for i, meta := range metas {
		readingOrder.WriteString(fmt.Sprintf("%d. [%s](%s)\n", i+1, meta.title, meta.file))
	}

	// Build file table
	var fileTable strings.Builder
	for _, meta := range metas {
		fileTable.WriteString(fmt.Sprintf("| [%s](%s) | %s |\n", meta.file, meta.file, meta.description))
	}

	// Replace placeholders
	result := string(content)
	result = strings.ReplaceAll(result, "{{READING_ORDER}}", strings.TrimSpace(readingOrder.String()))
	result = strings.ReplaceAll(result, "{{FILE_TABLE}}", strings.TrimSpace(fileTable.String()))

	readmePath := filepath.Join(outDir, "README.md")
	return os.WriteFile(readmePath, []byte(result), 0644)
}
