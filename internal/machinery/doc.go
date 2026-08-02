// Package machinery holds no product code. It exists so that the repository can assert, from a
// file it owns, things about the CI machinery it does not own.
//
// `.github/`, `.workflow/bin/` and `.claude/` are installed by agent-dev-flow and are REPLACED
// WHOLESALE on every refresh. A fix made in those trees survives until the next install and no
// longer; the confusion this package guards against has already been fixed there twice and come
// back twice. A test here runs under `make ci`, which the `Build and tests` gate invokes, so a
// refresh that reintroduces the defect turns this repository's own suite red instead of silently
// restoring it.
//
// Tests here therefore READ AND EXECUTE the installed machinery rather than reimplementing it.
// A test that asserted a copy of the gate would go green while the gate itself was broken; this
// project has already shipped one of those.
package machinery
