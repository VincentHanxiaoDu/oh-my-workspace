//go:build !unix

package daemon

import (
	"fmt"

	"github.com/VincentHanxiaoDu/oh-my-workspace/internal/tri"
)

// confirmOwnerOnly on a build with no owner-only file semantics this package can read.
//
// It returns UNDETERMINED, not No. The distinction is the whole of §4.3 and it changes what a
// person is told: "this socket is reachable by others" is a finding, and "I could not establish
// who can reach this socket" is an absence of one. Both close the control API — §4.6 opens only on
// a confirmed yes — but only the second is honest here.
func confirmOwnerOnly(path string) (tri.Value, string) {
	return tri.Undetermined, fmt.Sprintf(
		"this build cannot read owner-only permissions for %s, so it could not be confirmed that "+
			"only you can reach the control API", path)
}
