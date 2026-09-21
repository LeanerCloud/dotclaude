---
name: darling-app-crash
description: What to do when a crash turns out to be a macOS app running under Darling - classify the
  failure before assuming a missing framework, find the component that owns it under ~/src, and land
  the fix as a PR against the VibeDarling fork. Invoke when a coredump's executable is Darling's mldr,
  or when diagnosing why a macOS app fails under Darling.
---

# Crashes in macOS apps running under Darling

`diagnose-crash` establishes what crashed. This skill covers one case it hands off: the crashing
process is Darling's Mach-O loader running a macOS program. These are not Arch or Omarchy bugs and
are reported nowhere. They are fixed in the Darling sources under `~/src` and shipped as PRs against
the **VibeDarling** fork.

## Recognise it

`coredumpctl info` shows a process named `mldr` whose executable is
`/usr/local/libexec/darling/usr/libexec/darling/mldr`, while **Command Line** shows a macOS path.
The command line is the guest program; `mldr` is only the loader hosting it. `mldr` rewrites its own
cmdline, so **`ps aux | grep mldr` finds no real instances** and matches other agents' prompt text
instead: match on `ps -eo comm` or `/proc/<pid>/exe`.

A guest command exiting with 128+N died of signal N, and a core almost certainly exists already.
Find it before building a reproduction.

## Triage the core, in this order

1. **Signal.** SIGABRT and SIGTRAP are different populations here; do not pool them.
2. **`si_code`.** `SI_USER` means the signal arrived through `kill()`/`raise()`; anything else means
   the process trapped itself. **`si_code` alone does not separate the clusters** - dyld's
   `abort_with_payload` also goes through `sys_kill`, so a missing-framework self-abort is `SI_USER`
   too.
3. **Sibling timestamps and command lines**, the field that actually separates them. One process
   dying alone killed itself (dyld aborting on a missing dylib). *Several* dying within a second or
   two under one command line is a process-group kill: `kill(0, sig)` silently takes the caller's
   whole group. Count across a second or two, not one exact second.

A launcher dying alongside its child means someone passed `setStartsNewProcessGroup:NO`; `NSTask`
otherwise gives the child its own group, which a group signal does not escape.

Two resolved exit codes, both triggered by asynchronous signal delivery, in practice `SIGCHLD` from
the guest's own subprocesses:

- **133** (`128+SIGTRAP`): a **non-main** thread hits `brk #0x1` in libplatform's unfair-lock slow
  path, because the guest `__ulock_wait` stub ignores `ULF_NO_ERRNO`, so negative returns go through
  `_cerror_nocancel` and collapse to `-1`, and libplatform traps on anything but a few specific
  values. The fatal SIGTRAP on the main thread is downstream of that.
- **134** (`128+SIGABRT`): `__darling_thread_rpc_socket` (`src/startup/mldr/elfcalls/threads.c`)
  calls `abort()` when the RPC socket is gone and the caller is not the main thread; only the main
  thread blocks signals.

## Classify before fixing

Four classes reach `SIGABRT` through different mechanisms. A stub written for class 2, 3 or 4 wastes
a day.

### 1. Missing dylib - dyld aborts during load

**Both discriminators must hold: a dyld `Library not loaded` payload in the core, AND `TID == PID`
in `coredumpctl info` (dyld aborts on the main thread, before any other exists).** Either absent
means look elsewhere.

```bash
coredumpctl info -1 <pid> | grep -E '^\s+Storage:' | grep -q '(present)$' || exit 1
core=$(mktemp -t crash-XXXXXX.core); trap 'rm -f "$core"' EXIT
coredumpctl dump <pid> --output="$core"
strings "$core" | grep -E 'Library not loaded|Referenced from|Reason:|shared cache'
```

**Never pipe `coredumpctl dump --output=-`.** It truncates silently to about 2 KB at exit 0 with
nothing on stderr, and the payload sits past the cut, so the pipeline returns nothing and reads
exactly like an app with no missing library; a `(truncated)` core and a still-being-written one fail
identically. **An empty result is never evidence of "no missing library"** unless `Storage:` read
`(present)` and the dump went to a file.

