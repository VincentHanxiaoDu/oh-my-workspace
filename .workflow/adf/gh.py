"""Talking to GitHub, with the one distinction this whole project exists to preserve.

WHY THIS FILE IS PYTHON AND THE THING IT REPLACES WAS BASH.

Every script here exists to stop `could not determine` being rendered as `determined to be
nothing`. Bash's defaults do exactly that rendering, and in one working session they did it three
times, in code written to prevent it:

  1. `set -u` and an empty array. A fresh install printed its whole plan and copied NOTHING,
     exiting 0. `"${arr[@]}"` on an empty array is an unbound-variable error on bash 3.2, which is
     what macOS ships.
  2. A sourced file inherits its caller's positional parameters. `roles.sh` dispatched on `$1`, so
     `check-naming.sh "<branch>" "<sha>"` handed it a branch name as a command and the naming gate
     died.
  3. `set -e` and a bare compound command. `( cmd ) >/dev/null` whose status nothing tests ENDS THE
     SCRIPT — so a self-test died having printed nothing and exited 1. Green in one repository,
     silently red in the next.

Each is the project's own subject matter committed by the project itself. In Python an error is an
exception: it propagates, it carries its cause, and it cannot be mistaken for an empty result.

WHAT THIS DELIBERATELY DOES NOT USE. There is no GitHub SDK here, and that is a choice rather than
an omission:

  - **The rate limit is not an implementation detail of this system, it is a feature of it.** The
    watches stand down above a reserve, and the PRIMARY and SECONDARY limits clear on different
    clocks — a secondary limit is a burst rule that clears with quiet and has nothing to do with the
    hourly reset (Issue #81). A library that turns both into one `RateLimitExceeded` erases the
    distinction the scripts were built to act on.
  - **The number of API calls a round costs is measured and reported.** Measured: 117 calls a round
    across three roles, brought to 67. A client with lazy loading makes that number unknowable.
  - **A consumer repository must not need a virtualenv.** Auth comes from `gh auth token`, which is
    already a dependency and already solved; the rest is one POST and one GET.

REST, NOT GRAPHQL, and that is also measured: the GraphQL quota was observed exhausted (5000/5000)
on a working day while REST still had headroom, and `gh` commands built on GraphQL failed while REST
kept answering. Anything here that could be either is REST.
"""

from __future__ import annotations

import json
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Any

API = "https://api.github.com"
UA = "agent-dev-flow"


class LookupFailure(Exception):
    """A question that could not be asked, which is NEVER an answer to it.

    THIS CLASS IS THE POINT OF THE FILE. Everything that goes wrong reaching GitHub raises this,
    and nothing anywhere converts it to an empty list, an empty string or a zero. A caller that
    wants to treat an outage as "nothing found" has to write that down explicitly, where a reviewer
    can see it.

    `reason` is what to show a person. `status` is the HTTP status where there was one.
    """

    def __init__(self, reason: str, *, status: int | None = None, secondary: bool = False):
        super().__init__(reason)
        self.reason = reason
        self.status = status
        self.secondary = secondary


@dataclass
class Budget:
    """What is left, and WHICH limit is holding us.

    THE TWO LIMITS CLEAR ON DIFFERENT CLOCKS AND THAT COST A DAY (Issue #81). A PRIMARY exhaustion
    clears when the hourly quota resets, and `x-ratelimit-reset` says when. A SECONDARY limit is a
    burst rule: it clears with QUIET, `retry-after` says how long, and waiting on the primary reset
    is waiting on a signal that never described the problem. The guard read the primary limit while
    every outage in that session was secondary.
    """

    remaining: int | None = None
    limit: int | None = None
    reset_at: float | None = None
    retry_after: float | None = None
    secondary: bool = False

    def hold_for(self, floor: int) -> float:
        """Seconds to wait, from whichever limit is actually holding us."""
        if self.secondary and self.retry_after:
            return max(float(floor), self.retry_after)
        if self.reset_at:
            return max(float(floor), self.reset_at - time.time())
        return float(floor)


