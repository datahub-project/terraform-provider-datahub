# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
"""
Download ISO 20022 XSD message schemas from iso20022.org.

Schemas are cached to .iso-cache/xsd/ and reused if less than 30 days old.
Run with --force to bypass the cache.

Usage:
    python3 scripts/iso20022/download.py [--force]

Output:
    .iso-cache/xsd/{message-id}.xsd  -- one file per message
    .iso-cache/manifest.json         -- metadata for downstream scripts
"""

import argparse
import json
import os
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime, timezone

CACHE_DIR = ".iso-cache"
XSD_DIR = os.path.join(CACHE_DIR, "xsd")
MANIFEST_PATH = os.path.join(CACHE_DIR, "manifest.json")
CACHE_MAX_AGE_DAYS = 30

BASE_URL = "https://www.iso20022.org/message/{id}/download"
USER_AGENT = "terraform-provider-datahub/iso20022-demo (github.com/datahub-project/terraform-provider-datahub)"

# Broad representative sample: ~2 families per ISO 20022 business area.
# Format: (message_id, family, business_area, human_name, description)
MESSAGES = [
    # Payments clearing and settlement (pacs)
    (
        "pacs.008.001.10",
        "pacs",
        "payments",
        "FIToFICustomerCreditTransfer",
        "Financial institution to financial institution customer credit transfer.",
    ),
    (
        "pacs.009.001.09",
        "pacs",
        "payments",
        "FinancialInstitutionCreditTransfer",
        "Credit transfer between financial institutions.",
    ),
    (
        "pacs.002.001.12",
        "pacs",
        "payments",
        "FIToFIPaymentStatusReport",
        "Status report on a previously submitted payment instruction.",
    ),
    # Payments initiation (pain)
    (
        "pain.001.001.11",
        "pain",
        "payments",
        "CustomerCreditTransferInitiation",
        "Initiates a credit transfer from a customer to one or more creditors.",
    ),
    (
        "pain.002.001.12",
        "pain",
        "payments",
        "CustomerPaymentStatusReport",
        "Status report on a previously submitted payment initiation.",
    ),
    (
        "pain.008.001.10",
        "pain",
        "payments",
        "CustomerDirectDebitInitiation",
        "Initiates a direct debit collection from a debtor.",
    ),
    # Cash management (camt)
    (
        "camt.052.001.10",
        "camt",
        "cash_management",
        "BankToCustomerAccountReport",
        "Intraday account balance and transaction report from bank to customer.",
    ),
    (
        "camt.053.001.10",
        "camt",
        "cash_management",
        "BankToCustomerStatement",
        "End-of-day account statement from bank to customer.",
    ),
    (
        "camt.054.001.10",
        "camt",
        "cash_management",
        "BankToCustomerDebitCreditNotification",
        "Real-time debit or credit notification from bank to customer.",
    ),
    # Securities settlement (sese)
    (
        "sese.023.001.11",
        "sese",
        "securities",
        "SecuritiesSettlementTransactionInstruction",
        "Instruction to settle a securities trade.",
    ),
    (
        "sese.024.001.11",
        "sese",
        "securities",
        "SecuritiesSettlementTransactionStatusAdvice",
        "Status advice on a previously submitted settlement instruction.",
    ),
    (
        "sese.034.001.09",
        "sese",
        "securities",
        "SecuritiesSettlementAllegementNotification",
        "Notification of an unmatched settlement instruction from a counterparty.",
    ),
    # Securities management (semt)
    (
        "semt.001.001.12",
        "semt",
        "securities",
        "SecuritiesMessageCancellationAdvice",
        "Advice that a securities message has been cancelled.",
    ),
    (
        "semt.002.001.12",
        "semt",
        "securities",
        "CustodyStatementOfHoldings",
        "Statement of securities holdings held in custody.",
    ),
    # Foreign exchange (fxtr)
    (
        "fxtr.008.001.08",
        "fxtr",
        "foreign_exchange",
        "ForeignExchangeTradeInstruction",
        "Instruction to settle a foreign exchange trade.",
    ),
    (
        "fxtr.014.001.05",
        "fxtr",
        "foreign_exchange",
        "ForeignExchangeTradeStatusNotification",
        "Status notification for a foreign exchange trade.",
    ),
    # Trade finance (tsin)
    (
        "tsin.009.001.05",
        "tsin",
        "trade_finance",
        "InvoiceTaxReport",
        "Tax report associated with an invoice in a trade transaction.",
    ),
    (
        "tsin.012.001.01",
        "tsin",
        "trade_finance",
        "TradeServicesInitiation",
        "Initiates trade services for open account financing.",
    ),
]


def _cache_path(message_id: str) -> str:
    return os.path.join(XSD_DIR, f"{message_id}.xsd")


def _is_fresh(path: str) -> bool:
    if not os.path.exists(path):
        return False
    age_days = (time.time() - os.path.getmtime(path)) / 86400
    return age_days < CACHE_MAX_AGE_DAYS


def _download(message_id: str) -> bytes | None:
    url = BASE_URL.format(id=message_id)
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.read()
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            print(f"  WARNING: {message_id} not found (404) -- skipping")
            return None
        raise


def main(force: bool = False) -> None:
    os.makedirs(XSD_DIR, exist_ok=True)

    manifest = []
    ok = 0
    skipped = 0
    failed = 0

    for message_id, family, business_area, name, description in MESSAGES:
        path = _cache_path(message_id)

        if not force and _is_fresh(path):
            print(f"  CACHED  {message_id}")
            skipped += 1
            downloaded_at = datetime.fromtimestamp(
                os.path.getmtime(path), tz=timezone.utc
            ).isoformat()
        else:
            print(f"  GET     {message_id} ...", end=" ", flush=True)
            try:
                data = _download(message_id)
            except Exception as exc:
                print(f"FAILED ({exc})")
                failed += 1
                continue

            if data is None:
                failed += 1
                continue

            with open(path, "wb") as fh:
                fh.write(data)

            downloaded_at = datetime.now(tz=timezone.utc).isoformat()
            print(f"OK ({len(data):,} bytes)")
            ok += 1

        manifest.append(
            {
                "id": message_id,
                "family": family,
                "business_area": business_area,
                "name": name,
                "description": description,
                "xsd_path": path,
                "downloaded_at": downloaded_at,
            }
        )

    with open(MANIFEST_PATH, "w") as fh:
        json.dump(manifest, fh, indent=2)

    print(
        f"\nDone: {ok} downloaded, {skipped} from cache, {failed} failed."
        f"\nManifest written to {MANIFEST_PATH}"
    )
    if failed:
        sys.exit(1)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Download ISO 20022 XSD schemas.")
    parser.add_argument(
        "--force", action="store_true", help="Re-download even if cached."
    )
    args = parser.parse_args()
    main(force=args.force)
