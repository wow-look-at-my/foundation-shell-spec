# Execution Specification

This document defines the complete specification for Foundation Shell's command execution system. The executor handles command chains, pipelines, I/O redirection, and both builtin and external commands.

## Overview

Foundation Shell's execution system consists of three layers:

1. **Shell Layer** (shell.go) - REPL, I/O management, interactive features
2. **Chain Layer** (chain.go) - Pipeline and operator handling
3. **Command Layer** (command.go) - Individual command execution

## Architecture

```
User Input
     │
     ▼
┌────────────┐
│   Shell    │  → REPL loop, context management
└─────┬──────┘
      │
      ▼
┌────────────┐
│   Parser   │  → Chain structure
└─────┬──────┘
      │
      ▼
┌────────────┐
│   Chain    │  → Pipeline/operator handling
│  Executor  │
└─────┬──────┘
      │
      ▼
┌────────────┐
│  Command   │  → Builtin or external execution
│  Executor  │
└────────────┘
```

## Shell Layer

### Shell Structure

```go
type Shell struct {
    stdin         io.Reader
    stdout        io.Writer
    stderr        io.Writer
    isInteractive bool
    lastExitCode  int
    executor      expander.SubshellExecutor
}
```

### Creation

```go
// Standard I/O, specify interactivity
func New(interactive bool) *Shell

// Custom I/O streams (for testing)
func NewWithIO(stdin io.Reader, stdout, stderr io.Writer, interactive bool) *Shell
```

### Run Modes

#### Interactive Mode

Activated when:
- `isInteractive == true`
- Uses readline for input with syntax highlighting
- Displays colored prompt based on last exit code
- Handles Ctrl+C (interrupt) and Ctrl+D (EOF)

```go
func (s *Shell) runInteractive(ctx context.Context) int
```

Features:
- Welcome message on start
- Colored prompt: green `$` for success, red `$` for failure
- Real-time syntax highlighting via Painter interface
- Line editing with readline

#### Non-Interactive Mode

Activated when:
- `isInteractive == false`
- Reads from stdin line by line
- No prompt, no welcome message
- Suitable for piped input or scripts

```go
func (s *Shell) runNonInteractive(ctx context.Context) int
```

### Script Execution

```go
func (s *Shell) RunScript(ctx context.Context, filename string) int
```

Opens file and executes as non-interactive shell.

### Single Command Execution

```go
func (s *Shell) RunCommand(ctx context.Context, cmdStr string) int
```

Parses and executes a single command string.

## Chain Execution

### Chain Structure

```go
type Chain struct {
    Commands  []*CommandSpec
    Operators []token.TokenType
}
```

Invariant: `len(Operators) == len(Commands) - 1`

### Executor for Subshells

```go
type Executor struct {
    ctx    context.Context
    stdin  io.Reader
    stderr io.Writer
}

func NewExecutor(ctx context.Context, stdin io.Reader, stderr io.Writer) *Executor
```

Implements `expander.SubshellExecutor` for `$(...)` expansion:

```go
func (e *Executor) Execute(command string) (output string, exitCode int, err error)
```

Key behavior:
- Parses command using internal parser
- Executes using internal chain executor
- Captures stdout to buffer
- Returns output, exit code, and error

### Chain Execution Functions

```go
// Default I/O
func Execute(ctx context.Context, chain *parser.Chain) (exitCode int, err error)

// Custom I/O
func ExecuteWithIO(ctx context.Context, chain *parser.Chain,
    stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error)
```

### Execution Algorithm

```
1. Handle empty chain → return 0
2. Single command → execute directly
3. Multiple commands:
   a. Group consecutive pipes into pipeline segments
   b. Execute each segment
   c. Apply operator logic between segments
   d. Return exit code of last executed command
```

### Operator Semantics

| Operator | Token Type | Behavior |
|----------|------------|----------|
| `\|` | `Pipe` | Connect stdout → stdin |
| `&&` | `And` | Execute next only if exitCode == 0 |
| `\|\|` | `Or` | Execute next only if exitCode != 0 |
| `;` | `Semicolon` | Execute next unconditionally |

### Pipeline Segments

Commands connected by `|` are grouped and executed as a pipeline:

```
cmd1 | cmd2 | cmd3 && cmd4 | cmd5
└───────────────┘    └──────────┘
   Segment 1           Segment 2
```

### Pipeline Execution

```go
func executePipelineWithIO(ctx context.Context, commands []*parser.CommandSpec,
    defaultStdin io.Reader, defaultStdout, defaultStderr io.Writer) (int, error)
```

Algorithm:
```
1. Create N-1 pipes for N commands
2. Wrap stderr in synchronized writer (for concurrent access)
3. Launch all commands concurrently as goroutines
4. Wire stdin/stdout:
   - First command: defaultStdin → pipe[0]
   - Middle commands: pipe[i-1] → pipe[i]
   - Last command: pipe[N-2] → defaultStdout
5. Wait for all commands
6. Return exit code of last command
```

