package store

import (
	"os"
	"path/filepath"
	"runtime"
)

// PathEnv is the environment variable that names the store's location outright.
//
// It exists so that a person can put the store where they want it, and so that every test in this
// product can run against a store of its own without touching the machine's real one. It takes
// precedence over the derived location.
const PathEnv = "OMW_STORE"

// Resolve works out where this device's store lives, WITHOUT looking at the disk and WITHOUT
// creating anything.
//
// ONE STORE PER DEVICE (§2.1, criterion 4). The answer depends only on the environment, never on
// the working directory, so every command in the product resolves to the same store from anywhere
// on the filesystem. There is no upward search for a nearby store — that is how a second store gets
// created in a project folder and half the tickets go missing.
//
// Order:
//
//	$OMW_STORE                       if set — used exactly as given, made absolute
//	$XDG_DATA_HOME/omw/store         if set
//	$HOME/Library/Application Support/omw/store   on macOS
//	$HOME/.local/share/omw/store     otherwise
//
// A missing home directory is [ErrPathUndetermined], not a fallback to the working directory: a
// store in whatever folder somebody happened to be standing in is exactly the store this product
// promises not to create.
func Resolve(getenv func(string) string) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if p := getenv(PathEnv); p != "" {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", pathErr("resolve", p, ErrPathUndetermined, PathEnv+" is set but is not a usable path")
		}
		return abs, nil
	}
	if x := getenv("XDG_DATA_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "omw", "store"), nil
	}
	home := getenv("HOME")
	if home == "" || !filepath.IsAbs(home) {
		return "", pathErr("resolve", "", ErrPathUndetermined,
			"neither "+PathEnv+" nor an absolute HOME is set in this environment")
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "omw", "store"), nil
	}
	return filepath.Join(home, ".local", "share", "omw", "store"), nil
}
