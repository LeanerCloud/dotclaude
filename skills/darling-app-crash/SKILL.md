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

Count siblings across a second or two, not within one exact second. One cluster here is two cores at
18:20:38 and a third at 18:20:39, so matching on an exact timestamp splits one group kill into two
events and undercounts it.

A launcher dying alongside its child is itself a signal. `NSTask` spawns with
`posix_spawnattr_setpgroup(&attrs, 0)` and `POSIX_SPAWN_SETPGROUP` when `startsNewProcessGroup` is
true, which is the default, so a launched guest app leads its own process group: a group signal
takes down that app's group and not the viewer that launched it. A launcher dying in the same second
as its child therefore means someone passed `setStartsNewProcessGroup:NO`. **This discriminates by
process-group topology, not by which call site sent the signal**, so it holds regardless of what the
sender turns out to be.

There is an open instance of exactly this shape on this machine: guest commands intermittently die
with 133 (`128 + SIGTRAP`), several processes of one invocation at a time, and one container yields
interleaved outcomes: trap, success, and non-completion. **What is established is the shape. Treat
every explanation of it as a standing model, not a finding, until one carries a falsifying test** -
two have died already, both killed by a measurement built so a negative result would mean
something, which is the only kind that has settled anything here.

The second to die is worth keeping as the worked example, because it looked quantitative. The model
was a fixed probability per guest process startup, fitted at about 4.5%, which appeared to explain
several run tallies at once. Test: 120 trivial children in **one** boot, counting failure lines
rather than a top-level exit. **Zero failures.** At p=4.5% that is roughly five expected and better
than 99% odds of at least one, so zero caps p near 2.5%, which would need 27-plus startups to
account for a single observed failure. Ruled out with it: brew, Ruby, `exec`, bootsnap, the API
path, and simply spawning many short-lived children. What survives is *which* programs get spawned.

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

Three lessons generalise past this bug. **Establishing that a call site *could* produce a symptom is
not evidence that it *did***: check the scope of your search before calling a mechanism found.
And the `DSERVER_LOG_LEVEL=info` point above is what made the disproof possible: running at info
first is what let a *missing* log line count as evidence instead of an artifact.

The third is the test to apply before writing "verified": **ask of each check, what would this show
if the answer were the opposite?** If two checks would read identically either way, they are not two
checks, they are one check counted twice, and their agreement carries no information. Several people
re-confirming the same too-narrow premise is what happened here, and it felt like corroboration
right up until a check that could actually discriminate was run.

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

The general form, which applies to anything you write that tallies problems: **a check that counts
bad things needs a "did I count anything at all" guard, because a parser matching nothing reports a
perfect score.** A dependency parser here printed `strong: 0 | weak: 0 | missing: 0`, a clean
all-clear, because one stray line put its paired-line parse permanently off by one so it matched
nothing. It was caught only because zero *weak* deps is impossible for a real binary, not because
anything complained. Assert a non-zero denominator and fail loudly when it is zero.

**And do not read an alarming output as proof the tool works.** The closure bug above is the same
class of parse artifact, but it failed the other way: it manufactured 9-14 findings per app and
nearly retracted a correct result. "It told me something bad, so it is probably working" is not
reasoning. When a tool and a direct observation disagree, the core is the observation and the
tool is the claim.

Confirm with the memory map: if the only mapped images are `mldr`, host `libc`/`ld-linux`, Darling's
`dyld` and the guest executable, nothing was loaded and this is class 1.

**Know what gdb cannot see here before you draw a conclusion from a stack.** Guest frames do not
symbolize: they render as `0x0000000305d3e614 in ?? ()` because Darling's guest dylibs ship no
symbols gdb can read, and most guest stacks stop unwinding after a frame or two with
`corrupt stack?`. So the memory map, the register state and `strings` are load-bearing here and the
backtrace mostly is not. Above all, **never conclude from a frame's absence**: a signal-handler
frame, or any other, would very likely be invisible even if present, so "no such frame in the
backtrace" is a fact about the method, not about the process. Answering that class of question
needs symbolized guest frames or the thread's saved registers.

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
  Chase it; it is usually more valuable than another stub. Measured on this prefix (2026-09-20,
  direct `LC_LOAD_DYLIB` vs `LC_LOAD_WEAK_DYLIB` only), **sixteen** apps have zero missing strong
  direct deps, Terminal and TextEdit among them. Check that list before starting a stub for an app:
  if it is on it, a stub is the wrong tool entirely. It is measured against today's binaries, so
  re-measure rather than cite it after an OS update; a framework can change tier with no signal.

  It is also direct deps only. The obvious next step, a transitive closure, was run and reported
  9-14 strong missing for *every* zero-missing app, which nearly retracted a correct finding. All
  of them were phantoms: for a fat binary `llvm-objdump --dylibs-used` prints one header line **per
  slice**, the tool stripped only the first, and the second architecture header contains `(` so it
  survived the filter and was parsed as a dependency whose name was an absolute on-disk path, which
  then had the prefix prepended a second time. Every path came out doubled and every one of those
  files exists. Terminal's real closure is satisfied, which is what its core said all along.

  Terminal is the proof this class exists and is worth more than stubbing. It is on the
  zero-missing list, it still aborted, and its 347 MB core carries **no** `Library not loaded`
  payload at all. It cleared dyld, mapped Cocotron's AppKit with the Wayland backend plus
  Foundation and CoreFoundation, spawned eight threads, and died on a *secondary* thread with an
  uncaught `NSException`: `1234 is out of bounds of array`. That string is raised by
  `darling-corefoundation`'s `NSArray.m`, so the crash names both the bug class and the repo that
  owns it. Nothing a stub does would have touched it.

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

