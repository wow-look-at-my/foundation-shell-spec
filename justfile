# Foundation Shell Specification

# Generate documentation to dist/
generate:
    rm -rf dist
    cd generator && go run . ../src ../dist
    cp index.html dist/

# Clean generated files
clean:
    rm -rf dist

# Run generator and show output
dev: generate
    @echo "\nGenerated files:"
    @ls -la dist/
