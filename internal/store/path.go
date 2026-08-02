package store

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// PathEnv is the environment variable that names the store's location outright.
//
// It exists so that a person can put the store where they want it, and so that every test in this
// product can run against a store of its own without touching the machine's real one. It takes
// precedence over everything else.
const PathEnv = "OMW_STORE"

// isDarwin is the one place the store's DEFAULT LOCATION depends on the operating system, and it is
// a convention about where a user's data belongs — not a behaviour. Nothing about detecting a
// synchronising location branches on it; criterion 7 is explicit that it must not.
var isDarwin = runtime.GOOS == "darwin"

// DefaultPath is where a store goes on this machine when nobody has said otherwise:
// `$XDG_DATA_HOME/omw/store`, else `~/Library/Application Support/omw/store` on macOS, else
// `~/.local/share/omw/store`.
func DefaultPath(getenv func(string) string) (string, error) {
	dir, err := productDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "store"), nil
}

// Resolve works out where THIS DEVICE'S store lives, without creating anything.
//
// ONE STORE PER DEVICE (§2.1, criterion 4). The answer never depends on the working directory, so
// every command in the product resolves to the same store from anywhere on the filesystem. There is
// no upward search for a nearby store — that is how a second store gets created in a project folder
// and half the tickets go missing.
//
// Order:
//
//	$OMW_STORE                    if set — used exactly as given, made absolute
//	the device's registered store if creation has recorded one (see [Registered])
//	[DefaultPath]                 otherwise
//
// THE REGISTRY LOOKUP IS WHY THIS FUNCTION TOUCHES THE DISK, which an earlier version of it did not.
// A person may create their store anywhere; without reading what creation recorded, every later
// command would look in the default location, find nothing, and report that the store they had just
// made did not exist. A pointer that exists and cannot be read is [ErrPathUndetermined] — never a
// silent fall through to the default, because that would send the next command off to create a
// SECOND store.
//
// A missing home directory is [ErrPathUndetermined] too, not a fallback to the working directory: a
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
	registered, found, err := Registered(getenv)
	switch found {
	case tri.Yes:
		return registered, nil
	case tri.Undetermined:
		if err != nil {
			return "", err
		}
		return "", pathErr("resolve", "", ErrPathUndetermined,
			"this device's store pointer could not be read")
	}
	return DefaultPath(getenv)
}
