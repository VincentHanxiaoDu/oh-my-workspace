// Issue #9: whether the person has a model configured, and their key.
//
// ISSUE #18 OWNS MODEL CONFIGURATION and has not landed. What is here is the smallest thing
// `review` cannot run without — a name, and a key — read from the environment, kept behind a method
// so that the day #18 lands there is one function to replace and no second vocabulary to reconcile.
// It is called out in the pull request rather than left for a reader to discover.
//
// THE KEY IS NEVER RENDERED (criterion 18). That is not achieved by remembering not to print it:
// the field is unexported, and [ModelConfig] has a String method, so the one accident that would
// otherwise do it — `fmt.Printf("%v", cfg)`, which reflects into unexported fields quite happily —
// prints the description instead. The only way to the key is [ModelConfig.Key], which is called in
// exactly one place.
package drafts

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/hub"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// The environment this build reads a model from. Placeholders for Issue #18.
const (
	// ModelEnv names the person's model.
	ModelEnv = "OMW_MODEL"
	// ModelKeyEnv carries the key itself.
	ModelKeyEnv = "OMW_MODEL_KEY"
	// ModelKeyFileEnv names a file holding the key, for people who will not put a secret in their
	// environment. It is also the honest source of an UNDETERMINED answer: a key file that exists
	// and cannot be read means whether a model is configured is not something we know.
	ModelKeyFileEnv = "OMW_MODEL_KEY_FILE"
)

// ErrNoModel — `review` was asked for and there is no model to run it with.
var ErrNoModel = &hub.Error{
	Code: "no-model",
	Msg:  "no model is configured, and review mode checks your drafts with your own model",
}

// ErrModelUndetermined — whether a model is configured could not be established.
var ErrModelUndetermined = &hub.Error{
	Code: "model-undetermined",
	Msg:  "whether a model is configured could not be determined, which is neither yes nor no",
}

// ModelConfig is what the client knows about the person's model.
type ModelConfig struct {
	// Configured is Yes, No, or Undetermined. Three answers (criterion 19).
	Configured tri.Value
	// Name is the model's name. Not a secret; a person needs to see which model would run.
	Name string
	// Missing names the part that is absent, when Configured is No.
	Missing string
	// Why says what could not be read, when Configured is Undetermined.
	Why string

	// key is unexported and stays that way. See the file comment.
	key string
}

// ReadModel reports whether a model is configured, without saying anything about the key beyond
// whether there is one.
func ReadModel(getenv func(string) string) ModelConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	name := strings.TrimSpace(getenv(ModelEnv))
	keyFile := strings.TrimSpace(getenv(ModelKeyFileEnv))
	key := getenv(ModelKeyEnv)

	// THE UNREADABLE KEY FILE IS CHECKED FIRST, and it is checked even when no model is named.
	// A person who set up a key file and cannot read it is not a person with no model; they are a
	// person whose configuration we cannot see, and answering "no model" would send them off to
	// configure the thing they already configured.
	if keyFile != "" {
		b, err := os.ReadFile(keyFile)
		switch {
		case err == nil:
			key = string(b)
		case errors.Is(err, fs.ErrNotExist):
			return ModelConfig{Configured: tri.No, Name: name, Missing: "the key file " + keyFile + " named by $" + ModelKeyFileEnv + " does not exist"}
		default:
			return ModelConfig{Configured: tri.Undetermined, Name: name, Why: "the key file named by $" + ModelKeyFileEnv + " could not be read: " + err.Error()}
		}
	}

	switch {
	case name == "" && strings.TrimSpace(key) == "":
		return ModelConfig{Configured: tri.No, Missing: "no model and no key are configured; set $" + ModelEnv + " and $" + ModelKeyEnv}
	case name == "":
		return ModelConfig{Configured: tri.No, Missing: "a key is configured but no model is named; set $" + ModelEnv}
	case strings.TrimSpace(key) == "":
		return ModelConfig{Configured: tri.No, Name: name, Missing: "the model " + name + " is named but no key is configured; set $" + ModelKeyEnv + " or $" + ModelKeyFileEnv}
	}
	return ModelConfig{Configured: tri.Yes, Name: name, key: key}
}

// Key returns the person's key. The one caller is the thing that talks to their model.
func (m ModelConfig) Key() string { return m.key }

// HasKey answers the only question about the key that any report may ask.
func (m ModelConfig) HasKey() bool { return m.key != "" }

// Render is the one rendering, and its three branches are pairwise distinct. It never contains the
// key, on any branch.
func (m ModelConfig) Render() string {
	switch m.Configured {
	case tri.Yes:
		return "model: " + m.Name + " is configured, with a key (the key is never printed)"
	case tri.No:
		s := "model: none is configured"
		if m.Missing != "" {
			s += "\n  " + m.Missing
		}
		return s
	default:
		s := "model: whether one is configured " + tri.Undetermined.String() + " — this is not 'no model'"
		if m.Why != "" {
			s += "\n  " + m.Why
		}
		return s
	}
}

// String makes the accident safe: %v on a ModelConfig prints the description, never the key.
func (m ModelConfig) String() string { return m.Render() }
