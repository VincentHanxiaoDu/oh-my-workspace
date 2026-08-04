import json, os, subprocess, sys, tempfile
R = "/Users/hanxiao.du/Desktop/vincent/projects/agent-dev-flow"
BASH = f"{R}/framework/.workflow/bin/check-review.sh"
PY   = f"{R}/framework/.workflow/adf/check_review.py"
tmp = tempfile.mkdtemp(); os.chdir(tmp)
def sh(*a, **k): return subprocess.run(a, capture_output=True, text=True, **k)
sh("git","init","-q","."); sh("git","config","user.email","a@b"); sh("git","config","user.name","a")
open("f","w").write("one"); sh("git","add","f"); sh("git","commit","-q","-m","chore: base\n\nAgent: dev")
BASE = sh("git","rev-parse","HEAD").stdout.strip()
open("f","w").write("two"); sh("git","commit","-q","-am","feat: work\n\nAgent: dev")
HEAD = sh("git","rev-parse","HEAD").stdout.strip()
os.makedirs(".workflow", exist_ok=True)
def v(role, sha, kind="approve", declared=None, fence=False, marker=True):
    b = f"Reviewed-by: {declared or role}\nReviewed-sha: {sha}\nVerdict: {kind}"
    if fence: b = "see:\n```\n" + b + "\n```"
    if marker: b = f"[{role}]\n" + b
    return {"body": b}
cases = [
 ("independent approve",        [v("qa",HEAD)], None),
 ("author self-approve strict", [v("dev",HEAD)], None),
 ("author self-approve allowed",[v("dev",HEAD)], "self-allowed"),
 ("changes-requested",          [v("qa",HEAD,"changes-requested")], None),
 ("stale sha (known)",          [v("qa",BASE)], None),
 ("no comments",                [], None),
 ("quoted verdict",             [v("qa",HEAD,fence=True)], None),
 ("poster/declared disagree",   [v("qa",HEAD,declared="product")], None),
 ("unplaceable sha",            [v("qa","0ddba110ddba110ddba110ddba110ddba110ddba1","changes-requested")], None),
 ("refusal then self-approve",  [v("qa",HEAD,"changes-requested"), v("dev",HEAD)], None),
 ("refuser withdraws",          [v("qa",HEAD,"changes-requested"), v("qa",HEAD)], None),
 ("unsigned verdict",           [v("qa",HEAD,marker=False)], None),
 ("two refusers",               [v("qa",HEAD,"changes-requested"), v("product",HEAD,"changes-requested")], None),
 ("approve after other refuses",[v("qa",HEAD,"changes-requested"), v("product",HEAD)], None),
 ("self-allowed but refused",   [v("qa",HEAD,"changes-requested"), v("dev",HEAD)], "self-allowed"),
]
bad = 0
for name, comments, policy in cases:
    json.dump(comments, open("c.json","w"))
    p = ".workflow/review-policy"
    if policy: open(p,"w").write(policy)
    elif os.path.exists(p): os.remove(p)
    b = sh("bash", BASH, HEAD, "c.json", BASE).returncode
    y = sh("python3", PY, HEAD, "c.json", BASE).returncode
    ok = "AGREE " if b == y else "DIFFER"
    if b != y: bad += 1
    print(f"  {ok} bash={b} py={y}  {name}")
print(f"\n{len(cases)-bad}/{len(cases)} agree")
