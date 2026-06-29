# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
"""
Download ISO 20022 XSD message schemas from iso20022.org.

Uses the business-area bulk download endpoint which returns a zip of all
XSD files for each message family. The per-message endpoint
(/message/{id}/download) requires a browser session; the business-area
endpoint (/business-area/{id}/download) serves zips directly.

Business area IDs were discovered by probing the catalogue at
https://www.iso20022.org/iso-20022-message-definitions.

Schemas are cached to .iso-cache/xsd/ and reused if less than 30 days old.
Run with --force to bypass the cache.

Usage:
    python3 scripts/iso20022/download.py [--force]

Output:
    .iso-cache/xsd/{message-id}.xsd  -- one file per message
    .iso-cache/manifest.json         -- metadata for downstream scripts
"""

import argparse
import io
import json
import os
import sys
import time
import urllib.request
import urllib.error
import zipfile
from datetime import datetime, timezone

CACHE_DIR = ".iso-cache"
XSD_DIR = os.path.join(CACHE_DIR, "xsd")
MANIFEST_PATH = os.path.join(CACHE_DIR, "manifest.json")
CACHE_MAX_AGE_DAYS = 30

BASE_URL = "https://www.iso20022.org/business-area/{id}/download"
USER_AGENT = "terraform-provider-datahub/iso20022-demo (github.com/datahub-project/terraform-provider-datahub)"

# Business area numeric IDs from iso20022.org, mapped to message family info.
# Each download returns a zip of all current XSD files for that family.
BUSINESS_AREAS = [
    (76,  "pacs", "payments",     "Payment Clearing and Settlement"),
    (81,  "pain", "payments",     "Payments Initiation"),
    (51,  "camt", "cash_management", "Cash Management"),
    (111, "sese", "securities",   "Securities Settlement"),
    (106, "semt", "securities",   "Securities Management"),
    (71,  "fxtr", "foreign_exchange", "Foreign Exchange Trade"),
    (121, "tsin", "trade_finance","Trade Services Initiation"),
]

# Human descriptions for well-known message types (name, description).
# Keyed by message ID prefix (e.g. "pacs.008"). Fallback is the raw filename stem.
MESSAGE_META: dict[str, tuple[str, str]] = {
    "pacs.002": ("FIToFIPaymentStatusReport", "Status report on a previously submitted payment instruction."),
    "pacs.003": ("FIToFICustomerDirectDebit", "Direct debit instruction from one financial institution to another."),
    "pacs.004": ("PaymentReturn", "Return of a previously executed payment transfer."),
    "pacs.007": ("FIToFIPaymentReversal", "Reversal of a previously executed payment."),
    "pacs.008": ("FIToFICustomerCreditTransfer", "Financial institution to financial institution customer credit transfer."),
    "pacs.009": ("FinancialInstitutionCreditTransfer", "Credit transfer between financial institutions."),
    "pacs.010": ("FinancialInstitutionDirectDebit", "Direct debit between financial institutions."),
    "pacs.028": ("FIToFIPaymentStatusRequest", "Request for status on a previously submitted payment."),
    "pacs.029": ("MultilateralSettlementRequest", "Request for multilateral settlement of net positions."),
    "pain.001": ("CustomerCreditTransferInitiation", "Initiates a credit transfer from a customer to one or more creditors."),
    "pain.002": ("CustomerPaymentStatusReport", "Status report on a previously submitted payment initiation."),
    "pain.007": ("CustomerPaymentReversal", "Reversal of a previously submitted customer payment."),
    "pain.008": ("CustomerDirectDebitInitiation", "Initiates a direct debit collection from a debtor."),
    "pain.013": ("CreditorPaymentActivationRequest", "Request from a creditor to a debtor to initiate a payment."),
    "pain.014": ("CreditorPaymentActivationRequestStatusReport", "Status of a creditor payment activation request."),
    "camt.003": ("GetAccount", "Request for account information."),
    "camt.004": ("ReturnAccount", "Account information returned in response to a query."),
    "camt.005": ("GetTransaction", "Request for transaction information."),
    "camt.052": ("BankToCustomerAccountReport", "Intraday account balance and transaction report."),
    "camt.053": ("BankToCustomerStatement", "End-of-day account statement from bank to customer."),
    "camt.054": ("BankToCustomerDebitCreditNotification", "Real-time debit or credit notification from bank to customer."),
    "camt.056": ("FIToFIPaymentCancellationRequest", "Request to cancel a previously submitted payment."),
    "camt.088": ("NetReport", "Report of net positions between financial institutions."),
    "sese.023": ("SecuritiesSettlementTransactionInstruction", "Instruction to settle a securities trade."),
    "sese.024": ("SecuritiesSettlementTransactionStatusAdvice", "Status of a securities settlement instruction."),
    "sese.034": ("SecuritiesSettlementAllegementNotification", "Notification of an unmatched settlement instruction."),
    "semt.001": ("SecuritiesMessageCancellationAdvice", "Advice that a securities message has been cancelled."),
    "semt.002": ("CustodyStatementOfHoldings", "Statement of securities holdings held in custody."),
    "fxtr.008": ("ForeignExchangeTradeInstruction", "Instruction to settle a foreign exchange trade."),
    "fxtr.013": ("ForeignExchangeTradeConfirmation", "Confirmation of a foreign exchange trade."),
    "fxtr.014": ("ForeignExchangeTradeStatusNotification", "Status notification for a foreign exchange trade."),
    "tsin.001": ("InvoiceFinancingRequest", "Request to finance a trade invoice."),
    "tsin.002": ("InvoiceFinancingRequestStatus", "Status of an invoice financing request."),
    "tsin.009": ("InvoiceTaxReport", "Tax report associated with an invoice in a trade transaction."),
}