**Pass `--arch` to every symbol count, or it is doubled.** These binaries are fat, and `llvm-nm`
without `--arch` sums *all* slices. Measured on `Weather`: 17,905 undefined symbols with no
`--arch`, against 8,961 for `arm64e` and 8,940 for `x86_64`, which add to 17,901. So check the
shape first and always state which slice a published figure is for:

```bash
llvm-lipo -archs <binary>                     # fat? which slices?
llvm-nm -u --arch arm64e <binary> | wc -l     # count one slice, never all of them
```

Treat every symbol and dependency count as **an order of magnitude plus a method, not a constant**.
Two passes over the same 66 bundles here disagreed by about 2%, and a build-edge count moved three
times in one evening. Publish the method alongside the number, say the shape is robust and the
figure is not, and re-derive before letting a decision turn on a precise value.

The same discipline applies to any claim you write down: **pin a property of your own code, not a
fact about the world.** "The installed launcher is unchanged" is a statement about the machine, and
it silently became false the moment a new runtime landed, leaving a checklist that quietly lied
rather than refusing. "Nothing in this profile modifies the installed launcher" says the useful
thing and stays true whatever is installed. Where you *do* intend an assertion to fire when the
world moves, such as a binary's hash, make it refuse loudly and re-pin deliberately with the old and
new values recorded. A provenance check that silently adopts whatever it finds is a rubber stamp.

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

**One framework per commit, per PR, and per worktree**, even when a single generator run produced
twenty of them. They are separate concerns, and a reviewer has to be able to take one and refuse
another. Batching them makes that impossible and the whole set stalls on the weakest member.

**A framework stub goes in the superproject, and no submodule hunt is needed.** Despite the ~149
submodules, `src/frameworks` and `src/private-frameworks` are plain trees, not submodules: `git
ls-tree HEAD src/` shows them as `040000 tree` where every real submodule is `160000 commit`, and
neither appears in `.gitmodules`. So fork `cristim/darling` and PR against `VibeDarling/darling`.
No pin is involved either, so the pin-bump caution below does not apply to stub PRs at all.

(It does apply when you fix a *submodule*: land the submodule change as its own PR and leave the
superproject pin bump out of it. Pin bumps are a separate argument here and several have died
unmerged; attaching one sinks the fix with it.)

Check for existing work before branching. Other sessions leave worktrees named
`~/src/darling-pr-<topic>`, and a framework you are about to stub may already have one. Prior art
worth reading for house style: `darling-pr-sck`, `darling-pr-sysadmin`, `darling-pr-dictionary`,
`darling-pr-icadevices`.

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

### Objective-C is the stub language, and that is also the limit

Write the implementation in Objective-C (`.m`), declaring the real class and protocol shapes in the
header and giving methods bodies that log and return a safe default. That is overwhelmingly the
house form: `src/private-frameworks` holds around 2,200 `.m` files against roughly two dozen `.c`,
the C ones used where a framework exports plain functions rather than classes
(`PerformanceAnalysis/src/functions.c` is the pattern). Depend on `system`, `objc` and `Foundation`.

**But an Objective-C stub can only satisfy Objective-C and C symbols, and that is not what is
actually blocking these apps.** Swift-ABI symbols are mangled `_$s...` and no `.m` file can provide
them. Measured on `Weather`'s `arm64e` slice: 8,475 of its 8,961 undefined symbols are Swift
mangled, about 95%. There are **zero** `.swift` sources anywhere in `src/private-frameworks`, so
there is no precedent in the tree for stubbing that class at all.

