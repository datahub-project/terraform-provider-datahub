# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
"""
Emit synthetic assertion run results for the ISO 20022 demo.

The data-quality assertions created by assertions.tf are profile-backed
(volume/field), schema-metadata-backed (schema), audit-log-backed
(freshness), or PASSIVE (sql). On the synthetic demo datasets none of them
have any evaluation data, so none have ever run - which means they do not
appear on the DataHub Observe "by assertions" summary page (that view lists
assertions with run history).

This script writes assertionRunEvent timeseries aspects directly, giving
every assertion a short evaluation history so the Observe view lights up.
Almost all assertions report SUCCESS; a small, deliberate set reports a
recent regression to FAILURE so the demo can speak to incident detection:

  - VOLUME    on camt.053 (bank-to-customer statement) - volume dropped
  - FRESHNESS on sese.023 (securities settlement instruction) - feed stalled
  - SQL       on pain.001 (customer credit transfer initiation) - null msg ids

Requires env vars:
    DATAHUB_GMS_URL    e.g. https://demo.gcp.acryl.io
    DATAHUB_GMS_TOKEN  DataHub access token

Usage:
    python3 scripts/iso20022/emit_assertion_results.py [--dry-run] [--history N]
"""

import argparse
import concurrent.futures
import json
import os
import sys
import threading
import time
import urllib.request

try:
    from datahub.emitter.rest_emitter import DatahubRestEmitter
    from datahub.emitter.mcp import MetadataChangeProposalWrapper
    from datahub.metadata.schema_classes import (
        AssertionResultClass,
        AssertionResultTypeClass,
        AssertionRunEventClass,
        AssertionRunStatusClass,
    )
except ImportError:
    print("ERROR: acryl-datahub not installed. Run: pip install 'acryl-datahub>=0.14'")
    sys.exit(1)

CONFIG_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "assertions_config.json"
)
DAY_MS = 24 * 60 * 60 * 1000

# (table key, assertion type) pairs that should report a recent FAILURE.
# Everything else reports SUCCESS across the whole history.
FAILURES = {
    ("camt-053", "VOLUME"),
    ("sese-023", "FRESHNESS"),
    ("pain-001", "SQL"),
}

# DataHub assertion-info type -> a plausible "actual value" generator.
# Returns (passing_value, failing_value) as strings for actualAggValue.
def _values(atype: str, table: dict):
    vol = (table.get("volume") or {}).get("min_rows") or 1000
    if atype == "VOLUME":
        return str(int(vol * 1.18)), str(int(vol * 0.41))
    if atype == "SQL":
        return "0", "37"
    if atype == "FIELD":
        return "0", "12"
    # DATA_SCHEMA / FRESHNESS have no aggregate value
    return None, None


def fetch_assertions(gms_url: str, token: str, dataset_urn: str) -> list:
    """Return [(assertion_urn, type)] for a dataset via GraphQL."""
    q = ("query a($urn:String!){ dataset(urn:$urn){ assertions(start:0,count:100)"
         "{ assertions{ urn info{ type } } } } }")
    body = json.dumps({"query": q, "variables": {"urn": dataset_urn}}).encode()
    req = urllib.request.Request(
        f"{gms_url}/api/graphql", data=body,
        headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=30) as r:
        data = json.load(r)
    ds = (data.get("data") or {}).get("dataset") or {}
    out = []
    for a in ((ds.get("assertions") or {}).get("assertions") or []):
        out.append((a["urn"], (a.get("info") or {}).get("type")))
    return out


def build_events(assertion_urn, atype, dataset_urn, table, history, now_ms, fail):
    """Build a list of AssertionRunEvent MCPs forming an evaluation history."""
    pass_val, fail_val = _values(atype, table)
    events = []
    for i in range(history):
        # i=0 is the oldest, i=history-1 is the most recent.
        ts = now_ms - (history - 1 - i) * DAY_MS
        age = history - 1 - i  # days ago
        # Failing assertions regressed: the two most recent runs fail.
        is_fail = fail and age <= 1
        rtype = AssertionResultTypeClass.FAILURE if is_fail else AssertionResultTypeClass.SUCCESS
        val = fail_val if is_fail else pass_val
        result = AssertionResultClass(
            type=rtype,
            actualAggValue=float(val) if val is not None else None,
        )
        events.append(
            MetadataChangeProposalWrapper(
                entityUrn=assertion_urn,
                aspect=AssertionRunEventClass(
                    timestampMillis=ts,
                    runId=f"tf-demo-{age}d",
                    asserteeUrn=dataset_urn,
                    status=AssertionRunStatusClass.COMPLETE,
                    assertionUrn=assertion_urn,
                    result=result,
                ),
            )
        )
    return events