def _message_meta(message_id: str) -> tuple[str, str]:
    """Return (name, description) for a message ID, falling back to the ID itself."""
    prefix = ".".join(message_id.split(".")[:2])
    return MESSAGE_META.get(prefix, (message_id, f"ISO 20022 {message_id} message."))


def _area_cache_marker(area_id: int) -> str:
    return os.path.join(XSD_DIR, f".area_{area_id}_downloaded")


def _area_is_fresh(area_id: int) -> bool:
    path = _area_cache_marker(area_id)
    if not os.path.exists(path):
        return False
    age_days = (time.time() - os.path.getmtime(path)) / 86400
    return age_days < CACHE_MAX_AGE_DAYS


def _download_area(area_id: int, family: str) -> bytes:
    url = BASE_URL.format(id=area_id)
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    print(f"  GET     {url}")
    with urllib.request.urlopen(req, timeout=30) as resp:
        return resp.read()


def main(force: bool = False) -> None:
    os.makedirs(XSD_DIR, exist_ok=True)

    manifest = []
    downloaded_at = datetime.now(tz=timezone.utc).isoformat()

    for area_id, family, business_area, area_name in BUSINESS_AREAS:
        if not force and _area_is_fresh(area_id):
            print(f"  CACHED  {family} (area {area_id})")
        else:
            try:
                zip_bytes = _download_area(area_id, family)
            except Exception as exc:
                print(f"  FAILED  {family} (area {area_id}): {exc}")
                continue

            zf = zipfile.ZipFile(io.BytesIO(zip_bytes))
            count = 0
            for name in zf.namelist():
                if not name.endswith(".xsd"):
                    continue
                xsd_bytes = zf.read(name)
                dst = os.path.join(XSD_DIR, name)
                with open(dst, "wb") as fh:
                    fh.write(xsd_bytes)
                count += 1
            print(f"  OK      {family}: {count} XSD files extracted")

            # Touch the marker file so freshness check works next run
            with open(_area_cache_marker(area_id), "w") as fh:
                fh.write(downloaded_at)

    # Build manifest from everything in XSD_DIR
    for filename in sorted(os.listdir(XSD_DIR)):
        if not filename.endswith(".xsd"):
            continue
        message_id = filename[:-4]  # strip .xsd
        family = message_id.split(".")[0]
        area_match = next((a for a in BUSINESS_AREAS if a[1] == family), None)
        business_area = area_match[2] if area_match else "other"
        name, description = _message_meta(message_id)
        manifest.append(
            {
                "id": message_id,
                "family": family,
                "business_area": business_area,
                "name": name,
                "description": description,
                "xsd_path": os.path.join(XSD_DIR, filename),
                "downloaded_at": downloaded_at,
            }
        )

    with open(MANIFEST_PATH, "w") as fh:
        json.dump(manifest, fh, indent=2)

    print(f"\nDone: {len(manifest)} XSD files in manifest.")
    print(f"Manifest written to {MANIFEST_PATH}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Download ISO 20022 XSD schemas.")
    parser.add_argument(
        "--force", action="store_true", help="Re-download even if cached."
    )
    args = parser.parse_args()
    main(force=args.force)
