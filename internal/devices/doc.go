// Package devices is the person's own inventory of the machines carrying their store (PRD §3.8).
//
// # The sentence this package exists for
//
// "Every device is listed, including one never started — a device that has not checked in is a
// fact worth seeing, not an absence." (PRD §3.8, and §4.3 read across to devices.)
//
// That sentence is a statement about REPRESENTATION, so the representation is where it is
// enforced. A device's check-in is a [CheckIn], whose State is a [tri.Value]:
//
//	tri.Yes           this device checked in, and At says when
//	tri.No            this device was registered and has never checked in
//	tri.Undetermined  whether it has checked in could not be worked out
//
// A bool with a timestamp beside it cannot hold three answers, and a `*time.Time` holds two of
// them by conflating "never" with "unknown" — which is the exact collapse §4.3 forbids. The
// distinction is not left to the renderer either: [CheckIn.Describe] is the only rendering, so
// there is one place where two of the three could ever be made to read alike, and a test compares
// the three PAIRWISE rather than against wording.
//
// # A registration is written, never inferred
//
// On disk, "never checked in" is a value the registry WROTE (`"state":"never"`), not the absence
// of a field. A missing or unparseable check-in reads back as [tri.Undetermined], because an
// answer nobody recorded has not been determined. Registration writes the "never" explicitly, so
// the never-started box is a fact on disk from the moment it is registered.
//
// # A registration is an explicit act (PRD §4.2)
//
// Nothing in this package registers a device as a side effect of anything else. [Registry.Register]
// is the only function that adds one, and the only caller is the `omw devices register` command a
// person types. Nothing here starts a daemon and nothing here opens a network connection — the
// package does not import net, and a tree-wide test enforces that.
//
// # A local list is not a complete list (PRD §4.4)
//
// This registry holds what THIS machine knows. The person's whole inventory lives on the hub, so a
// [Snapshot] carries a Complete [tri.Value] and the precise reasons it is not Yes. A listing that
// could not be completed and a listing that is genuinely empty are different facts and render
// differently; see [Snapshot.Render] and [Load].
package devices
