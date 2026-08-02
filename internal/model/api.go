// What an API surface may say about a person's model, and the one thing it may not (Issue #18
// criteria 6, 7, 18; PRD §3.12, §4.3, §4.5).
//
// # WHY A SEPARATE TYPE FROM Config
//
// [Config] carries the credential. It is careful about it — the field is unexported, String and
// GoString are defined — but "careful" is a property somebody has to keep true, and the surface
// that most needs it not to leak is the one furthest from this file: the control API and, when
// Issue #16 builds it, the agent API. Those serialise. `encoding/json` skips unexported fields
// today, and the change that makes it not skip them is one person exporting a field to fix a
// different problem.
//
// So the thing that crosses an API boundary is a DIFFERENT TYPE THAT HAS NO CREDENTIAL IN IT. Not
// a redacted Config, not a Config with a `json:"-"` tag — a struct with nowhere to put one. That is
// the difference between a rule and a guarantee, and TestTheViewHasNowhereToPutACredential asserts
// it structurally rather than by grepping one instance.
//
// # THE REFUSAL IS NOT AN EMPTY STRING (criterion 6)
//
// "A request that asks for it is refused rather than answered with a redacted or empty value that a
// caller could mistake for 'no credential configured' — refusal and 'no credential configured' are
// distinguishable in the agent API response."
//
// [CredentialThrough] is the whole implementation of that sentence: it always refuses, with
// [ErrCredentialNotReadable]'s own code, and "no credential configured" is a SUCCESSFUL answer
// carried in [View.CredentialPresent]. A caller can therefore be in exactly one of three states —
// told there is a credential, told there is not, told it may not have the value — and no two of
// them arrive the same way.
package model

import "github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"

// View is a model configuration as an API surface reports it.
//
// The tri values cross the wire as their own rendering rather than as integers, for the reason
// daemon.Report's text fields do: a Value's zero value is meaningful, and a future reordering of
// the constants would silently reinterpret every number already sent.
type View struct {
	// Provider is the chosen provider's name, or empty when none is chosen. Not a secret
	// (criterion 6: "may report which provider is configured").
	Provider string `json:"provider,omitempty"`
	// ProviderChosen is yes / no / could not be determined.
	ProviderChosen string `json:"provider_chosen"`
	// CredentialPresent is yes / no / could not be determined. WHETHER, never WHICH.
	CredentialPresent string `json:"credential_present"`
	// Detail is the reason behind a negative or an undetermined answer. It is built from
	// [Config.Missing] and [Config.Why], neither of which ever contains the credential.
	Detail string `json:"detail,omitempty"`
	// Adapter is whether THIS BUILD has code that can talk to the chosen provider — yes / no /
	// could not be determined — and is empty when no provider is chosen, because the question does
	// not arise.
	//
	// IT IS PART OF THE STATE, NOT ADVICE ONE SURFACE ADDS. It got here the hard way: `omw model
	// show` printed "this build has no adapter for acme" as two extra lines after the state
	// paragraph, and `omw daemon status` did not, so criterion 18's agreement test went red on all
	// four configurations. The fix a person reaches for first is to move the extra lines somewhere
	// the comparison does not look — which keeps two surfaces saying different things and hides it.
	// The fix that holds is for there to be nothing a surface may add: everything a person is told
	// about their model configuration is in this struct, and rendering it is [View.Render].
	//
	// It is a DETERMINED fact, and it is neither "no provider chosen" nor "undetermined". A person
	// who chose `acme` on a build with no acme adapter has done what Issue #18 asked of them; what
	// is missing is the code Issue #21's mechanism registers, and telling them "no provider is
	// chosen" would send them to configure what they already configured.
	Adapter string `json:"adapter,omitempty"`
}

// View projects a Config down to what may leave the machine's own process.
func (c Config) View() View {
	v := View{
		Provider:          c.Name,
		ProviderChosen:    c.Provider.String(),
		CredentialPresent: c.Credential.String(),
	}
	if c.Provider == tri.Yes {
		// The registry is consulted, and NOTHING IS OPENED and nothing is dialled: Lookup reads a
		// map. See provider.go — registration itself contacts nothing, which is what makes asking
		// this question on every read-out free and network-silent (§4.2).
		v.Adapter = tri.No.String()
		if _, ok := Lookup(c.Name); ok {
			v.Adapter = tri.Yes.String()
		}
	}
	switch {
	case c.Why != "":
		v.Detail = c.Why
	case c.Missing != "":
		v.Detail = c.Missing
	case c.Source != "":
		// WHERE the credential came from, never WHAT it is. A person with a key in their
		// environment and a key file on disk needs to know which one is in play; "$OMW_MODEL_KEY"
		// and a path are both locations, and neither is a secret.
		v.Detail = "the credential comes from " + c.Source
	}
	return v
}

// Chosen and Present read the wire text back as tri values, so that a consumer of a View compares
// values pairwise instead of matching strings against literals it spelled itself.
func (v View) Chosen() tri.Value  { return triFrom(v.ProviderChosen) }
func (v View) Present() tri.Value { return triFrom(v.CredentialPresent) }

func triFrom(text string) tri.Value {
	switch text {
	case tri.Yes.String():
		return tri.Yes
	case tri.No.String():
		return tri.No
	default:
		// A word this build does not know is, precisely, a state it could not determine.
		return tri.Undetermined
	}
}

// Render is the one rendering of a View, and it is what every API-adjacent surface prints so that
// the CLI and the control API cannot word the same state differently (criterion 18, §4.3).
func (v View) Render() string {
	switch {
	case v.Chosen() == tri.Undetermined:
		return withDetail("model: which provider is configured "+tri.Undetermined.String()+
			" — this is NOT 'no model configured'", v.Detail)
	case v.Chosen() == tri.No:
		return withDetail("model: no provider is chosen", v.Detail)
	case v.Present() == tri.Undetermined:
		return withDetail("model: provider "+v.Provider+" is chosen; whether a credential is supplied for it "+
			tri.Undetermined.String()+" — this is NOT 'no credential'", v.Detail)
	case v.Present() == tri.No:
		return v.withAdapter(withDetail("model: provider "+v.Provider+" is chosen, and NO credential has been supplied for it", v.Detail))
	default:
		return v.withAdapter(withDetail("model: provider "+v.Provider+" is configured, with a credential"+
			" (the credential itself is never printed)", v.Detail))
	}
}

// withAdapter appends the adapter fact, and only when there is something to say: a build that CAN
// talk to the chosen provider adds no line, because the ordinary case should not carry a sentence
// about a thing that is fine.
//
// The undetermined branches above do not call this. Whether this build has an adapter for a
// provider whose NAME could not be established is not a question with an answer.
func (v View) withAdapter(s string) string {
	if v.Adapter != tri.No.String() {
		return s
	}
	return s + "\n  this build has no adapter for " + v.Provider +
		", so review cannot run against it yet; 'omw model providers' lists the ones it has."
}

// CredentialThrough is what an API surface gets when it asks for the credential VALUE.
//
// It always refuses, and the signature is why it is a function at all rather than a comment: there
// is no code path that returns a credential from a View, so a surface cannot accidentally acquire
// one, and a surface that WANTS to must delete this call and reach into [Config.Secret] — which is
// guarded by its own test.
//
// The View is taken and ignored on purpose. A caller holding a fully configured View still gets the
// refusal; "there is a credential and you may not have it" and "there is no credential" are the two
// answers criterion 6 requires be distinguishable, and they are, because the first is this error
// and the second is v.Present() == tri.No on a successful call.
func CredentialThrough(v View) (string, error) {
	_ = v
	return "", ErrCredentialNotReadable
}