So the language question answers itself for the frameworks a stub can help with, and for the rest
it is the wrong question: `Combine`, `GroupActivities`, `SwiftUI` and the `libswift*` overlays that
dominate the blocked-app rankings need the Swift toolchain or hand-written mangled-symbol stubs, not
Objective-C. Establish which kind of symbol you are missing before choosing to write a stub at all.

## Before touching anything

This machine runs many concurrent sessions over these trees. Invoke the `multi-agent-comms` skill and
check ownership first. Most of what follows is a familiar rule whose *precondition* does not hold in
this repo, which is the shape worth watching for generally: check that a safety primitive is
actually isolating what you assume before you rely on it. Standing hazards:

- **Never write to `~/src/darling` itself.** It is the shared checkout, usually on `local/dev` with a
  large uncommitted working set that belongs to other sessions, and sometimes with broken submodule
  gitdirs. Read it freely; isolate every edit, following the `~/src/darling-<topic>` convention.
- **A worktree is NOT sufficient isolation here once submodules are involved.** "Use a worktree" is
  sound advice in general and its precondition fails in this repo: all 149 submodule gitdirs are
  centralized in the shared clone, so a linked worktree points at the *same* ones rather than
  getting copies. Verify it in one command - `cat src/external/AvailabilityVersions/.git` reads
  `gitdir: ../../../.git/modules/...`. Consequently `git submodule update`, `init`, `sync`, or any
  `--recurse-submodules` operation run inside a worktree **moves submodule HEADs for the shared
  clone and all 72 worktrees on it**, one of which holds another agent's only copy of uncommitted
  work. Split by what the change needs:
  - **Superproject source only**, touching no submodule (framework stubs qualify, since
    `src/frameworks` and `src/private-frameworks` are plain trees): a worktree is fine. What is
    forbidden there is entering a submodule, not touching its pointer: resolve a submodule-pointer
    conflict with `git update-index --cacheinfo 160000,<sha>,src/external/<name>`, which writes the
    superproject index alone and never enters the submodule. Never resolve one by `cd`-ing in and
    checking something out.
  - **Populated submodules, a build, or any recursive operation**: use an independent clone with
    `--reference` against the existing checkout, not a worktree. Objects are shared via alternates
    so it is nearly free in disk and network, while refs and HEAD are yours alone. The superproject
    clone takes about a second; initializing all 149 submodules with
    `submodule.alternateLocation=superproject` is a one-off of roughly twenty minutes and is the
    real setup cost.
- **Never use bare `git stash` / `git stash pop`.** The stash stack is shared across every worktree in
  the repo, so a pop can silently take another session's work.
- **"Committed to a branch in a submodule" is not the safety it sounds like, and this one destroys
  data.** Because the gitdirs are centralized, a branch created in a submodule from *any* worktree
  lives in the **shared** ref space. It survives checkouts, but it is not isolated from a branch
  deletion, a `git gc`, a `git prune` or a `git worktree prune` run in that submodule from any
  worktree or from the shared clone. Separate filesystem paths imply an isolation the refs do not
  have. Salvaged work-in-progress is sometimes the only copy of itself on one of those branches, so
  run none of those commands inside a submodule; ask first, every time.
- **The ban is blanket, not submodule-scoped: no `git gc`, `git prune`, `git repack` or
  `git worktree prune` anywhere under the shared clone or its submodules.** The reason is the
  `--reference` clone recommended above: it *borrows* objects from the parent through alternates
  rather than copying them, so a `gc --prune` in the shared clone can delete objects a reference
  clone depends on and break it. Confirm it yourself with
  `cat <clone>/.git/objects/info/alternates`, which names the parent's object store outright. The
  cost is asymmetric and worth stating plainly: such an image is roughly twenty minutes of submodule
  init plus half an hour of build, while the disk those objects occupy is not scarce. Two kinds of
  shared state now hang off that clone, refs in the centralized submodule gitdirs and objects in its
  store, and both die to routine housekeeping run in the wrong directory.

  That is a reason to *dissociate*, not to avoid `--reference`. Use it by lifetime: for a throwaway
  clone you will delete within the hour, borrow freely. For anything anyone else depends on, or
  anything expensive to rebuild, create it cheaply with `--reference` and then immediately make it
  stand alone:

  ```bash
  git -C <yourclone> repack -a -d                  # materialise every borrowed object locally
  rm <yourclone>/.git/objects/info/alternates
  git -C <yourclone> fsck --connectivity-only      # must exit 0
  ```

  Run that in **your** clone only; a `repack` under the shared Darling clone or its submodules is
  the forbidden case above. Afterwards confirm `HEAD` still matches your branch and your PR head.
