# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
"""
Obtain ISO 20022 XSD message schemas for the demo pipeline.

The iso20022.org /message/{id}/download endpoint requires a browser session
(anti-scraping protection) and cannot be fetched programmatically. Instead,
this script shallow-clones a GitHub-hosted mirror of the schemas:

    https://github.com/socrates8300/mx20022  (Apache-2.0 wrapper)

The underlying XSD content remains governed by the ISO 20022 Registration
Authority IPR policy -- see NOTICE for the attribution statement.

Clones to .iso-cache/iso20022-repo/ (gitignored), then copies the selected
XSD files to .iso-cache/xsd/ and writes .iso-cache/manifest.json.

Usage:
    python3 scripts/iso20022/download.py [--force]
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import time
from datetime import datetime, timezone

CACHE_DIR = ".iso-cache"
XSD_DIR = os.path.join(CACHE_DIR, "xsd")
REPO_DIR = os.path.join(CACHE_DIR, "iso20022-repo")
MANIFEST_PATH = os.path.join(CACHE_DIR, "manifest.json")
CACHE_MAX_AGE_DAYS = 30

REPO_URL = "https://github.com/socrates8300/mx20022.git"

# Selected messages from socrates8300/mx20022, mapped to their repo paths.
# Format: (message_id, repo_relative_path, family, business_area, name, description)
MESSAGES = [
    # Payments clearing and settlement (pacs)
    (
        "pacs.008.001.10",
        "schemas/pacs/pacs.008.001.10.xsd",
        "pacs",
        "payments",
        "FIToFICustomerCreditTransfer",
        "Financial institution to financial institution customer credit transfer.",
    ),
    (
        "pacs.009.001.10",
        "schemas/pacs/pacs.009.001.10.xsd",
        "pacs",
        "payments",
        "FinancialInstitutionCreditTransfer",
        "Credit transfer between financial institutions.",
    ),
    (
        "pacs.002.001.12",
        "schemas/pacs/pacs.002.001.12.xsd",
        "pacs",
        "payments",
        "FIToFIPaymentStatusReport",
        "Status report on a previously submitted payment instruction.",
    ),
    (
        "pacs.004.001.11",
        "schemas/pacs/pacs.004.001.11.xsd",
        "pacs",
        "payments",
        "PaymentReturn",
        "Return of a previously executed payment transfer.",
    ),
    # Payments initiation (pain)
    (
        "pain.001.001.11",
        "schemas/pain/pain.001.001.11.xsd",
        "pain",
        "payments",
        "CustomerCreditTransferInitiation",
        "Initiates a credit transfer from a customer to one or more creditors.",
    ),
    (
        "pain.002.001.13",
        "schemas/pain/pain.002.001.13.xsd",
        "pain",
        "payments",
        "CustomerPaymentStatusReport",
        "Status report on a previously submitted payment initiation.",
    ),
    (
        "pain.013.001.09",
        "schemas/pain/pain.013.001.09.xsd",
        "pain",
        "payments",
        "CreditorPaymentActivationRequest",
        "Request from a creditor to a debtor to initiate a payment.",
    ),
    # Cash management (camt)
    (
        "camt.053.001.11",
        "schemas/camt/camt.053.001.11.xsd",
        "camt",
        "cash_management",
        "BankToCustomerStatement",
        "End-of-day account statement from bank to customer.",
    ),
    (
        "camt.054.001.11",
        "schemas/camt/camt.054.001.11.xsd",
        "camt",
        "cash_management",
        "BankToCustomerDebitCreditNotification",
        "Real-time debit or credit notification from bank to customer.",
    ),
    (
        "camt.056.001.11",
        "schemas/camt/camt.056.001.11.xsd",
        "camt",
        "cash_management",
        "FIToFIPaymentCancellationRequest",
        "Request from one financial institution to another to cancel a payment.",
    ),
]


def _repo_is_fresh() -> bool:
    if not os.path.isdir(REPO_DIR):
        return False
    fetch_head = os.path.join(REPO_DIR, ".git", "FETCH_HEAD")
    if not os.path.exists(fetch_head):
        return False
    age_days = (time.time() - os.path.getmtime(fetch_head)) / 86400
    return age_days < CACHE_MAX_AGE_DAYS


def _clone_repo() -> None:
    os.makedirs(CACHE_DIR, exist_ok=True)
    print(f"  Cloning {REPO_URL} (shallow) ...")
    subprocess.run(
        ["git", "clone", "--depth", "1", REPO_URL, REPO_DIR],
        check=True,
        capture_output=True,
        text=True,
    )
    print("  Clone complete.")


def _pull_repo() -> None:
    print(f"  Pulling latest from {REPO_URL} ...")
    subprocess.run(
        ["git", "-C", REPO_DIR, "pull", "--ff-only"],
        check=True,
        capture_output=True,
        text=True,
    )
    print("  Pull complete.")


def main(force: bool = False) -> None:
    os.makedirs(XSD_DIR, exist_ok=True)

    if force and os.path.isdir(REPO_DIR):
        print(f"  Removing existing clone at {REPO_DIR}")
        shutil.rmtree(REPO_DIR)

    if not os.path.isdir(REPO_DIR):
        _clone_repo()
    elif force or not _repo_is_fresh():
        _pull_repo()
    else:
        print(f"  Reusing existing clone at {REPO_DIR} (less than {CACHE_MAX_AGE_DAYS} days old)")

    manifest = []
    ok = 0
    missing = 0
    downloaded_at = datetime.now(tz=timezone.utc).isoformat()

    for message_id, repo_path, family, business_area, name, description in MESSAGES:
        src = os.path.join(REPO_DIR, repo_path)
        dst = os.path.join(XSD_DIR, f"{message_id}.xsd")

        if not os.path.exists(src):
            print(f"  MISSING {message_id} (not in repo: {repo_path})")
            missing += 1
            continue

        shutil.copy2(src, dst)
        print(f"  OK      {message_id}")
        ok += 1

        manifest.append(
            {
                "id": message_id,
                "family": family,
                "business_area": business_area,
                "name": name,
                "description": description,
                "xsd_path": dst,
                "source": f"{REPO_URL.rstrip('.git')}/blob/main/{repo_path}",
                "downloaded_at": downloaded_at,
            }
        )

    with open(MANIFEST_PATH, "w") as fh:
        json.dump(manifest, fh, indent=2)

    print(
        f"\nDone: {ok} XSD files ready, {missing} missing."
        f"\nManifest written to {MANIFEST_PATH}"
    )
    if missing and ok == 0:
        sys.exit(1)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Obtain ISO 20022 XSD schemas via GitHub clone.")
    parser.add_argument(
        "--force", action="store_true", help="Re-clone even if already cached."
    )
    args = parser.parse_args()
    main(force=args.force)
