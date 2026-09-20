---
name: darling-app-crash
description: What to do when a crash turns out to be a macOS app running under Darling - classify the
  failure before assuming a missing framework, find the component that owns it under ~/src, and land
  the fix as a PR against the VibeDarling fork. Invoke when a coredump's executable is Darling's mldr,
  or when diagnosing why a macOS app fails under Darling.
---

# Crashes in macOS apps running under Darling

The `diagnose-crash` skill establishes what crashed and why. This one picks up where it ends for one
specific case: the crashing process is Darling's Mach-O loader running a macOS program. Those are not
upstream Arch or Omarchy bugs and they are not reported anywhere. They are fixed here, in the Darling
sources under `~/src`, and shipped as PRs against the **VibeDarling** fork.

## Recognising one

`coredumpctl info` shows a process named `mldr` whose executable is
`/usr/local/libexec/darling/usr/libexec/darling/mldr`, while **Command Line** shows a macOS path
(`/Applications/Weather.app/Contents/MacOS/Weather`). The command line is the guest program; `mldr`
is only the loader that hosts it.

`mldr` rewrites its own cmdline to the guest program name. **`ps aux | grep mldr` is worthless** - it
finds zero real instances and matches other agents' prompt text instead. Match on `ps -eo comm` or
`/proc/<pid>/exe`.

## Read the core before designing an experiment

**A guest command that exits with 128+N died of signal N, and there is almost certainly a core for
it already.** Check that before building a reproduction. This machine has accumulated 97 SIGTRAP
cores recorded automatically; a fleet here ran matched pairs, alternated arms and bisected a package
manager to find something the crash log had held from the start.

Read three fields, **in this order**. The order matters, because no one of them is sufficient:

1. **Signal number.** SIGABRT and SIGTRAP are different populations here; do not pool them.
2. **`si_code`.** `SI_USER` means the signal arrived through `kill()` or `raise()`. Anything else
   means the process trapped itself: a breakpoint, `__builtin_trap`, a pointer-authentication
   failure, a hardware fault. So a SIGTRAP with `SI_USER` is not a trap instruction, however much it
   looks like one. **But `si_code` alone does not identify the culprit**, and treating it as decisive
   merges two unrelated clusters: dyld's `abort_with_payload` also goes through `sys_kill`, so a
   plain missing-framework self-abort is `SI_USER` too. Verified here on both populations.
3. **Sibling timestamps and command lines.** This is the field that actually separates them. One
   process dying alone is a process killing itself, such as dyld aborting on a missing dylib.
   *Several* processes dying within a second or two sharing *one* command line is a process-group
   kill, because `kill(0, sig)` targets the caller's whole group and takes down the shell, the
   program and every child at once, silently.

**Command Line** also gives you the guest program even though `EXE` is always `mldr`.

A launcher dying alongside its child is itself a signal. `NSTask` spawns with
`posix_spawnattr_setpgroup(&attrs, 0)` and `POSIX_SPAWN_SETPGROUP` when `startsNewProcessGroup` is
true, which is the default, so a launched app that hits a group kill takes down its own group and
not the viewer that launched it. If the launcher *does* die in the same second, someone passed
`setStartsNewProcessGroup:NO` - which tells the two cases apart from `coredumpctl` alone.

There is an open instance of exactly this shape on this machine: guest commands intermittently die
with 133 (`128 + SIGTRAP`), several processes of one invocation at a time. What is established is
the shape, not the culprit. It happens per invocation rather than per boot, demonstrated twice
independently, and one container yields interleaved outcomes: trap, success, and non-completion.

> **SUPERSEDED: do not act on this, it is recorded so the disproof travels with the claim.** This
> was attributed to `sigexc_setup()`, which runs under `VARIANT_DYLD` at the start of every guest
> process and calls `sys_kill(0, SIGTRAP, 0)` on believing itself traced
> (`src/external/xnu/.../signal/sigexc.c:146-148`). **That branch never fires.** Re-run at
> `DSERVER_LOG_LEVEL=info`, a 25 MB darlingserver log across ten invocations carried 1090 `sigexc:`
> lines from that exact file, proving `kern_printf` there was being captured, and *zero* `already
> traced` lines, the line that would print immediately before that `sys_kill`. The attribution came
> from grepping guest-side xnu, finding the only `kill(0, SIGTRAP)` there, and reporting "only sender
> in this subtree" as "the sender". The subtree was not the search space: it never covered the
> launcher, shellspawn, launchd, or darlingserver's own signal delivery.

