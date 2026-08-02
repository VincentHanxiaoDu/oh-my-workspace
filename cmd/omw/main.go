// Command omw is the client a person types.
//
// This file stays small on purpose. It does not know what commands exist — that is the registry's
// job (internal/cli) and each command's own file registers itself. Adding a subcommand must not
// mean editing this file, or every branch that adds one conflicts with every other.
package main

import (
	"os"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"

	// Imported for the registrations in its files' inits. This is the ONE line that grows when a
	// new command package is added, and it is a line of imports rather than a switch — two commands
	// added to the same existing package do not touch this file at all.
	_ "github.com/VincentHanxiaoDu/oh-my-workspace/internal/commands"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}
