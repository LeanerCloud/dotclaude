#!/usr/bin/env python3
"""Block-level dedupe of identical files on btrfs (FIDEDUPERANGE). Nothing is moved, deleted or rewritten.

  btrfs_dedupe.py scan  ROOT... [--min-size BYTES] [--protected REGEX] --out groups.jsonl
  btrfs_dedupe.py apply groups.jsonl --fs-path PATH [--protected REGEX] [--recent-secs N]
                                     [--min-free-gib N] [--max-meta-pct N] [--log FILE]

`scan` is read-only: it groups regular files of at least --min-size bytes that have the same size, the
same first 64 KiB and the same blake2b digest, counting each inode once (hardlinks already share
storage). `apply` asks the kernel to share the extents of every group member with the first member;
the kernel compares the bytes itself and refuses on any difference, so a changed or racing file is
skipped, never corrupted. Run under `nice -n 19 ionice -c3`. Use `python3 -I`.

Size honesty: the scan reports an upper bound. Files whose extents are already shared (reflink,
earlier dedupe) are counted again, so compare `df` before and after for the real gain. Later writes
to a deduped copy cost space again (copy-on-write).
"""
import argparse, fcntl, hashlib, json, os, re, struct, subprocess, sys, time

FIDEDUPERANGE = 0xC0189436          # _IOWR(0x94, 54, struct file_dedupe_range), 24-byte header
CHUNK = 16 * 1024 * 1024            # btrfs limit per call
DEFAULT_PROTECTED = r"^/swap|\.swapfile|/\.ssh/|/\.gnupg/"


def digest(path, limit=None):
    h = hashlib.blake2b(digest_size=16)
    with open(path, "rb") as fh:
        left = limit
        while True:
            n = 1 << 22 if left is None else min(1 << 22, left)
            if n == 0:
                break
            b = fh.read(n)
            if not b:
                break
            h.update(b)
            if left is not None:
                left -= len(b)
    return h.digest()


def scan(args):
    protected = re.compile(args.protected)
    by_size, seen = {}, set()
    roots = [os.path.realpath(r) for r in args.roots]   # absolute and alias-free, so protection sees real locations
    if len({os.stat(r).st_dev for r in roots}) > 1:
        sys.exit("scan roots must be on one filesystem (the kernel cannot share extents across filesystems)")
    for root in roots:
        dev = os.stat(root).st_dev
        for cur, dirs, files in os.walk(root, followlinks=False):
            try:
                if os.stat(cur).st_dev != dev:
                    dirs[:] = []
                    continue
            except OSError:
                dirs[:] = []
                continue
            if protected.search(cur + "/"):
                dirs[:] = []
                continue
            for f in files:
                p = os.path.join(cur, f)
                if protected.search(p):
                    continue
                try:
                    st = os.lstat(p)
                except OSError:
                    continue
                if not os.path.isfile(p) or os.path.islink(p) or st.st_size < args.min_size:
                    continue
                key = (st.st_dev, st.st_ino)
                if key in seen:
                    continue
                seen.add(key)
                by_size.setdefault(st.st_size, []).append(p)
    wasted, groups = 0, 0
    with open(args.out, "w") as out:
        for size, paths in sorted(by_size.items(), reverse=True):
            if len(paths) < 2:
                continue
            heads = {}
            for p in paths:
                try:
                    heads.setdefault(digest(p, 65536), []).append(p)
                except OSError:
                    pass
            for ps in heads.values():
                if len(ps) < 2:
                    continue
                fulls = {}
                for p in ps:
                    try:
                        fulls.setdefault(digest(p), []).append(p)
                    except OSError:
                        pass
                for same in fulls.values():
                    if len(same) > 1:
                        out.write(json.dumps({"size": size, "files": same}) + "\n")
                        wasted += size * (len(same) - 1)
                        groups += 1
    print("groups=%d upper-bound-wasted=%.1f GiB (already-shared extents are counted again)" % (groups, wasted / 2**30))


def free_bytes(path):
    st = os.statvfs(path)
    return st.f_bavail * st.f_frsize


def metadata_use(path):
    """Fraction of allocated metadata chunks in use (any profile); None when it cannot be measured."""
    try:
        r = subprocess.run(["btrfs", "filesystem", "usage", "-b", path], capture_output=True, text=True)
    except OSError:
        return None
    m = re.search(r"Metadata,\w+: Size:(\d+), Used:(\d+)", r.stdout) if r.returncode == 0 else None
    return int(m.group(2)) / int(m.group(1)) if m else None