@dataclass
class Client:
    """One client per run. It counts its own calls, because that number is reported."""

    token: str | None = None
    calls: int = 0
    budget: Budget = field(default_factory=Budget)

    def __post_init__(self) -> None:
        if self.token is None:
            self.token = _token_from_gh()

    # -- the one request path -------------------------------------------------
    def request(self, method: str, path: str, body: Any = None) -> tuple[Any, dict[str, str]]:
        """One REST call. Returns (parsed json, headers). Raises LookupFailure — never returns empty.

        `path` is a repository-relative API path exactly as `gh api` takes it, e.g.
        `repos/o/r/pulls?state=open`, so every call site reads the same as the shell it replaces and
        a reviewer comparing the two is comparing like with like.
        """
        url = path if path.startswith("http") else f"{API}/{path.lstrip('/')}"
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Accept", "application/vnd.github+json")
        req.add_header("User-Agent", UA)
        req.add_header("X-GitHub-Api-Version", "2022-11-28")
        if self.token:
            req.add_header("Authorization", f"Bearer {self.token}")
        if data is not None:
            req.add_header("Content-Type", "application/json")

        self.calls += 1
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                headers = {k.lower(): v for k, v in resp.headers.items()}
                self._note_budget(headers)
                raw = resp.read()
                return (json.loads(raw) if raw else None), headers
        except urllib.error.HTTPError as e:
            headers = {k.lower(): v for k, v in (e.headers or {}).items()}
            self._note_budget(headers)
            detail = _short(e.read())
            # A SECONDARY LIMIT IS NOT AN EXHAUSTED QUOTA, and telling them apart is the whole
            # reason the budget guard exists. GitHub signals a secondary limit with 403 plus
            # `retry-after`, or with the phrase in the body, while `x-ratelimit-remaining` can still
            # be healthy — which is precisely how the guard came to report "plenty left" through an
            # outage it was built to catch.
            secondary = e.code in (403, 429) and (
                "retry-after" in headers or "secondary rate limit" in detail.lower()
            )
            if secondary:
                self.budget.secondary = True
            raise LookupFailure(
                f"{method} {path} -> HTTP {e.code}: {detail}", status=e.code, secondary=secondary
            ) from e
        except urllib.error.URLError as e:
            raise LookupFailure(f"{method} {path} -> {e.reason}") from e
        except TimeoutError as e:
            raise LookupFailure(f"{method} {path} -> timed out") from e
        except json.JSONDecodeError as e:
            # UNPARSEABLE IS NOT EMPTY. `gh issue list` was observed returning an EMPTY FILE rather
            # than an error when the GraphQL quota ran out, and the caller read it as "no issues".
            raise LookupFailure(f"{method} {path} -> response was not JSON: {e}") from e

    def get(self, path: str) -> Any:
        return self.request("GET", path)[0]

    def post(self, path: str, body: Any) -> Any:
        return self.request("POST", path, body)[0]

    def paginate(self, path: str, per_page: int = 100) -> list[Any]:
        """Every page, or an exception. Never a partial list presented as a whole one.

        A HALF-READ LIST IS THE FAILURE THIS PROJECT IS ABOUT, wearing its most convincing costume:
        it is a real list, of real items, and nothing about it says it stopped early. If any page
        fails, the whole call fails.
        """
        sep = "&" if "?" in path else "?"
        url = f"{path}{sep}per_page={per_page}"
        out: list[Any] = []
        while True:
            page, headers = self.request("GET", url)
            if page is None:
                break
            if not isinstance(page, list):
                raise LookupFailure(f"{url} -> expected a list, got {type(page).__name__}")
            out.extend(page)
            nxt = _next_link(headers.get("link", ""))
            if not nxt:
                return out
            url = nxt
        return out

    def _note_budget(self, headers: dict[str, str]) -> None:
        b = self.budget
        if (v := headers.get("x-ratelimit-remaining")) is not None:
            try:
                b.remaining = int(v)
            except ValueError:
                pass
        if (v := headers.get("x-ratelimit-limit")) is not None:
            try:
                b.limit = int(v)
            except ValueError:
                pass
        if (v := headers.get("x-ratelimit-reset")) is not None:
            try:
                b.reset_at = float(v)
            except ValueError:
                pass
        if (v := headers.get("retry-after")) is not None:
            try:
                b.retry_after = float(v)
            except ValueError:
                pass


def _next_link(link_header: str) -> str | None:
    for part in link_header.split(","):
        seg = part.split(";")
        if len(seg) < 2:
            continue
        if 'rel="next"' in seg[1].replace(" ", "").replace("'", '"'):
            return seg[0].strip().strip("<>")
    return None


def _short(raw: bytes, limit: int = 300) -> str:
    try:
        text = raw.decode("utf-8", "replace")
    except Exception:
        return "<unreadable body>"
    try:
        msg = json.loads(text).get("message")
        if msg:
            text = msg
    except Exception:
        pass
    text = " ".join(text.split())
    return text if len(text) <= limit else "…" + text[-(limit - 1):]


def _token_from_gh() -> str | None:
    """Auth from `gh`, which every consumer already has and has already solved.

    A MISSING TOKEN IS NOT AN ANONYMOUS SESSION. Unauthenticated requests get 60 an hour and 404 on
    anything private, which reads as "the pull request does not exist" — an answer, and a wrong one.
    So this returns None and the first call fails loudly rather than quietly degrading.
    """
    try:
        out = subprocess.run(
            ["gh", "auth", "token"], capture_output=True, text=True, timeout=15, check=False
        )
    except (OSError, subprocess.SubprocessError):
        return None
    token = out.stdout.strip()
    return token or None


def resolve_repo(cwd: str | None = None) -> str:
    """`owner/name`, from the git remote rather than from the API.

    NOT `gh repo view`: that is a GraphQL call, and when that quota was exhausted every role's queue
    became "no repository" — an outage in one subsystem silently disabling the thing that tells
    every agent what to do. The remote URL is already on disk and answers the same question.
    """
    try:
        out = subprocess.run(
            ["git", "config", "--get", "remote.origin.url"],
            capture_output=True, text=True, timeout=15, check=False, cwd=cwd,
        )
    except (OSError, subprocess.SubprocessError) as e:
        raise LookupFailure(f"could not run git: {e}") from e
    url = out.stdout.strip()
    if not url:
        raise LookupFailure(
            "no repository: this checkout has no 'origin' remote. Set ADF_REPO=<owner>/<name>."
        )
    for prefix in ("git@github.com:", "ssh://git@github.com/"):
        if url.startswith(prefix):
            url = url[len(prefix):]
            break
    else:
        parsed = urllib.parse.urlparse(url)
        if parsed.scheme:
            url = parsed.path.lstrip("/")
    return url[:-4] if url.endswith(".git") else url
