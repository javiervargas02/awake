# ADR-0008 — Session exclusivity via an OS advisory lock

- **Status:** Accepted (ratified 2026-08-07)
- **Date:** 2026-08-07
- **Relates to:** principles 1, 3 and 4
- **Supersedes:** the exclusivity mechanism proposed in
  [ADR-0003](0003-local-first-recoverable-state.md)
- **Blocks:** M3 (file store), M6 (app service)

## Context

ADR-0002 requires that exactly one session be active at a time. ADR-0003
proposed enforcing that with the session record plus a liveness check of the
recorded PID, and explicitly deferred a real lock.

Working the mechanism out in detail (see
[State and repair](../architecture/state-and-repair.md)) exposed two holes
that documentation cannot close:

1. **A check-then-write race.** Reading `session.json`, concluding no session
   is active, and then writing a new record are three separate operations.
   Two `awake start` invocations can interleave, both conclude the coast is
   clear, and both start a session. The window is small; it is not zero, and
   "small" is not a guarantee.

2. **Deleting `~/.awake` mid-session breaks exclusivity.** The project
   actively invites users to delete that directory (principle 4). Do it while
   a session runs and the record disappears, so a concurrent `awake start`
   sees no active session and starts a second one.

There is also a pre-existing weakness worth folding in: PID-based liveness
needs `owner_started_at` to defend against PID reuse, and even then it infers
liveness rather than establishing it.

Both holes are the same shape: **exclusivity was being derived from data,
when it needs to be held as a resource.**

## Decision

**A session holds an OS advisory file lock for its entire lifetime.** The
lock, not the session record, is the exclusivity mechanism.

**The lock lives outside `~/.awake`,** in a per-user runtime directory
(`$TMPDIR/awake/session.lock` on macOS, which is already per-user and
private; falling back to a user-scoped path under the system temp directory
if unset).

**Acquisition is atomic and non-blocking.** A process either takes the lock or
learns immediately that another holds it. There is no check-then-act sequence
to race, which closes hole 1 by construction rather than by narrowing a
window.

**The kernel releases the lock when the holding process dies** — including
under `SIGKILL`, where no cleanup code runs. A stale lock is therefore not
possible, which is the property a PID file can never offer.

**Lock state is authoritative for liveness:**

| Lock | Session record | Conclusion |
| --- | --- | --- |
| acquirable | any non-terminal state | previous run died; record is stale |
| held | non-terminal | a session is genuinely running |
| held | terminal or absent | anomaly; report, do not act |

This inverts the earlier design. PID and `owner_started_at` remain in the
record as **diagnostic** information — useful for `awake stop` and for
explaining what happened — but they no longer decide whether a session is
live.

**Exclusivity is per-user, not per-machine.** The lock lives in a per-user
location, matching `~/.awake` and the fact that sessions are a user's
sessions. Two different users on one machine may each hold one.

**If the lock cannot be created, Awake degrades rather than refuses.** It logs
a warning, falls back to record-based checking, and `doctor` reports the
condition as a problem. Principle 4 prefers a working tool with a stated
weakness over a tool that will not start because of a temp directory.

## Consequences

- **Hole 2 closes.** `rm -rf ~/.awake` during a session no longer permits a
  second session: the lock is not in that directory, so it survives.
- **Stale-session detection becomes exact** rather than inferential. "Can I
  take the lock?" is a fact; "is PID 4823 still the process I think it is?" is
  an inference.
- **The `owner_started_at` mitigation stops being load-bearing.** It stays for
  diagnostics, but a platform that cannot supply it no longer weakens
  exclusivity.
- **A file exists outside `~/.awake`.** This is a genuine cost and must be
  documented in the README, because it is exactly the sort of surprise
  principle 3 forbids. It is mitigated by what the file is: zero bytes of
  content, no user data, no history, recreated per session, and cleared by
  the OS on reboot. It is runtime, not state — and placing it outside
  `~/.awake` is precisely what makes the guarantee survive that directory's
  deletion.
- **`doctor` gains a check** for the lock directory's availability, and can
  now report the lock/record anomaly in the table above.
- **Deleting the lock file itself mid-session** re-opens hole 2 in miniature:
  the holder keeps its lock on the now-unlinked file while a new process
  creates and locks a different one. Unlike `rm -rf ~/.awake`, this is not a
  path the project invites anyone down, and `doctor` can detect the resulting
  inconsistency. Accepted.
- **v0.2 detached sessions inherit a working mechanism** instead of needing
  one designed under pressure.

## Alternatives considered

**Keep the record-plus-PID check and document the races.** The prior
position. Rejected on reflection: exclusivity is a correctness property, and
"the window is only microseconds" is a probability, not a guarantee. The
`rm -rf ~/.awake` hole is worse still, because the project explicitly tells
users that deletion is safe.

**A lock file inside `~/.awake`.** Keeps everything in one directory and
closes hole 1. Rejected: it does not close hole 2, and hole 2 is the one
created by a behaviour we actively encourage.

**A PID file with `owner_started_at` verification.** Closes PID reuse, and is
what the earlier design was converging on. Rejected: it still cannot be
acquired atomically, so hole 1 survives, and it requires cleanup logic that a
`SIGKILL` skips — the kernel-released lock needs no cleanup at all.

**A POSIX named semaphore.** Lives in a kernel namespace rather than the
filesystem, so no path can be deleted out from under it. Rejected: it is
*not* released when the holder dies, so a crash leaves exclusivity
permanently taken — trading a rare race for a persistent lockout, which is a
much worse failure under principle 4.

**Binding a fixed localhost port.** Atomic, kernel-released, path-independent.
Rejected: it can collide with unrelated software, and a tool that promises
its only network activity is an update check should not be opening sockets
for bookkeeping — the optics alone violate principle 3.

**A macOS-specific kernel mechanism** (Mach service registration). Would be
the most robust option on the MVP's only platform. Deferred, not rejected:
advisory locks are well understood, portable to the platforms on the roadmap,
and easy for a contributor to audit. Revisit only if a concrete failure
appears.
