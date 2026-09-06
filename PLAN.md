# Gate

**Buildathon 2026 · Track 2: Graph Intelligence**
Fork: `divii2205/entire-graph` · Entire mirror region: `aws-ap-south-1` (India)

---

## 1. What we are building

A command that tells you whether an AI agent's changes are safe to keep.

```
entire graph gate --base <before> --head <after>
```

It prints a verdict — **keep / continue / revert** — a short list of what a human should actually
read, and it exits with a number so CI can use it with nobody watching.

## 2. The problem

A human pull request is three files because the human got tired. An agent pull request is forty
files because the agent didn't. The reviewer has two options today:

1. Read all forty — which throws away the reason you used an agent.
2. Skim and approve — which is how bugs ship.

Git tells you which *lines* changed. It does not tell you what those lines can break, which of them
anyone verified, or which forty-first file everybody forgot.

**Gate is the third option.**

**Who it's for:** a developer, or a reviewing agent, who has just been handed a pile of AI-written
code and has to decide whether to merge it.

## 3. Why this wins — one sentence

> **Gate never asks the agent what it did.**

Every finding comes from something the agent did not write:

- the callers — written by other people, before this change existed
- git history — older than the session
- the repo's own test tree

So an agent cannot talk its way to a `keep` by describing its work nicely. Gate never reads the
description.

That is also why Gate can block a push. Anything with an AI model in the loop changes its mind
between runs, and a reviewer that changes its mind is not a gate. Run Gate twice on the same commit
and compare the output bytes — they are identical.

**The second half of the idea:** every other review tool reports what it *found*. Gate reports
**what nobody looked at** — where "nobody" means the agent, the developer, and the test suite.
Absence is the product.

> **Pitch:** Your agent wrote a 47-file PR. Gate tells you which 4 to read, and what nobody checked.

## 4. What it looks like

```
Gate verdict: CONTINUE          (47 entities changed)

REVIEW ORDER — read these 4 first
 1. VerifyToken    internal/auth/token.go:88
    14 dependents · UNCHECKED · 2 near-duplicate copies untouched
 2. parseFlags     internal/cli/root.go:729
    9 dependents · verified (root_test.go:212)
 3. Config.Load    internal/config/load.go:31
    6 dependents · UNCHECKED
 4. handleList     internal/api/list.go:44
    3 dependents · companion gap: list_test.go (14/15 past commits)

 The other 43 entities have 0 dependents and no findings.  Full list: --all

exit code 1
```

The verdict says **whether**. The review order says **what to read**. The second one is what a
reviewer actually uses — it is the real product.

## 5. The four checks

Each is a small, self-contained function.

| # | Check | The question it asks | Evidence |
|---|---|---|---|
| 1 | **Risk** | What breaks if this changed? | Who calls it, who uses its types (depth ≤ 2) |
| 2 | **Coverage** | Did anything verify this? | The repo's own test tree |
| 3 | **Companion gap** | What normally changes with this, but didn't? | Git history |
| 4 | **Clone drift** | Are there copies that didn't get the fix? | Near-duplicate code bodies |

**Checks 3 and 4 are our edge.** Everyone will build check 1 — it is the first bullet in the track
description. Almost nobody will build 3 or 4.

- **Companion gap:** *"`token.py` and `test_token.py` changed together in 14 of the last 15 commits.
  Not this time."* That finds **forgotten work**. It cannot be seen by reading the change, because
  the evidence is not in the change.
- **Clone drift:** *"You fixed the bug in one copy. Three near-duplicate copies still have it."*

## 6. The verdict rules

Printed in every run, so the tool is never a black box.

```
keep     (exit 0)  nothing risky, or it is covered
continue (exit 1)  risky but tested, OR a companion gap, OR new code with no tests
revert   (exit 2)  something removed or signature-changed has dependents AND no covering test
unusable (exit 5)  Gate ran but could not get evidence — here is the report, do not build on it
```

### The degradation rule

**A check that did not run must never count against you.**

Say the coverage check fails. Then nothing has a covering test, so every change with a dependent
would read as `revert`. That is not strictness — it is a false accusation caused by a missing input.

So each check carries a flag saying whether it actually ran, and a check that did not run can never
push the verdict *upward*:

- coverage unavailable → cap at `continue`
- risk unavailable → cap at `continue`
- both unavailable → `unusable`

**A check that did not run is reported as not-run, never as a failure.**

## 7. How the code is laid out

```
internal/gate/         types, index, risk, coverage, companions, clones, verdict, render
                       ← pure logic. NO C compiler needed to build or test this.

internal/cli/gate.go   flags, and the calls into Entire's internals
                       ← this part needs the heavy tree-sitter build.
```

**Why this split is the most important decision in the plan:** the big build compiles C parsers for
36 languages. It is slow and may not work on every laptop. This split means **two-thirds of the
product can be written and tested on any machine while that build is still unproven.**

Everything in `internal/gate/` is tested with fake, hand-written data — no graph, no network, no
compiler. Those tests run in milliseconds.

Data flows **one way only**:

```
collect → index → checks → verdict → render
```

Nothing calls backwards. That is deliberate — see the noon section.

## 8. Who builds what

Three lanes, no shared files, so nobody waits on anybody.

| Lane | Files | Needs the big build? |
|---|---|---|
| **A** | `internal/cli/gate.go`, command registration | Yes |
| **B** | `internal/gate/` — index, risk, coverage | No |
| **C** | `internal/gate/` — companions, clones, verdict, render, all tests | No |

**The unblocking rule:** Lane A commits a fake `collect` that returns hand-made sample data within
10 minutes of `types.go` landing. Then B and C never wait for the compiler, and that same fake data
becomes the test fixtures.

## 9. Build order

