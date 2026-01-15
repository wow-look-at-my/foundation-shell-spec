#!/usr/bin/env bats

# Command Substitution Tests
# These tests verify that $(...) and backtick command substitutions work correctly.

setup() {
    # Path to the shell executable
    FSH="${FSH:-$BATS_TEST_DIRNAME/../../build/fsh-exec}"

    # Ensure the shell exists
    if [[ ! -x "$FSH" ]]; then
        skip "fsh-exec not found at $FSH - run 'just build' first"
    fi
}

# Helper to run a command through fsh-exec
# fsh-exec takes command as arguments (not -c flag)
run_fsh() {
    run "$FSH" $1
}

@test "simple command substitution: echo \$(echo hello)" {
    run_fsh 'echo $(echo hello)'
    [ "$status" -eq 0 ]
    [ "$output" = "hello" ]
}

@test "command substitution with which: echo \$(which echo)" {
    run_fsh 'echo $(which echo)'
    [ "$status" -eq 0 ]
    # Output should contain a path to echo (e.g., /bin/echo or /usr/bin/echo)
    [[ "$output" == *"/echo" ]]
}

@test "command substitution with spaces in command: echo \$(echo hello world)" {
    run_fsh 'echo $(echo hello world)'
    [ "$status" -eq 0 ]
    [ "$output" = "hello world" ]
}

@test "nested command substitution: echo \$(echo \$(echo nested))" {
    run_fsh 'echo $(echo $(echo nested))'
    [ "$status" -eq 0 ]
    [ "$output" = "nested" ]
}

@test "backtick substitution: echo \`echo backtick\`" {
    run_fsh 'echo `echo backtick`'
    [ "$status" -eq 0 ]
    [ "$output" = "backtick" ]
}

@test "backtick with spaces: echo \`echo hello world\`" {
    run_fsh 'echo `echo hello world`'
    [ "$status" -eq 0 ]
    [ "$output" = "hello world" ]
}

@test "command substitution with pipe: echo \$(echo hello | tr a-z A-Z)" {
    run_fsh 'echo $(echo hello | tr a-z A-Z)'
    [ "$status" -eq 0 ]
    [ "$output" = "HELLO" ]
}

@test "multiple command substitutions: echo \$(echo one) \$(echo two)" {
    run_fsh 'echo $(echo one) $(echo two)'
    [ "$status" -eq 0 ]
    [ "$output" = "one two" ]
}

@test "command substitution in middle of string: prefix\$(echo middle)suffix" {
    run_fsh 'echo prefix$(echo middle)suffix'
    [ "$status" -eq 0 ]
    [ "$output" = "prefixmiddlesuffix" ]
}

@test "command substitution preserves exit code on success" {
    run_fsh 'echo $(true)'
    [ "$status" -eq 0 ]
}

@test "single quotes prevent command substitution" {
    run_fsh "echo '\$(echo hello)'"
    [ "$status" -eq 0 ]
    [ "$output" = '$(echo hello)' ]
}

@test "double quotes allow command substitution" {
    run_fsh 'echo "$(echo hello)"'
    [ "$status" -eq 0 ]
    [ "$output" = "hello" ]
}

@test "command substitution with wc" {
    run_fsh 'echo $(echo -n test | wc -c)'
    [ "$status" -eq 0 ]
    # wc output may have leading spaces, so trim
    trimmed=$(echo "$output" | tr -d ' ')
    [ "$trimmed" = "4" ]
}

@test "pwd in command substitution" {
    run_fsh 'echo $(pwd)'
    [ "$status" -eq 0 ]
    # Should output a valid path
    [[ "$output" == /* ]]
}