Confirm with the memory map (`gdb -q <mldr> "$core" -batch -ex 'set debuginfod enabled off' -ex
'info proc mappings'`): only `mldr`, host `libc`/`ld-linux`, Darling's `dyld` and the guest
executable mapped means nothing loaded, and this is class 1. Guest frames do not symbolize and most
guest stacks stop unwinding after a frame or two, so the map, the registers and `strings` are
load-bearing and the backtrace is not. **Never conclude from a frame's absence** - it would likely
be invisible even if present.

**The first missing name is rarely the whole story**, because dyld reports the first unresolved
dependency and stops. Get the bundle's full gap with
`llvm-objdump --macho --dylibs-used <app>/Contents/MacOS/<binary>`: Apple's apps are typically
missing dozens of direct dependencies, and stubbing the one name dyld printed usually just moves the
error to the next.

### 2. Shared-cache-only frameworks

`dyld: No shared cache present`. Since Big Sur most Apple system frameworks exist **only** inside the
dyld shared cache with no file on disk, so they cannot be copied off a Mac however complete the
prefix looks. Stub, reimplement, or go through the cache - `~/src/macos-frameworks` is the standing
experiment for the third, and its `patches/README.md` comes before any stubbing campaign.

### 3. Mac Catalyst apps

A missing image under `/System/iOSSupport/System/Library/...` means the app wants a UIKit substrate.
`add_framework()`'s `IOSSUPPORT` flag is passed by nothing in the tree, there is no UIKit, and
prefixes have no `/System/iOSSupport`. Trap: the framework may already exist under
`/System/Library/PrivateFrameworks` and Catalyst apps still will not find it.

### 4. Not a missing framework at all

- **Page-size mismatch.** Darling has reported `hw.pagesize` as 4096 on 16K-page hosts, so anything
  aligning `mmap`/`mprotect` to the reported size gets `EINVAL`. Compare `getconf PAGESIZE` with
  what the guest reports before blaming a library.
- **A real bug in Darling's own code** - the most valuable class, and a normal PR. An app that maps
  AppKit and Foundation and *then* dies is a defect in the component that crashed. Terminal: zero
  missing direct deps, no `Library not loaded` payload at all, died on a secondary thread with
  `1234 is out of bounds of array`, which `darling-corefoundation`'s `NSArray.m` raises.
- **An app with no missing direct dependencies that still fails**: transitive or runtime, and worth
  chasing. Re-measure the zero-missing set rather than citing an earlier one; an OS update moves
  frameworks between tiers with no signal.

## Instrumenting guest code

**Probe with `fprintf(stderr, ...)`, never `NSLog`.** Foundation's `__NSLogCString` (`src/NSLog.m`)
emits with `printf`, so guest `NSLog` goes to *stdout*, which is fully buffered once redirected to a
file or pipe - and whatever sits in that buffer when the process aborts is lost, which is exactly
the case you are debugging. `NSLog` probes produced a retracted "loads AppKit and never draws"
report here; the draw path had been running all along. Keep the streams separate and **never merge
with `2>&1`**, or you cannot tell a discarded buffer from a probe that never ran.

A CoreFoundation line is not proof `NSLog` works: the banners match, but CF's second bracket field
is `pthread_self()` and varies per thread while `NSLog`'s is the uid, constant.

`kern_printf` logs at info while darlingserver's default cutoff is Error, so a missing line is an
artifact until you re-run at `DSERVER_LOG_LEVEL=info`. A container can be pointed at a privately
built `darlingserver` through `DARLING_LIBEXEC_PATH`, with no install.

## Where the fix goes

Darling is a superproject of per-component submodules. Find the owning component, then let git name
the repo: `git -C ~/src/darling/src/external/<component> remote get-url origin` (VibeDarling) or
`... get-url fork` (cristim). **Exception:** in the *superproject* `origin` is `darlinghq/darling`.
Do not read that and open a PR upstream - one opened that way had to be closed and redone.