Two lessons generalise past this bug. **Establishing that a call site *could* produce a symptom is
not evidence that it *did***: check the scope of your search before calling a mechanism found.
And the `DSERVER_LOG_LEVEL=info` point above is what made the disproof possible: running at info
first is what let a *missing* log line count as evidence instead of an artifact.

The current lead, which points outside guest code entirely: `darlingserver.cpp` detaches launchd
into its own session because "on ARM64 we observed launchd's startup broadcasting SIGTRAP, killing
the parents". That `setsid()` protects darlingserver's own parents and does nothing for processes
*inside* the container. Consistent with it, `sigexc: emulating default signal effects` appears in
that log, which is Darling processing a *delivered* signal's default action: the victims are
receiving the SIGTRAP, not raising it.

On absent log lines: `kern_printf` logs at info while darlingserver's default cutoff is Error, so a
missing line is an artifact until you have re-run with `DSERVER_LOG_LEVEL=info`. Do not treat its
absence as evidence.

## Classify before fixing

Four failure classes reach `SIGABRT` through completely different mechanisms. Writing a framework
stub for a crash from class 2 or 3 wastes a day. Work down this list.

### 1. Missing dylib (dyld aborts during load)

dyld calls `abort_with_payload` before a single dependent library is mapped. The payload string
survives in the core:

```bash
# the core must be fully stored before it means anything
# -1 pins this to the newest matching record, the one `dump` will pick
coredumpctl info -1 <pid> | grep -E '^\s+Storage:' | grep -q '(present)$' || exit 1

core=$(mktemp -t crash-XXXXXX.core)
trap 'rm -f "$core"' EXIT
coredumpctl dump <pid> --output="$core"
file -b "$core" | grep -q 'ELF.*core file' || exit 1

strings "$core" | grep -E 'Library not loaded|Referenced from|Reason:|shared cache'
```

Both guards matter, and they guard the same mistake. `Storage:` prints a state in parentheses, and
anything other than `(present)` means the bytes you want may not be there: on a crash you were just
notified about, `systemd-coredump` may still be writing, and `(truncated)` means the core exceeded
its size limit and was not stored in its entirety. That limit is reachable here - Darling cores of
688 MB have been seen against a 1 GB default, with `/etc/systemd/coredump.conf` overriding nothing.
A truncated core loses its tail, which is exactly where the dyld payload sits.

**Use the `mktemp` file, never `coredumpctl dump --output=-`.** Piping to stdout does not error, it
silently truncates: on a 3,457,024-byte core it emitted 1,664 bytes, exit 0, nothing on stderr. The
dyld payload string sits past that cut, so `... --output=- | strings | grep 'Library not loaded'`
returns nothing and reads exactly like an app with no missing library. That command circulates here;
it is wrong, and it fails in the direction that produces a confident false negative.

Note the shared failure mode across all three: a stream-truncated core, a size-truncated core and a
still-being-written core all yield an empty `grep` and a clean exit. **An empty result is never
evidence of "no missing library"** unless both guards above passed. Re-read the core, do not
conclude.

Confirm with the memory map: if the only mapped images are `mldr`, host `libc`/`ld-linux`, Darling's
`dyld` and the guest executable, nothing was loaded and this is class 1.

```bash
gdb -q /usr/local/libexec/darling/usr/libexec/darling/mldr "$core" \
  -batch -ex 'set debuginfod enabled off' -ex 'info proc mappings'
```

**The first missing name is rarely the whole story.** dyld reports the first unresolved dependency
and stops. Before scoping any work, get the full gap for that bundle:

```bash
llvm-objdump --macho --dylibs-used <app>/Contents/MacOS/<binary>
```

Apple's own apps are typically missing dozens of their direct dependencies. A stub for the one name
dyld happened to print usually just moves the error to the next name. Count first, then decide
whether the app is worth it - apps missing only a handful of direct dependencies are the ones where a
stub actually produces a launch.

### 2. Shared-cache-only frameworks (class 1 that cannot be fixed by copying)

If the error says `dyld: No shared cache present`, note what that implies. Since Big Sur, most Apple
system frameworks exist **only inside the dyld shared cache** and have no file on disk at all, so
they cannot be copied off a Mac no matter how complete the prefix looks. They have to be stubbed,
reimplemented, or reached through the cache itself.

`~/src/macos-frameworks` is the standing experiment for that third option: run Apple's real
`/usr/lib/dyld` against the extracted cache with Darling as the XNU emulator underneath. Read
`patches/README.md` and `patches/README-apple-dyld-trial.md` there before proposing a large stubbing
campaign - it reached phase 1 already and the measured gap is 178 BSD syscalls and 10 Mach traps, not
an unbounded amount of work.

