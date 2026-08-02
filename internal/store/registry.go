package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// registryName is the file recording WHICH store is this device's store.
//
// WHY A POINTER FILE EXISTS AT ALL. "One store per device" (§2.1, criterion 4) cannot be enforced by
// a naming convention: a person may put their store anywhere, so the product cannot tell "the store
// they created somewhere unusual" from "a second store" by looking at paths. Without something
// recording the choice, `omw store create ~/A` followed by `omw store create ~/B` makes two stores
// and neither §3.14's "sole home of unpublished data" nor criterion 4 survives it.
//
// It is deliberately a POINTER and not the store: it holds one path and no unpublished data, so it
// is not a second copy of anything (criterion 14).
const registryName = "device-store.json"

// registryFormat is the pointer file's layout version.
const registryFormat = 1

type registryFile struct {
	Format    int    `json:"format"`
	StorePath string `json:"store_path"`
}

// productDir is the per-user directory the product keeps its own state in — the pointer file, and
// nothing else today. It is NOT the store.
func productDir(getenv func(string) string) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if x := getenv("XDG_DATA_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "omw"), nil
	}
	home := getenv("HOME")
	if home == "" || !filepath.IsAbs(home) {
		return "", pathErr("resolve", "", ErrPathUndetermined,
			"neither "+PathEnv+" nor an absolute HOME is set in this environment")
	}
	if isDarwin {
		return filepath.Join(home, "Library", "Application Support", "omw"), nil
	}
	return filepath.Join(home, ".local", "share", "omw"), nil
}

// RegistryPath is where this device records which store is its store.
func RegistryPath(getenv func(string) string) (string, error) {
	dir, err := productDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, registryName), nil
}

// Registered reports the store path this device has registered, in three values.
//
// Yes with a path: this device has a registered store. No: nothing has been registered — which is
// the state of a machine that has never run the creation command, and is NOT an error. Undetermined:
// the pointer exists and could not be read, so whether a store is registered is unknown; a caller
// must not treat that as "none" and go on to create a second one.
func Registered(getenv func(string) string) (string, tri.Value, error) {
	path, err := RegistryPath(getenv)
	if err != nil {
		return "", tri.Undetermined, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return "", tri.No, nil
		}
		return "", tri.Undetermined, pathErr("open", path, ErrUnreadable,
			"this device's store pointer could not be read: "+err.Error())
	}
	var rf registryFile
	if err := json.Unmarshal(body, &rf); err != nil {
		return "", tri.Undetermined, pathErr("open", path, ErrUnreadable,
			"this device's store pointer is damaged: "+err.Error())
	}
	if rf.Format != registryFormat || rf.StorePath == "" || !filepath.IsAbs(rf.StorePath) {
		return "", tri.Undetermined, pathErr("open", path, ErrUnreadable,
			"this device's store pointer does not name an absolute path this build understands")
	}
	return rf.StorePath, tri.Yes, nil
}

// register records storePath as this device's store, atomically.
//
// The product's own directory IS created here if it is missing, because it is the product's, not the
// person's — no unpublished data lives in it, and refusing to make it would mean the pointer could
// never be written on a machine that has never run omw before.
func register(getenv func(string) string, storePath string) error {
	path, err := RegistryPath(getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return pathErr("create", path, ErrPermissionDenied,
			"this device's store pointer could not be written: "+err.Error())
	}
	body, err := json.Marshal(registryFile{Format: registryFormat, StorePath: storePath})
	if err != nil {
		return pathErr("create", path, ErrUnreadable, err.Error())
	}
	if err := writeFileAtomic(path, body); err != nil {
		return pathErr("create", path, ErrPermissionDenied,
			"this device's store pointer could not be written: "+err.Error())
	}
	return nil
}
