#!/usr/bin/env bash
# A SHIM, AND IT IS FIVE LINES ON PURPOSE. The implementation moved to ../adf/check_review.py; this path
# stays because gates.yml, run-gates.sh, every role prompt and the consumer's own tests all invoke
# it. A shim with no logic in it is a shim with nothing to get wrong — which is the whole reason the
# implementation moved in the first place.
#
# `exec`, so the exit code is the Python program's own. These scripts signal five different facts
# through five different exit codes and a wrapper that returned its own would erase all of them.
exec python3 "$(dirname "${BASH_SOURCE[0]}")/../adf/check_review.py" "$@"