### Thread Safety

Stderr is wrapped in a synchronized writer:

```go
type syncWriter struct {
    mu sync.Mutex
    w  io.Writer
}
```

This prevents interleaved error output from concurrent pipeline commands.

## Command Execution

### CommandSpec Structure

```go
type CommandSpec struct {
    Args         []string
    InputFile    string
    OutputFile   string
    ErrorFile    string
    AppendOutput bool
    AppendError  bool
}
```

### Execute Function

```go
func Execute(ctx context.Context, spec *parser.CommandSpec,
    stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error)
```

### Execution Flow

```
1. Validate: spec != nil && len(Args) > 0
2. Set up I/O redirection:
   a. InputFile → actualStdin
   b. OutputFile → actualStdout
   c. ErrorFile → actualStderr
3. Track opened files for cleanup (deferred)
4. Check if builtin command
   - Yes: ExecuteBuiltin()
   - No: executeExternal()
5. Return exit code
```

### I/O Redirection

#### Input Redirection (`<`)

```go
if spec.InputFile != "" {
    inputFile, err := os.Open(spec.InputFile)
    // Handle error
    actualStdin = inputFile
}
```

#### Output Redirection (`>`, `>>`)

```go
if spec.OutputFile != "" {
    outputFile, err := openOutputFile(spec.OutputFile, spec.AppendOutput)
    // Handle error
    actualStdout = outputFile
}
```

#### Error Redirection (`2>`, `2>>`)

```go
if spec.ErrorFile != "" {
    errorFile, err := openOutputFile(spec.ErrorFile, spec.AppendError)
    // Handle error
    actualStderr = errorFile
}
```

### File Opening

```go
func openOutputFile(path string, appendMode bool) (*os.File, error) {
    flags := os.O_WRONLY | os.O_CREATE
    if appendMode {
        flags |= os.O_APPEND  // >>
    } else {
        flags |= os.O_TRUNC   // >
    }
    return os.OpenFile(path, flags, 0644)
}
```

File permissions: `0644` (rw-r--r--)

### External Command Execution

```go
func executeExternal(ctx context.Context, name string, args []string,
    stdin io.Reader, stdout, stderr io.Writer) (int, error) {

    cmd := exec.CommandContext(ctx, name, args...)
    cmd.Stdin = stdin
    cmd.Stdout = stdout
    cmd.Stderr = stderr

    err := cmd.Run()
    // Handle exit codes and errors
}
```

#### Exit Code Extraction

```go
if err != nil {
    // Check context cancellation
    if ctx.Err() != nil {
        return 1, ctx.Err()
    }

    // Extract exit code from ExitError
    var exitErr *exec.ExitError
    if errors.As(err, &exitErr) {
        return exitErr.ExitCode(), nil
    }

    // Command not found or other error
    return 1, err
}
return 0, nil
```

## Builtin Commands

### Detection

```go
func IsBuiltin(name string) bool
```

### Execution

```go
func ExecuteBuiltin(name string, args []string,
    stdin io.Reader, stdout, stderr io.Writer) error
```

### Available Builtins

See `builtins.go` for implementation. Common builtins:
- `cd` - Change directory
- `pwd` - Print working directory
- `echo` - Print arguments
- `exit` - Exit shell
- `export` - Set environment variables

## Context Handling

All execution functions accept `context.Context`:

```go
func Execute(ctx context.Context, ...) (int, error)
```

### Cancellation

```go
select {
case <-ctx.Done():
    return s.lastExitCode
default:
    // Continue execution
}
```

### Timeout

Commands use `exec.CommandContext` which respects context cancellation and timeout.

## Exit Codes

### Standard Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Misuse of shell command |
| 126 | Command not executable |
| 127 | Command not found |
| 128+N | Killed by signal N |

### Exit Code Flow

```
Command → Exit Code → Chain Logic → Shell.lastExitCode → Prompt Color
```

### Last Exit Code

The shell tracks the last exit code for:
- Prompt coloring (green/red)
- Future `$?` expansion

```go
type Shell struct {
    lastExitCode int
}
```

## Error Handling

### Parse Errors

```go
cmdChain, err := parser.ParseWithExecutor(line, s.executor)
if err != nil {
    if errors.Is(err, parser.ErrEmptyInput) {
        return true  // Silent skip
    }
    // Display diagnostic error
    result := syntax.Analyze(line)
    if !result.Valid {
        fmt.Fprint(s.stderr, syntax.FormatDiagnostics(line, result.Errors))
    } else {
        fmt.Fprintf(s.stderr, "parse error: %v\n", err)
    }
    s.lastExitCode = 1
}
```

### Execution Errors

```go
exitCode, err := chain.ExecuteWithIO(ctx, cmdChain, s.stdin, s.stdout, s.stderr)
if err != nil {
    fmt.Fprintf(s.stderr, "execution error: %v\n", err)
}
s.lastExitCode = exitCode
```

