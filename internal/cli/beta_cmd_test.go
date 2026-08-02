package cli

import "fmt"

// The second of the two files behind Issue #24 criterion 3. See alpha_cmd_test.go — which this
// file does not reference, and which does not reference this one.

func init() {
	Register(&Command{
		Name:    "beta-probe",
		Summary: "a second command, in a second file",
		Run: func(env Env) int {
			fmt.Fprintf(env.Stdout, "beta ran with %v\n", env.Args)
			return Success
		},
	})
}