| Crashing in | Component | Repo |
|---|---|---|
| AppKit, Cocoa, CoreGraphics, CoreText, QuartzCore, CoreData, Onyx2D | `src/external/cocotron` | `VibeDarling/darling-cocotron` |
| Foundation (`NS*`) | `src/external/foundation` | `VibeDarling/darling-foundation` |
| CoreFoundation (`CF*`) | `src/external/corefoundation` | `VibeDarling/darling-corefoundation` |
| syscalls, sysctl, Mach traps | `src/external/xnu` | `VibeDarling/darling-xnu` |
| dyld / image loading | `src/external/dyld` | `VibeDarling/darling-dyld` |
| the server, process lifecycle | `src/external/darlingserver` | `VibeDarling/darlingserver` |
| Objective-C runtime | `src/external/objc4` | `VibeDarling/darling-objc4` |
| framework stubs, CMake, `src/frameworks`, `src/private-frameworks` | the superproject | `VibeDarling/darling` |

Cocotron supplies the whole AppKit layer; Foundation is separate (Apportable-derived, not gnustep);
Apple's *private* frameworks have no OSS equivalent, so stub or reimplement. The `NS*` row is a
starting point, not a rule - several `NS*` classes live in `corefoundation` (`NSArray.m` among
them), so `git grep` the symbol across both before branching. When you fix a *submodule*, land it as
its own PR and leave the superproject pin bump out; pin bumps are a separate argument and several
have died unmerged.

### Stubbing a framework

**Establish which kind of symbol is missing before deciding to stub at all.** An Objective-C stub
satisfies only Objective-C and C symbols; Swift-ABI symbols are mangled `_$s...` and no `.m` file
provides them. On `Weather`'s `arm64e` slice, 8,475 of 8,961 undefined symbols are Swift, and
`src/private-frameworks` has zero `.swift` sources - `SwiftUI`, `Combine`, `GroupActivities` and the
`libswift*` overlays dominate the blocked-app rankings and are not stubbable this way.

**Pass `--arch` to every symbol count or it doubles**: these binaries are fat and `llvm-nm` sums all
slices (17,905 without, against 8,961 and 8,940 per slice). `llvm-objdump --dylibs-used` likewise
prints one header line *per slice*, so a parser stripping only the first invents phantom
dependencies.

`~/src/darling/tools/darling-stub-gen` generates stubs from a real Mach-O, but its `class-dump` path
is hardcoded to a macOS-shaped `/Users/<user>/bin/class-dump` and none is installed here, so only
its C-symbol half runs.

A stub is **3 files, 1 registration line, and 4 committed symlinks**:

```
src/private-frameworks/<Name>/CMakeLists.txt   remove_sdk_framework -> generate_sdk_framework -> add_framework
src/private-frameworks/<Name>/include/<Name>/<Name>.h
src/private-frameworks/<Name>/src/<Name>.m
  + add_subdirectory(<Name>) in src/private-frameworks/CMakeLists.txt, in the matching COMPONENT_*
    block (plain stubs: COMPONENT_gui_stubs)
  and four symlinks, paths relative to the superproject root, with
  SDK = Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk/System/Library:
framework-private-include/<Name>                 -> ../$SDK/PrivateFrameworks/<Name>.framework/Headers
$SDK/PrivateFrameworks/<Name>.framework/Headers  -> Versions/A/Headers
$SDK/PrivateFrameworks/<Name>.framework/Versions/A/Headers
                                                 -> src/private-frameworks/<Name>/include/<Name>
$SDK/PrivateFrameworks/<Name>.framework/Versions/Current -> A
```

Those four are **tracked files at mode 120000, committed, not generated by the build**; omitting
them is two failed builds. Public frameworks take the identical shape with `framework-include/` and
`$SDK/Frameworks/`. Confirm the shape against a real one before copying:
`git ls-files -s | awk '$1=="120000"' | grep GameController`.

**A stub goes in the superproject and needs no submodule hunt**: `src/frameworks` and
`src/private-frameworks` are plain trees, not submodules (`git ls-tree HEAD src/` shows `040000
tree`), so fork `cristim/darling` and PR against `VibeDarling/darling`. **One framework per commit,
per PR, and per worktree**, even when one generator run produced twenty.