### Defined Errors

```go
var ErrEmptyCommand = errors.New("empty command")
```

## Prompt System

### Prompt Colors

```go
const (
    promptSuccess = "\033[32m"  // Green
    promptFailure = "\033[31m"  // Red
    promptReset   = "\033[0m"
)
```

### Prompt Generation

```go
func (s *Shell) getPrompt() string {
    if s.lastExitCode == 0 {
        return promptSuccess + "$ " + promptReset
    }
    return promptFailure + "$ " + promptReset
}
```

## Interactive Features

### Readline Configuration

```go
cfg := &readline.Config{
    Prompt:          s.getPrompt(),
    InterruptPrompt: "^C",
    EOFPrompt:       "exit",
    Painter:         painter,  // Syntax highlighting
    Stdout:          s.stdout,
    Stderr:          s.stderr,
}
```

### Syntax Highlighting Painter

```go
type syntaxPainter struct {
    highlighter *syntax.Highlighter
}

func (p *syntaxPainter) Paint(line []rune, pos int) []rune {
    return []rune(p.highlighter.Highlight(string(line)))
}
```

### Input Handling

| Input | Behavior |
|-------|----------|
| Empty line | Skip, show new prompt |
| Ctrl+C | Clear line, show new prompt |
| Ctrl+D | Exit shell |
| Valid command | Parse and execute |

## Subshell Execution

### Same-Process Model

Foundation Shell executes command substitutions in the **same process**:

```go
func (e *Executor) Execute(command string) (string, int, error) {
    // Parse using internal parser
    chain, err := parser.Parse(command)

    // Execute using internal chain executor
    var stdout bytes.Buffer
    exitCode, err := ExecuteWithIO(e.ctx, chain, e.stdin, &stdout, e.stderr)

    return stdout.String(), exitCode, nil
}
```

Benefits:
- Access to same environment variables
- No shell spawning overhead
- Consistent behavior with main shell

### Output Capture

- Only stdout is captured
- Stderr goes to shell's stderr
- Trailing newlines preserved (stripped by expander)

## Examples

### Simple Command

```
Input: ls -la
Flow:  Shell → Parser → Chain(1 cmd) → Command → external(ls, [-la])
```

### Pipeline

```
Input: cat file | grep pattern | wc -l
Flow:  Shell → Parser → Chain(3 cmds, 2 pipes)
       → executePipeline:
         goroutine 1: cat file (stdin → pipe1)
         goroutine 2: grep pattern (pipe1 → pipe2)
         goroutine 3: wc -l (pipe2 → stdout)
```

### Conditional

```
Input: make && make test
Flow:  Shell → Parser → Chain([make], [make test], [And])
       → Execute(make)
       → if exitCode == 0: Execute(make test)
       → else: skip
```

### Mixed Operators

```
Input: cmd1 | cmd2 && cmd3 || cmd4 ; cmd5
Flow:
1. Execute pipeline: cmd1 | cmd2 → exitCode1
2. If exitCode1 == 0: Execute cmd3 → exitCode2
   Else: exitCode2 = exitCode1, skip cmd3
3. If exitCode2 != 0: Execute cmd4 → exitCode3
   Else: exitCode3 = exitCode2, skip cmd4
4. Execute cmd5 → finalExitCode (unconditional)
```

### Redirection

```
Input: cat < in.txt > out.txt 2> err.txt
Flow:
1. Open in.txt for reading → actualStdin
2. Open out.txt for writing (truncate) → actualStdout
3. Open err.txt for writing (truncate) → actualStderr
4. Execute cat with redirected I/O
5. Close all opened files
```

### Command Substitution

```
Input: echo $(whoami)
Flow:
1. Parser calls expander with executor
2. Expander finds $(whoami)
3. Executor.Execute("whoami"):
   - Parse "whoami"
   - Execute, capture stdout
   - Return "username\n"
4. Expander strips newline → "username"
5. Parser continues with "echo username"
```

## Testing Considerations

### Shell Testing

- Use `NewWithIO` with buffers for controlled I/O
- Use `os.Pipe()` for stdin when testing interactive mode (readline requires ReadCloser)
- Test both interactive and non-interactive modes

### Chain Testing

- Test single commands
- Test pipelines of various lengths
- Test each operator type
- Test operator combinations
- Test error propagation

### Command Testing

- Test builtin commands
- Test external commands
- Test all redirection types
- Test file permission errors
- Test command not found

### Context Testing

- Test cancellation during execution
- Test timeout behavior

## Resource Management

### File Handle Cleanup

```go
var filesToClose []*os.File
defer func() {
    for _, f := range filesToClose {
        f.Close()
    }
}()
```

### Pipe Cleanup

Pipeline goroutines close their respective pipe writers when done:

```go
defer pipeWriters[idx].Close()
```

### Readline Cleanup

```go
rl, err := readline.NewEx(cfg)
defer rl.Close()
```
