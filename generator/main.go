package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// includePattern matches [include:path](path) style links
var includePattern = regexp.MustCompile(`\[include:([^\]]+)\]\([^)]+\)`)

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

func run(srcDir, outDir string) error {
	// Create output directory
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

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

	// Parse frontmatter and process each file
	var metas []*fileMeta
	for _, srcPath := range mdFiles {
		relPath, err := filepath.Rel(srcDir, srcPath)
		if err != nil {
			return fmt.Errorf("getting relative path for %s: %w", srcPath, err)
		}

		outPath := filepath.Join(outDir, relPath)

		if err := processFile(srcDir, srcPath, outPath); err != nil {
			return fmt.Errorf("processing %s: %w", srcPath, err)
		}

		meta, err := parseFrontmatter(srcPath)
		if err != nil {
			return fmt.Errorf("parsing frontmatter for %s: %w", srcPath, err)
		}
		metas = append(metas, meta)
		fmt.Printf("Processed: %s\n", relPath)
	}

	// Sort by recommend_after
	sortedMetas, err := topoSort(metas)
	if err != nil {
		return err
	}

	// Generate llms.txt
	if err := generateLLMsTxt(outDir, sortedMetas); err != nil {
		return fmt.Errorf("generating llms.txt: %w", err)
	}

	// Generate README.md from template if it exists
	readmeTmpl := filepath.Join(srcDir, "README.md.tmpl")
	if _, err := os.Stat(readmeTmpl); err == nil {
		if err := generateREADME(srcDir, outDir, sortedMetas); err != nil {
			return fmt.Errorf("generating README.md: %w", err)
		}
	}

	fmt.Printf("\nGenerated %d files + llms.txt + README.md\n", len(sortedMetas))
	return nil
}

func processFile(srcDir, srcPath, outPath string) error {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	// Process includes
	processed := processIncludes(srcDir, filepath.Dir(srcPath), string(content))

	// Create output directory if needed
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	if err := os.WriteFile(outPath, []byte(processed), 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

func processIncludes(srcDir, currentDir, content string) string {
	return includePattern.ReplaceAllStringFunc(content, func(match string) string {
		// Extract the path from the match
		submatches := includePattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		includePath := submatches[1]

		// Resolve the include path relative to current file's directory
		fullPath := filepath.Join(currentDir, includePath)

		// Read the included file
		includeContent, err := os.ReadFile(fullPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not include %s: %v\n", includePath, err)
			return match // Keep original if include fails
		}

		// Recursively process includes in the included content
		processed := processIncludes(srcDir, filepath.Dir(fullPath), string(includeContent))

		return processed
	})
}

// fileMeta holds frontmatter metadata for a file
type fileMeta struct {
	file           string
	title          string
	description    string
	recommendAfter string // filename this should come after
}

// parseFrontmatter extracts title, description, and recommend_after from file
func parseFrontmatter(path string) (*fileMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	filename := filepath.Base(path)
	meta := &fileMeta{file: filename}
	scanner := bufio.NewScanner(f)

	// Look for YAML frontmatter
	if !scanner.Scan() {
		return nil, fmt.Errorf("%s: missing frontmatter", filename)
	}
	firstLine := scanner.Text()
	if firstLine != "---" {
		return nil, fmt.Errorf("%s: missing frontmatter (must start with ---)", filename)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			break
		}
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

const baseURL = "https://wow-look-at-my-code.github.io/foundation-shell-spec/"

func generateLLMsTxt(outDir string, metas []*fileMeta) error {
	var builder strings.Builder

	builder.WriteString("Foundation Shell Specification\n\n")
	builder.WriteString("Base URL: " + baseURL + "\n\n")
	builder.WriteString("Documentation (recommended reading order):\n")

	for i, meta := range metas {
		builder.WriteString(fmt.Sprintf("  %d. %s - %s\n", i+1, meta.file, meta.description))
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