### 3. Mac Catalyst apps

A missing image under `/System/iOSSupport/System/Library/...` means the app is Catalyst and wants a
UIKit substrate, not an AppKit one. `add_framework()` accepts an `IOSSUPPORT` flag
(`cmake/darling_framework.cmake`), but no framework in the tree passes it, there is no UIKit
anywhere, and prefixes have no `/System/iOSSupport` directory. Report these as a separate class;
do not fold them in with the AppKit apps.

Note the trap: the framework may already exist under `/System/Library/PrivateFrameworks`. Catalyst
apps will still not find it, because they look under the iOSSupport prefix.

### 4. Not a missing framework at all

Rule these out before writing any stub:

- **Page-size mismatch.** Darling has reported `hw.pagesize` as 4096 on hosts using 16K pages, so
  anything aligning `mmap`/`mprotect` to the reported size gets `EINVAL`. Signature: `EINVAL`, a
  silent non-zero exit, or an abort during early allocation rather than at symbol lookup. Check
  `getconf PAGESIZE` on the host against what the guest reports before blaming a library.
- **A real bug in Darling's own code.** An app that maps AppKit, Foundation and friends and *then*
  dies (uncaught `NSException`, assertion, segfault in a Darling frame) is a defect in the
  component that crashed. That is the most valuable class of all - it is a concrete, fixable bug with
  a stack trace, and it lands as a normal PR.
- **Apps with no missing direct dependencies that still fail.** The cause is transitive or runtime.
  Chase it; it is usually more valuable than another stub.

## Where the fix goes

Darling is a superproject of per-component submodules. Find the component that owns the crashing
code, then let git tell you the repo rather than guessing:

```bash
git -C ~/src/darling/src/external/<component> remote get-url origin   # VibeDarling/<repo>
git -C ~/src/darling/src/external/<component> remote get-url fork     # cristim/<repo>
```

**One exception, and it has bitten before:** in the *superproject* (`~/src/darling`) `origin` is
`darlinghq/darling`, not VibeDarling. Do not read that remote and conclude the PR goes upstream: a
darlinghq PR opened that way had to be closed and redone. Superproject PRs go to
`VibeDarling/darling` like everything else.

Rough routing:

| Crashing in | Component | Repo |
|---|---|---|
| AppKit, Cocoa, CoreGraphics, CoreText, QuartzCore, CoreData, Onyx2D | `src/external/cocotron` | `VibeDarling/darling-cocotron` |
| Foundation (`NS*`) | `src/external/foundation` | `VibeDarling/darling-foundation` |
| CoreFoundation (`CF*`) | `src/external/corefoundation` | `VibeDarling/darling-corefoundation` |
| syscalls, sysctl, Mach traps | `src/external/xnu` | `VibeDarling/darling-xnu` |
| dyld / image loading | `src/external/dyld` | `VibeDarling/darling-dyld` |
| the server, process lifecycle | `src/external/darlingserver` | `VibeDarling/darlingserver` |
| Objective-C runtime | `src/external/objc4` | `VibeDarling/darling-objc4` |
| new/changed framework stubs, CMake, `src/frameworks`, `src/private-frameworks` | the superproject itself | `VibeDarling/darling` |

Cocotron supplies the whole AppKit layer, so "is there OSS we can reuse" is already answered yes
there. Foundation is a separate component (Apportable-derived; its README corrects the stale GitHub
claim that it tracks gnustep). Apple's *private* frameworks have no OSS equivalents; those are
stub-or-reimplement.

### Stubbing a private framework

Do not hand-write stubs before checking `~/src/darling/tools/darling-stub-gen`. It takes a real
Mach-O and emits a complete `CMakeLists.txt`, headers and forwarding implementations
(`nm -Ug` for C symbols, `class-dump` for Objective-C). It needs the genuine binary as input.

Check it can actually run before planning around it. Its `class-dump` path is hardcoded near the top
of the script to a macOS-shaped `/Users/<user>/bin/class-dump`, and no `class-dump` is installed on
this machine at all, so the Objective-C half of the generator is currently unusable here. The C
symbol half still works. Until a `class-dump` build exists, a stub's class and selector list has to
come from one of the sources above rather than from the generator.

Where the symbol list comes from is worth a moment's thought, because `darling-stub-gen` reads
Apple's shipped framework binary directly. Two lower-friction sources give the same
symbol-and-selector surface: the `.tbd` text stubs in an installed Xcode SDK, which Apple ships
specifically for third-party linking, and `nm -u` on the consuming app itself, which reveals only
what that app actually calls and touches no Apple framework binary. The latter also tends to produce
a smaller, more honest stub. Stub *implementations* are original either way. This is Darling's
established practice rather than a settled question, so raise it with the user before a large
generation run rather than deciding it silently.

