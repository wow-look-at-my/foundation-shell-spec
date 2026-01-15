# Foundation Shell Specification

# Build the generator
build:
    cd generator && go build -o ../bin/generate .

# Generate documentation to dist/
generate: build
    rm -rf dist
    ./bin/generate src dist

# Clean generated files
clean:
    rm -rf dist bin

# Run generator and show output
dev: generate
    @echo "\nGenerated files:"
    @ls -la dist/