1. `types.go` — the shared shapes everyone codes against. Commit. **← Checkpoint 1**
2. Plumbing: `collect → index → verdict → render`, with **risk** only. A real verdict on a real
   range.
3. Add **coverage**.
4. Add **companion gap**.
5. Tests and handoff notes. Commit. **← Checkpoint 2 — the safety net**

**The plumbing is the work.** Once it exists, each extra check is cheap — a small function reading
data that is already loaded.

### What gets cut, in order

**Cut clone drift first, then coverage. Protect the companion gap.**

An earlier draft kept coverage over companions, because dropping coverage broke the verdict rule.
The degradation rule (§6) removes that problem — a missing coverage check now just caps the verdict
at `continue`. So we are free to keep the check that actually makes us different.

### Two things we do not write ourselves

- **A test resolver.** The repo already has one, covering Go, pytest, npm, cargo, maven, gradle,
  composer and ruby, and it reports its own confidence level. We consume it.
  *(Measured: the graph's own `TESTS` relation resolves only 9 links out of 11,397 symbols in this
  repo, so it is unusable here. That number is also a good honest answer for a judge.)*
- **A dependents counter.** It arrives already filled in.

## 10. Two bugs to avoid

- **Sort everything before printing.** Go deliberately randomizes map ordering. If any renderer
  loops over a map without sorting, the output changes between runs — and the "run it twice, same
  bytes" claim fails live, on the exact point the product rests on. Add a test that runs it twice
  and compares.
- **Check the test resolver works on this repo in the first ten minutes.** Coverage depends entirely
  on it, and it has not been confirmed here.

## 11. Noon — the part worth 15 points

At 12:00 a mandatory new constraint arrives. **The process is scored, not just the result.**

**Before noon:** stop adding features, get to a runnable state, tests green, commit. Confirm the
commit actually has a checkpoint. Save Gate's output on our own repo to a file — if the constraint
breaks something, that file is the demo.

**At noon — do not write code for the first 15 minutes:**

1. **Close the session completely.** Open a fresh one. Do not paste the old conversation —
   rebuild understanding from the checkpoints and the repo.
2. Say the constraint in one sentence, and name **which assumption of our design it breaks**. Write
   it down; it goes into the submission word-for-word.
3. Run impact analysis on the area we are about to touch — **before editing**. Screenshot it.
4. **Run Gate on ourselves** to see what the change would break.

Then build the smallest complete response, with tests. Commit. **← Checkpoint 3**

**Never weaken or delete an existing test to make the build pass.** That is the integrity failure
the rules explicitly call out.

**Why our shape should absorb it:** most likely constraints land in one pure layer.

| If the constraint is about... | It lands in | What we already have |
|---|---|---|
| Output format or size | `render` | review order, `--all`, byte budgets |
| Stricter or looser gating | `verdict` | pure function, printed rule, four states |
| Missing or partial evidence | `verdict` | `unusable` already exists |
| An unsupported language | `coverage` | `unchecked` is already first-class |
| A new evidence source | a new file in `internal/gate/` | checks are independent by design |

## 12. How to run

```powershell
# build — first time takes 5-15 min (compiles C parsers); fast after that
$env:CGO_ENABLED="1"; $env:CC="C:\msys64\ucrt64\bin\gcc.exe"; $env:PATH="C:\msys64\ucrt64\bin;$env:PATH"; C:\Users\user\scoop\shims\go.exe build -o entire-graph.exe ./cmd/entire-graph

# run
entire graph gate --repo . --base main --head HEAD
entire graph gate --repo . --base main --head HEAD --json
echo $?     # 0 keep · 1 continue · 2 revert · 5 unusable

# test the pure logic (no C compiler needed)
go test ./internal/gate/
```

Go 1.27.1 is installed. mise is not needed.

**If the build fails:** try `go mod download`, then one PATH/`CC` correction, then stop. Do not
debug in a loop — the pure logic in `internal/gate/` still works and can be driven from a script
reading the same data.

## 13. Limitations — stated openly, not hidden

- Dependent counts and call resolution are best-effort, not compiler-exact. Reflection, dynamic
  dispatch and generated code are invisible.
- The graph's `TESTS` relation is unusable on Go in this repo (9 links / 11,397 symbols, measured).
  Coverage uses the convention-based resolver instead, which reports its own confidence.
- A test that asserts nothing still counts as covered.
- The companion-gap threshold (70%, minimum 5 observations) is hand-tuned, not learned.
- A heavily rewritten rename may read as a delete plus an add.
- **Verdicts are advisory.** Exit codes make them enforceable, but the evidence is heuristic, and
  the tool says so in its own output.

## 14. Databricks: not opting in

It is a separate prize that adds nothing to our main score, and to qualify it would have to be
*essential* to the product. Gate's whole claim is that it is local, offline and has no model in the
loop — adding a cloud service would break the one sentence that makes it trustworthy.

A lakehouse aggregating Gate verdicts across many repos over time — *risk debt* — is a genuinely
good next step, and belongs in future work.

## 15. Rules we do not break

- Do not touch `internal/sem` or the vendored grammars.
- No network calls, ever. This project is no-egress by contract, and it is our whole thesis.
- No secrets, credentials or personal data in code, commits, prompts, docs or screenshots.
- `BUILDATHON.md` in the repo root is a required submission field with a fixed section list.

---

## HANDOFF — fill this in before noon

A fresh session with no memory of this morning depends on this section.

- **Intent:** _one paragraph on why Gate exists_
- **Architecture:** _the layers and the file implementing each_
- **Done:** _which checks actually work, and the command that proves it_
- **Not done:** _explicitly, so nobody rediscovers it at 12:30_
- **Known bugs / open risks:**
- **Commands:** build · run Gate on this repo · run tests
- **Last stable commit SHA + checkpoint ID:**
