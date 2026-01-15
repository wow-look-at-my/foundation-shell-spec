package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
		// Skip the generator directory and hidden files
		if info.IsDir() && (info.Name() == "generator" || strings.HasPrefix(info.Name(), ".")) {
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

	// Process each file
	var processedFiles []string
	for _, srcPath := range mdFiles {
		relPath, err := filepath.Rel(srcDir, srcPath)
		if err != nil {
			return fmt.Errorf("getting relative path for %s: %w", srcPath, err)
		}

		outPath := filepath.Join(outDir, relPath)

		if err := processFile(srcDir, srcPath, outPath); err != nil {
			return fmt.Errorf("processing %s: %w", srcPath, err)
		}

		processedFiles = append(processedFiles, relPath)
		fmt.Printf("Processed: %s\n", relPath)
	}

	// Generate llms.txt
	if err := generateLLMsTxt(outDir, processedFiles); err != nil {
		return fmt.Errorf("generating llms.txt: %w", err)
	}

	fmt.Printf("\nGenerated %d files + llms.txt\n", len(processedFiles))
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

func generateLLMsTxt(outDir string, files []string) error {
	var builder strings.Builder

	builder.WriteString("# Foundation Shell Specification\n\n")
	builder.WriteString("This is the authoritative specification for Foundation Shell.\n\n")
	builder.WriteString("## Documentation Files\n\n")

	// Sort files for consistent output
	for _, file := range files {
		// Create relative URL path
		urlPath := strings.ReplaceAll(file, string(filepath.Separator), "/")
		builder.WriteString(fmt.Sprintf("- [%s](%s)\n", file, urlPath))
	}

	builder.WriteString("\n## Reading Order\n\n")
	builder.WriteString("For best understanding, read in this order:\n")
	builder.WriteString("1. README.md - Overview\n")
	builder.WriteString("2. lexer.md - Tokenization\n")
	builder.WriteString("3. parser.md - Parsing\n")
	builder.WriteString("4. expansion.md - Variable expansion\n")
	builder.WriteString("5. execution.md - Command execution\n")

	llmsPath := filepath.Join(outDir, "llms.txt")
	return os.WriteFile(llmsPath, []byte(builder.String()), 0644)
}
