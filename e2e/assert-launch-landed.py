#!/usr/bin/env python3
"""Assert that an uploaded launch actually reached the project the badge reads.

`qf collect` exiting 0 means the server returned 2xx. It does NOT mean a launch
appeared under the project slug this repo's README links to -- those are two
different things, and they diverged once already: qualflare-jest uploaded 11
consecutive green runs, each reporting "OK Test results collected successfully",
while its badge said "no runs" the whole time. Nothing in CI noticed, because the
CLI's exit code was the only success signal and the CLI was telling the truth.

So this reads the same source the badge and banner derive from -- the public
report page for the slug -- and requires the run count to go UP across the
upload. That is the property the README actually claims.

Usage:
    assert-launch-landed.py <slug> --print          # before the upload
    assert-launch-landed.py <slug> --before <n>     # after the upload

Deliberately fails OPEN when the count cannot be read at all (page unreachable,
markup changed): an unverifiable check must not turn a good run red. It fails
CLOSED only when the count is readable and did not move, which is the exact
shape of the bug it exists to catch.
"""

import os
import re
import sys
import time
import urllib.error
import urllib.request

PAGE = "https://reports.qualflare.com/p/{slug}/launches"
# A browser UA: the host serves a bot challenge otherwise, and a challenge page
# parses as "no runs" -- which would be a false failure, the worst outcome here.
UA = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)
DESCRIPTION = re.compile(r'<meta\s+name="description"\s+content="([^"]*)"')
RUNS = re.compile(r"(\d+)\s+runs?\b")

# Overridable so the script's own failure path can be exercised without waiting
# out the real budget -- see the negative control in the commit that added this.
TIMEOUT_S = int(os.environ.get("LAUNCH_ASSERT_TIMEOUT", "180"))
INTERVAL_S = int(os.environ.get("LAUNCH_ASSERT_INTERVAL", "10"))


def read_count(slug):
    """Current run count for the slug, or None if it cannot be determined."""
    try:
        req = urllib.request.Request(PAGE.format(slug=slug), headers={"User-Agent": UA})
        with urllib.request.urlopen(req, timeout=20) as resp:
            html = resp.read().decode("utf8", "replace")
    except (urllib.error.URLError, TimeoutError, OSError):
        return None

    found = DESCRIPTION.search(html)
    if not found:
        return None
    text = found.group(1)
    # "No runs yet" is a real, readable zero -- not an unknown.
    if "no runs" in text.lower():
        return 0
    match = RUNS.search(text)
    return int(match.group(1)) if match else None


def main(argv):
    if len(argv) < 3:
        print(__doc__, file=sys.stderr)
        return 2
    slug = argv[1]

    if argv[2] == "--print":
        count = read_count(slug)
        print("unknown" if count is None else count)
        return 0

    if argv[2] != "--before" or len(argv) < 4:
        print(__doc__, file=sys.stderr)
        return 2

    if argv[3] == "unknown":
        print(
            f"::warning::could not read {slug}'s run count before the upload; "
            "skipping the launch-landed assertion"
        )
        return 0
    before = int(argv[3])

    deadline = time.monotonic() + TIMEOUT_S
    latest = None
    while True:
        latest = read_count(slug)
        if latest is not None and latest > before:
            print(f"launch landed: {slug} went from {before} to {latest} runs")
            return 0
        if time.monotonic() >= deadline:
            break
        time.sleep(INTERVAL_S)

    if latest is None:
        print(
            f"::warning::could not read {slug}'s run count after the upload; "
            "skipping the launch-landed assertion"
        )
        return 0

    print(
        f"::error::the upload reported success but no launch reached {slug}: "
        f"still {latest} runs after {TIMEOUT_S}s (was {before}). The report was "
        f"accepted by the server, so this is not a reporter bug -- most likely "
        f"QF_TOKEN belongs to a different project than the badge reads, or this "
        f"project's launches are not public."
    )
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