def dedupe_pair(src, dst, size):
    sfd = os.open(src, os.O_RDONLY)
    try:
        dfd = os.open(dst, os.O_RDONLY)
        try:
            off = 0
            while off < size:
                ln = min(CHUNK, size - off)
                buf = bytearray(struct.pack("=QQHHI", off, ln, 1, 0, 0) + struct.pack("=qQQiI", dfd, off, 0, 0, 0))
                fcntl.ioctl(sfd, FIDEDUPERANGE, buf, True)
                _, _, done, status, _ = struct.unpack_from("=qQQiI", buf, 24)
                if status == 1:
                    return "differs"
                if status < 0:
                    return "errno%d" % -status
                if done == 0:
                    return "zero"
                off += done
            return "ok"
        finally:
            os.close(dfd)
    finally:
        os.close(sfd)


def apply(args):
    protected = re.compile(args.protected)
    log = open(args.log, "a") if args.log else sys.stderr
    fs = args.fs_path
    now, stats = time.time(), {"groups": 0, "ok": 0, "differs": 0, "err": 0, "skipped": 0}
    groups = [json.loads(l) for l in open(args.groups)]
    groups.sort(key=lambda g: -g["size"] * (len(g["files"]) - 1))
    state = {"meta": metadata_use(fs), "n": 0}

    def limits_ok():
        """Free space every call, metadata every 50 calls; an unmeasurable metadata use stops the run."""
        state["n"] += 1
        if state["n"] % 50 == 0:
            state["meta"] = metadata_use(fs)
        if state["meta"] is None:
            log.write("STOP: metadata use cannot be measured\n")
            return False
        if free_bytes(fs) < args.min_free_gib * 2**30 or state["meta"] * 100 > args.max_meta_pct:
            log.write("STOP: free-space or metadata limit reached\n")
            return False
        return True

    stopped = False
    for i, g in enumerate(groups):
        if i % 25 == 0:
            log.write("progress %d/%d free=%.1fG %s\n" % (i, len(groups), free_bytes(fs) / 2**30, stats))
            log.flush()
        usable = []
        for p in g["files"]:
            try:
                st = os.lstat(p)
                if protected.search(p) or os.path.islink(p) or not os.path.isfile(p) or st.st_size != g["size"] \
                        or now - st.st_mtime < args.recent_secs:
                    stats["skipped"] += 1
                    continue
                usable.append(p)
            except OSError:
                stats["skipped"] += 1
        if len(usable) < 2:
            continue
        stats["groups"] += 1
        for dst in usable[1:]:
            if not limits_ok():
                stopped = True
                break
            try:
                a, b = os.lstat(usable[0]), os.lstat(dst)
                if (a.st_dev, a.st_ino) == (b.st_dev, b.st_ino):
                    continue
                r = dedupe_pair(usable[0], dst, g["size"])
            except OSError as e:
                r = "oserr%s" % e.errno
            if r == "ok":
                stats["ok"] += 1
            elif r == "differs":
                stats["differs"] += 1
            else:
                stats["err"] += 1
                log.write("%s %s\n" % (r, dst))
        if stopped:
            break
    meta = metadata_use(fs)
    log.write("DONE %s free=%.1fG meta=%s\n" % (stats, free_bytes(fs) / 2**30, "n/a" if meta is None else "%.1f%%" % (meta * 100)))
    print(json.dumps(stats))


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("scan")
    s.add_argument("roots", nargs="+")
    s.add_argument("--min-size", type=int, default=1 << 20)
    s.add_argument("--protected", default=DEFAULT_PROTECTED)
    s.add_argument("--out", required=True)
    a = sub.add_parser("apply")
    a.add_argument("groups")
    a.add_argument("--fs-path", required=True, help="any path on the filesystem being deduped (free space and metadata limits)")
    a.add_argument("--protected", default=DEFAULT_PROTECTED)
    a.add_argument("--recent-secs", type=int, default=600)
    a.add_argument("--min-free-gib", type=float, default=20)
    a.add_argument("--max-meta-pct", type=float, default=85)
    a.add_argument("--log")
    args = ap.parse_args()
    scan(args) if args.cmd == "scan" else apply(args)


if __name__ == "__main__":
    main()
