// Issue #67, BLOCKER 3: `omw devices list` exited 1 on a successful listing.
//
// A partial inventory is a screen that WAS produced, with something on it this machine could not
// establish — the other devices registered under this person's name. `omw status --help` names the
// code for exactly that shape ("3 — the screen was produced and something on it could not be
// determined") and `references scan` already uses it. Exit 1 says the command could not do what was
// asked, so every script that treats non-zero as failure reported a healthy device list as broken.
//
// This file drives BOTH endings — a genuinely complete inventory and a partial one — and asserts
// they differ in the rendered listing AND in the exit code. A test that only asserted the partial
// listing says something about being partial passes against the build this Issue was filed on.
package commands

import (
	"strings"
	"testing"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/cli"
	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/devices"
)

// ISSUE #67 CRITERION 5. Both halves are driven; neither is assumed from the other.
func TestAPartialDeviceListingExitsUndeterminedAndACompleteOneExitsSuccess(t *testing.T) {
	// The partial half: no hub configured, so the machines registered on this person's OTHER
	// devices are not in this listing and cannot be.
	noHub := devicesEnv(t, nil)
	registerVia(t, noHub, "laptop-1", "store-A")
	partialCode, partial, partialErr := runDevices2(t, noHub, "list")

	// The complete half: a hub that answers, so the listing really is the whole inventory.
	withHubEnv := devicesEnv(t, map[string]string{"OMW_HUB": "https://hub.example"})
	registerVia(t, withHubEnv, "laptop-1", "store-A")
	withHub(t, []devices.Device{{Label: "laptop-1", CheckIn: devices.NeverCheckedIn(), Source: devices.SourceHub}})
	completeCode, complete, completeErr := runDevices2(t, withHubEnv, "list")

	// THE TWO MUST DIFFER, IN BOTH CHANNELS. This is the assertion that the broken build fails and
	// that a fix which always exits 0 would also fail.
	if partial == complete {
		t.Errorf("a partial inventory and a complete one render identically:\n%s", partial)
	}
	if partialCode == completeCode {
		t.Errorf("a partial inventory and a complete one both exit %d", partialCode)
	}

	if completeCode != cli.Success {
		t.Errorf("a complete inventory exited %d, want Success (%d):\n%s%s",
			completeCode, cli.Success, complete, completeErr)
	}
	if partialCode != cli.ExitUndetermined {
		t.Errorf("a partial inventory exited %d, want ExitUndetermined (%d) — 1 is 'the command could "+
			"not do what was asked', and this listing was produced:\n%s%s",
			partialCode, cli.ExitUndetermined, partial, partialErr)
	}

	// The listing itself still reports the partiality in prose; the exit code change does not
	// quietly turn a partial listing into one that presents as whole.
	if !strings.Contains(partial, "listing complete: no") {
		t.Errorf("the partial listing no longer says it is partial:\n%s", partial)
	}
	if !strings.Contains(partial, "missing:") {
		t.Errorf("the partial listing no longer says WHAT is missing (PRD §4.4):\n%s", partial)
	}
	if !strings.Contains(complete, "listing complete: yes") {
		t.Errorf("the complete listing does not claim completeness:\n%s", complete)
	}
}

// THE OTHER HALF OF BLOCKER 3'S RISK. Moving the partial listing off ExitFailure must not move a
// genuine refusal off it: a duplicate label is still a command that could not do what was asked.
func TestARefusedRegistrationStillExitsFailure(t *testing.T) {
	env := devicesEnv(t, nil)
	if code, _, _ := registerVia(t, env, "laptop-1", "store-A"); code != cli.Success {
		t.Fatalf("the first registration exited %d", code)
	}
	code, _, errOut := registerVia(t, env, "laptop-1", "store-A")
	if code != cli.ExitFailure {
		t.Errorf("a refused duplicate registration exited %d, want ExitFailure (%d):\n%s",
			code, cli.ExitFailure, errOut)
	}
}