## Shared-tree hazards

Many sessions run over these trees at once; invoke `multi-agent-comms` and check ownership first.

- **Never write to `~/src/darling` itself.** It is the shared checkout, usually on `local/dev` with
  other sessions' uncommitted work. Isolate every edit as `~/src/darling-<topic>`.
- **A worktree is not isolation once submodules are involved.** All 149 submodule gitdirs are
  centralized in the shared clone (`cat src/external/AvailabilityVersions/.git` shows `gitdir:
  ../../../.git/modules/...`), so `git submodule update|init|sync` or any `--recurse-submodules` run
  in a worktree moves submodule HEADs for the shared clone and every worktree on it. Superproject-
  only work, framework stubs included, is fine in a worktree: resolve a submodule pointer with
  `git update-index --cacheinfo 160000,<sha>,<path>`, never by `cd`-ing in. For populated
  submodules, a build, or anything recursive, use an independent `--reference` clone.
- **No `git gc`, `prune`, `repack`, `worktree prune`, or bare `git stash`/`stash pop` under the
  shared clone or its submodules.** The stash stack and the submodule ref space are shared across
  every worktree, and reference clones borrow objects through alternates, so each of these can
  destroy another session's only copy of something. Ask first, every time. "Do not run it in
  `~/src/darling`" is not the guard, because the path you would type is your worktree's and a
  worktree has no alternates file while sharing the object store outright:
  `git -C <path> rev-parse --git-common-dir` resolving under the shared clone is what identifies one.
- **Never write into a live prefix** such as `~/.darling-apps`, and never `darling shutdown` or kill
  `darlingserver`/`mldr`. Free lock files are not evidence the runtime is idle.
- **Launching a guest GUI app attaches real clients to the user's compositor**, since containers
  inherit `WAYLAND_DISPLAY` - test suites included, as they isolate `HOME` and `XDG_*` but not that.
  Announce deliberate coredumps, so nobody diagnoses your trap-patched binary as a real crash.
- **Ask before building**: a ~20 minute submodule init plus 23-46 minutes of compiling, and someone
  may already have a reference image.

## The loop

1. Classify. Classes 2 and 3 stop here and get reported as research, not a PR.
2. Resolve the owning component's repo from `git remote`.
3. Worktree or `--reference` clone off the VibeDarling base as `~/src/darling-<topic>` (or
   `~/src/<component>-pr-<topic>`). Check for an existing one - another session may hold it.
4. Fix at root cause, with a regression test where the component has a suite; where it does not, the
   evidence is the app getting further than it did, captured concretely.

   **A fix that switches on a genuinely never-run path can be worse than the benign bug it
   replaces**, so establish today's failure mode rather than assuming it is benign. The test is not
   "has this run *here*" but "has it run anywhere in a configuration we trust": a path dead on 16K
   hosts and live on every x86_64 install is not unexercised, and fixing it restores parity.
5. Verify by rerunning the actual failing app; a rebuild that compiles is not verification. This is
   the one step reaching outside your worktree, into the live prefix and compositor, so if the
   session is under a hold, stop here, open the PR on the source alone, and say in it that the
   behavioural evidence is outstanding.
6. Review per CLAUDE.md §1c, commit atomically, push to the `fork` remote.
7. Open the PR with any labels on the creation call itself (CLAUDE.md §2). The closing issue is
   often in a *different* repo from the PR, so pass `--repo` to `gh issue view` explicitly. Only
   labels both repos define can be applied, and VibeDarling repos carry only GitHub's default set
   today, so that intersection is usually empty: pass no `--label`.

Upstream `darlinghq` PRs are not opened from this loop unless the user asks.

## What not to claim

A stub does not make an app launch. The honest shape is **"gets further, still fails at X"**, and
naming X is worth more to a reviewer than the stub is: Calculator has eleven further blockers after
`TextInputUI`, all Swift-ABI.

**Compiling is not reachability.** Show the code is reached: if the name you stubbed stops appearing
in `strings <core> | grep -A2 'Library not loaded'` and a different one takes its place, the stub is
demonstrably being used.
