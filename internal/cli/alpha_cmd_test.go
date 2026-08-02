package cli

import "fmt"

// One of the two files behind Issue #24 criterion 3. It is self-contained: it names no other
// command file, and no other file names it. Its sibling is beta_cmd_test.go. Adding a third would
// mean adding a third file and editing neither of these.
//
// This is the shape a real command file takes — the only difference is that a real one lives in
// internal/commands and is compiled into the binary.

func init() {
	Register(&Command{
		Name:    "alpha-probe",
		Summary: "a command declared entirely within its own file",
		Run: func(env Env) int {
			fmt.Fprintln(env.Stdout, "alpha ran")
			return Success
		},
	})
}