def main(dry_run: bool = False, history: int = 10, workers: int = 16) -> None:
    gms_url = os.environ.get("DATAHUB_GMS_URL", "").rstrip("/")
    token = os.environ.get("DATAHUB_GMS_TOKEN", "")
    if not gms_url and not dry_run:
        print("ERROR: DATAHUB_GMS_URL is not set.")
        sys.exit(1)

    with open(CONFIG_PATH) as fh:
        tables = json.load(fh)["tables"]
    by_urn = {t["urn"]: t for t in tables}

    # Gather all assertions across the 26 datasets (serial GraphQL reads).
    # Each configured failure pair fails exactly ONE assertion (the first
    # encountered), so a table with two SQL rules does not fail both.
    print(f"Fetching assertions for {len(tables)} datasets...")
    work = []  # (assertion_urn, type, dataset_urn, table, fail)
    fail_hits = []
    used_failures = set()
    for t in tables:
        for aurn, atype in fetch_assertions(gms_url, token, t["urn"]):
            pair = (t["key"], atype)
            fail = pair in FAILURES and pair not in used_failures
            if fail:
                used_failures.add(pair)
                fail_hits.append((t["key"], atype, aurn))
            work.append((aurn, atype, t["urn"], t, fail))

    print(f"Found {len(work)} assertions; {len(fail_hits)} will report a recent FAILURE:")
    for k, atype, aurn in fail_hits:
        print(f"  FAIL  {k:10} {atype}")
    missing = FAILURES - {(k, a) for k, a, _ in fail_hits}
    if missing:
        print(f"  WARNING: configured failures not found (skipped): {sorted(missing)}")

    now_ms = int(time.time() * 1000)
    mcps = []
    for aurn, atype, durn, table, fail in work:
        mcps.extend(build_events(aurn, atype, durn, table, history, now_ms, fail))

    print(f"Emitting {len(mcps)} run events ({history} per assertion)...")
    if dry_run:
        print("[dry-run] no events emitted.")
        return

    lock = threading.Lock()
    done = [0]
    failed = [0]
    total = len(mcps)
    _tl = threading.local()

    def emitter():
        if not hasattr(_tl, "e"):
            _tl.e = DatahubRestEmitter(
                gms_server=gms_url, token=token or None,
                timeout_sec=120, retry_max_times=5,
            )
        return _tl.e

    def _one(mcp):
        # Tolerate transient failures against a slow instance: retry a few
        # times, then give up on this single event without aborting the run.
        for attempt in range(4):
            try:
                emitter().emit_mcp(mcp)
                break
            except Exception:
                if attempt == 3:
                    with lock:
                        failed[0] += 1
                    return
                time.sleep(2)
        with lock:
            done[0] += 1
            if done[0] % 200 == 0 or done[0] == total:
                print(f"  {done[0]}/{total} events emitted ({failed[0]} failed)", flush=True)

    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        list(pool.map(_one, mcps))

    print(f"\nDone: {done[0]} run events emitted, {failed[0]} failed, across {len(work)} assertions.", flush=True)


if __name__ == "__main__":
    p = argparse.ArgumentParser(description="Emit synthetic assertion run results.")
    p.add_argument("--dry-run", action="store_true", help="Do not contact DataHub.")
    p.add_argument("--history", type=int, default=10, help="Run events per assertion (default 10).")
    p.add_argument("--workers", type=int, default=16, help="Parallel emitter threads.")
    args = p.parse_args()
    main(dry_run=args.dry_run, history=args.history, workers=args.workers)
