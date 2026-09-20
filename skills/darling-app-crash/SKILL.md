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

## Classify before fixing

Four failure classes reach `SIGABRT` through completely different mechanisms. Writing a framework
stub for a crash from class 2 or 3 wastes a day. Work down this list.

### 1. Missing dylib (dyld aborts during load)

dyld calls `abort_with_payload` before a single dependent library is mapped. The payload string
survives in the core:

```bash
core=$(mktemp -t crash-XXXXXX.core)
trap 'rm -f "$core"' EXIT
coredumpctl dump <pid> --output="$core"
strings "$core" | grep -E 'Library not loaded|Referenced from|Reason:|shared cache'
```

`coredumpctl dump --output=-` does **not** work - it needs a seekable file, so use `mktemp`.

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
(`nm -Ug` for C symbols, `class-dump` for Objective-C). Its `class-dump` path is hardcoded near the
top of the script, and it needs the genuine binary as input.

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

   ```bash
   gh label list --repo VibeDarling/<repo> --limit 100 | cut -f1
   ```

   The VibeDarling repos currently carry only GitHub's default label set, with no `type/*`,
   `severity/*`, `urgency/*`, `impact/*`, `effort/*` or `priority/*`, and their merged PRs are
   unlabelled. So there is usually nothing to mirror there, and the full `triage-labels` rubric
   applies only once a repo defines those labels. Where it does, mirror the closing issue's labels
   plus `triaged` on the create call. Then report the link.

Upstream `darlinghq` PRs are **not** opened from this loop. Fixes live in the VibeDarling fork unless
the user asks for an upstream submission; it is fine to note that upstream is still affected.

## What not to claim

Getting an app past its first missing library is not the same as making it run. Say which of the
four classes the crash was, how many dependencies the bundle is still missing, and what the app
actually did on the retry. "Stub added, builds clean" is not a working app.