A stub is three files plus one line of registration:

```
src/private-frameworks/<Name>/CMakeLists.txt          remove_sdk_framework -> generate_sdk_framework -> add_framework
src/private-frameworks/<Name>/include/<Name>/<Name>.h
src/private-frameworks/<Name>/src/<Name>.m
```

then one `add_subdirectory(<Name>)` in `src/private-frameworks/CMakeLists.txt`, inside the
`COMPONENT_*` block that matches (plain stubs go in `COMPONENT_gui_stubs`). Copy the shape from an
existing small one such as `RecapPerformanceTesting`, and read a couple of merged stub PRs on
`VibeDarling/darling` for the expected form.

## Before touching anything

This machine runs many concurrent sessions over these trees. Invoke the `multi-agent-comms` skill and
check ownership first. Standing hazards:

- **Never write to `~/src/darling` itself.** It is the shared checkout, usually on `local/dev` with a
  large uncommitted working set that belongs to other sessions, and sometimes with broken submodule
  gitdirs. Read it freely; branch a worktree off the VibeDarling base for any edit, following the
  `~/src/darling-<topic>` convention the repo already uses everywhere.
- **Never use bare `git stash` / `git stash pop`.** The stash stack is shared across every worktree in
  the repo, so a pop can silently take another session's work.
- **Never write into a live prefix** such as `~/.darling-apps`, and never run `darling shutdown` or
  kill `darlingserver`/`mldr`. The user's real desktop session runs there. Free lock files are **not**
  evidence the runtime is idle - sessions outside the lock protocol run containers too. Check for a
  live `darlingserver` at the moment of use, not once at the start.
- **Announce deliberate coredumps.** Trap-patching a guest binary to get a backtrace produces `mldr`
  `SIGABRT` dumps and can trigger the desktop crash notifier. Say which dumps are yours so nobody
  diagnoses them as real crashes.

## The loop

1. Classify per the four classes above. Classes 2 and 3 stop here: say so and report, because a
   shared-cache-only framework and a missing UIKit substrate are research, not a PR. Everything
   else continues, including an app with no missing direct dependencies - that one is a class-4
   diagnosis (transitive or runtime), and it lands as a normal PR once the cause is found.
2. Identify the owning component and resolve its repo from `git remote`.
3. Worktree off the VibeDarling base, named `~/src/darling-<topic>` (or
   `~/src/<component>-pr-<topic>` for a submodule).
4. Fix at root cause. Add a regression test where the component has a suite; where it does not, the
   evidence is the app getting further than it did, captured concretely.
5. Verify by rerunning the actual failing app and showing the new outcome. A rebuild that compiles is
   not verification. Respect the live-prefix rules above when doing it.
6. Review the diff per CLAUDE.md §1c, commit atomically, push to the `fork` remote.
7. Open the PR, putting any labels on the creation call itself rather than a follow-up `gh pr edit`
   (CLAUDE.md §2). Check what the target repo actually defines first - `gh pr create --label` fails
   on a label the repo does not have:

   Intersect rather than assume. The closing issue often lives in a *different* repo from the PR
   (a Darling crash is frequently tracked on the superproject while the fix lands in a submodule),
   and its labels need not exist in the target. Pass only the labels both sides have:

   ```bash
   comm -12 \
     <(gh issue view <n> --repo <issue-repo> --json labels -q '.labels[].name' | sort) \
     <(gh label list --repo VibeDarling/<repo> --limit 100 | cut -f1 | sort) \
     | paste -sd,
   ```

   Empty output means pass no `--label` at all, not that something went wrong. The VibeDarling repos
   currently carry only GitHub's default label set, with no `type/*`, `severity/*`, `urgency/*`,
   `impact/*`, `effort/*` or `priority/*`, and their merged PRs are unlabelled, so today that
   intersection is usually empty. The full `triage-labels` rubric applies once a repo defines those
   labels. Then report the link.

Upstream `darlinghq` PRs are **not** opened from this loop. Fixes live in the VibeDarling fork unless
the user asks for an upstream submission; it is fine to note that upstream is still affected.

## What not to claim

Getting an app past its first missing library is not the same as making it run. Say which of the
four classes the crash was, how many dependencies the bundle is still missing, and what the app
actually did on the retry. "Stub added, builds clean" is not a working app.
