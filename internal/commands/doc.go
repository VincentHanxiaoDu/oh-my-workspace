// Package commands holds one file per `omw` subcommand.
//
// A command file declares its Command and calls cli.Register from an init. Nothing in this package
// imports anything else in it, and no file here lists the others — so two Issues each adding a
// command add one new file apiece and never touch a line the other wrote.
//
// It is EMPTY as of the scaffold, and that is the honest state: this build has no commands. The
// capabilities that fill it are the open feature Issues, each of which brings its own file here.
package commands