- **Ask before building.** A full Darling build is a ~20 minute one-off submodule init plus 23-46
  minutes of compiling, and someone may already have a clean reference image you can be pointed at.
  Pay that cost once for the fleet rather than once per agent.
- **Never write into a live prefix** such as `~/.darling-apps`, and never run `darling shutdown` or
  kill `darlingserver`/`mldr`. The user's real desktop session runs there. Free lock files are **not**
  evidence the runtime is idle - sessions outside the lock protocol run containers too. Check for a
  live `darlingserver` at the moment of use, not once at the start.
- **Announce deliberate coredumps.** Trap-patching a guest binary to get a backtrace produces `mldr`
  `SIGABRT` dumps and can trigger the desktop crash notifier. Say which dumps are yours so nobody
  diagnoses them as real crashes.
- **Running a guest GUI app is not only a prefix concern, it touches the user's live compositor.**
  Containers inherit `WAYLAND_DISPLAY`, so a launched app attaches real Wayland clients to the
  session the user is actually working in, and clients that outlive their cleanup can leave it
  degraded. Treat "launch an app to check" as an action against the desktop, not against a sandbox.
  If a session-level incident is in progress, launching anything is exactly the wrong move; hold
  and say so rather than gathering behavioural evidence.
- **A test suite is not safe by virtue of being a test suite.** Before running any harness, check
  whether its leaf steps spawn GUI clients, read `WAYLAND_DISPLAY` or `DISPLAY`, or shell out to
  `hyprctl`, `systemctl` or a compositor tool. Suites routinely isolate `HOME` and the `XDG_*`
  config paths while deliberately leaving `WAYLAND_DISPLAY` alone, so an otherwise well-sandboxed
  run still lands on the live session. If you cannot establish that cheaply, do not run it. "It is
  just tests" is the same shape of assumption as "the lock is free, so nothing is running".

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

   **"Is it a bug" and "should it be fixed" are separate questions, and the second needs to know
   what the fix turns *on*.** A correct fix that enables a never-exercised code path can be worse
   than the benign bug it replaces. A real arithmetic bug in the guest `mremap` path was left
   deliberately unfixed here for exactly that reason: correcting it would make `mremap` succeed,
   which switches on an allocator fast path that has been dead in effect on every 16K host, and
   turning on a never-run branch inside the memory allocator, untestable locally, is the worse
   trade. That call was only available because the current failure mode had been *established*
   rather than assumed benign: `mremap` returns `EINVAL`, `realloc` falls back to alloc/copy/free,
   no bogus address reaches the caller. When a bug's present-day symptom is "a fast path silently
   does not run", establish what actually happens today, then ask what the patch activates, before
   writing it.
5. Verify by rerunning the actual failing app and showing the new outcome. A rebuild that compiles is
   not verification. But this step launches a guest process against the user's live prefix *and*
   their live compositor, so it is the one step in this loop that can damage something outside your
   worktree: observe the rules above, and if the session is under a hold, stop here, open the PR on
   the source work alone, and say in it that the behavioural evidence is outstanding.
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

**Say in the PR body what the change does *not* provide.** A stub that lets an app fail later than
it did before is genuinely useful and should be described exactly that way, never as support for
the framework. "These stubs compile and are well formed" is an honest claim. "These stubs make the
app launch" requires a launch you actually performed, and if you have not run it, say so plainly
instead of implying it.

For Apple's own bundled apps a stub demonstrably does **not** produce a launch. Calculator has
eleven further blockers after `TextInputUI`, all Swift-ABI, `SwiftUI` alone binding symbols in the
hundreds (see the per-slice caveat below before quoting a figure). So
the honest shape is **"gets further, still fails at X"**, and naming X is worth more to a reviewer
than the stub is. Date any satisfiability claim too: "satisfiable with zero bound symbols" is
measured against today's binaries, and an OS update can move a framework between tiers with no
signal at all.

**Show the code you added is reached before claiming it helps.** Compiling is not reachability. A
fix here was ranked the worst bug in a sweep and turned out to sit in a function no build variant
ever calls, because its only call site was inside an `#ifndef` whose macro is set at directory
scope. `#if` guards and CMake `add_definitions` are part of the search scope, not background
detail. The cheap check for this class is the dyld payload itself: `strings <corefile> | grep -A2
'Library not loaded'` names the missing dylib directly, and if the name you stubbed stops appearing
and a different one takes its place, the stub is demonstrably being reached.
