#!/usr/bin/env python3
"""Measure and prune Go cache entries for the cache experiment."""

import argparse
import json
import os
from pathlib import Path
import shutil
import time


def entries(root):
    for bucket in root.iterdir():
        if not bucket.is_dir() or len(bucket.name) != 2:
            continue
        for path in bucket.iterdir():
            if len(path.name) == 66 and path.name.endswith(("-a", "-d")):
                yield path


def size(path):
    if path.is_dir():
        return sum(child.stat().st_size for child in path.iterdir())
    return path.stat().st_size


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["stats", "mark", "prune"])
    parser.add_argument("root", type=Path)
    parser.add_argument("--marker", type=Path)
    args = parser.parse_args()
    if args.mode == "mark":
        # Go refreshes last-use mtimes at most hourly; force cache hits to refresh.
        cold = time.time() - 2 * 60 * 60
        for path in entries(args.root):
            os.utime(path, (cold, cold))
        args.marker.touch()
    elif args.mode == "prune":
        cutoff = args.marker.stat().st_mtime_ns
        for path in entries(args.root):
            if path.stat().st_mtime_ns < cutoff:
                if path.is_dir():
                    shutil.rmtree(path)
                else:
                    path.unlink()
    else:
        totals = {}
        largest = []
        for path in entries(args.root):
            kind = "executable" if path.is_dir() else path.name[-1]
            count, total = totals.get(kind, (0, 0))
            n = size(path)
            totals[kind] = (count + 1, total + n)
            largest.append((n, str(path.relative_to(args.root))))
        print(json.dumps({"entries": totals, "largest": sorted(largest, reverse=True)[:20]}, indent=2))


if __name__ == "__main__":
    main()
