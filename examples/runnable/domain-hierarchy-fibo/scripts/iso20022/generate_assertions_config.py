# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
"""
Generate assertions_config.json for ISO 20022 DataHub Observe rules.

Reads .iso-cache/manifest.json and .iso-cache/avro/*.fields.json to produce
.iso-cache/assertions_config.json which Terraform reads at plan time to create
schema, volume, field-metric, and SQL assertions on the PostgreSQL datasets.

The output lives in the gitignored .iso-cache/ directory (like .fibo-cache/
fibo.json) rather than being committed - it is a generated artifact rebuilt
by 'make iso-assertions-config'.

Usage:
    python3 scripts/iso20022/generate_assertions_config.py
"""

import json
import os
import re
import sys

MANIFEST_PATH = ".iso-cache/manifest.json"
AVRO_DIR = ".iso-cache/avro"
OUTPUT_PATH = ".iso-cache/assertions_config.json"

# The 26 representative message prefixes for which we create assertions.
# Maps prefix -> human description suffix (appended to message name).
TARGET_PREFIXES = [
    "pacs.002", "pacs.003", "pacs.004", "pacs.007",
    "pacs.008", "pacs.009", "pacs.010",
    "pain.001", "pain.002", "pain.007", "pain.008", "pain.013",
    "camt.052", "camt.053", "camt.054", "camt.056",
    "sese.023", "sese.024", "sese.034",
    "fxtr.008", "fxtr.014",
    "colr.003",
    "tsin.001",
    "acmt.001", "acmt.002", "acmt.003",
]

# Volume thresholds per prefix: (min_rows, severity).
# Reflects realistic daily volumes for a mid-tier financial institution.
VOLUME_CONFIG = {
    "pacs.008": (50000, "HIGH"),
    "pacs.002": (50000, "HIGH"),
    "pain.001": (25000, "HIGH"),
    "pain.002": (25000, "HIGH"),
    "pacs.009": (10000, "HIGH"),
    "pacs.003": (5000,  "MEDIUM"),
    "pacs.010": (5000,  "MEDIUM"),
    "camt.054": (5000,  "MEDIUM"),
    "pacs.004": (3000,  "MEDIUM"),
    "pacs.007": (3000,  "MEDIUM"),
    "pain.007": (2000,  "MEDIUM"),
    "pain.008": (2000,  "MEDIUM"),
    "sese.023": (2000,  "HIGH"),
    "sese.024": (2000,  "HIGH"),
    "camt.053": (1000,  "HIGH"),
    "camt.052": (500,   "MEDIUM"),
    "fxtr.008": (500,   "MEDIUM"),
    "fxtr.014": (500,   "MEDIUM"),
    "camt.056": (200,   "MEDIUM"),
    "colr.003": (200,   "MEDIUM"),
    "sese.034": (200,   "MEDIUM"),
    "pain.013": (100,   "LOW"),
    "tsin.001": (100,   "LOW"),
    "acmt.001": (50,    "LOW"),
    "acmt.002": (50,    "LOW"),
    "acmt.003": (50,    "LOW"),
}

# Keywords ranked by financial significance for mandatory-field selection.
# Fields whose path (last segment, case-insensitive) contains a higher-priority
# keyword are preferred for field/SQL rules. Tries each keyword in order; stops
# when it has enough candidates.
MANDATORY_FIELD_KEYWORDS = [
    # Universal message envelope fields
    "msgid",
    "nboftxs",
    # Transaction identifiers
    "txid", "endtoendid", "instrid", "unqtxid",
    # Settlement / amount
    "intrbksttlmamt", "intrbksttlmdt",
    "ttlintrbksttlmamt", "ctrlsum",
    # Payment/trade amounts
    "instdamt", "txamt", "tradamt", "qlfd_fctrd_amt",
    "amt",
    # Dates
    "reqdexctndt", "reqdcolltndt", "instrdamt",
    # Identifiers
    "acctid", "isin", "fsci",
]

# Business area to database mapping (mirrors emit_entities.py BUSINESS_AREA_DB)
BUSINESS_AREA_DB = {
    "payments":           "payments_db",
    "cash_management":    "payments_db",
    "securities":         "securities_db",
    "foreign_exchange":   "fx_db",
    "trade_finance":      "trade_finance_db",
    "collateral":         "securities_db",
    "account_management": "accounts_db",
    "reference_data":     "reference_db",
    "authorities":        "regulatory_db",
    "cards":              "cards_db",
    "administration":     "admin_db",
}


def _snake(name: str) -> str:
    s = re.sub(r"(?<=[a-z0-9])(?=[A-Z])", "_", name)
    return s.lower()


def _pg_type_to_dh(pg_type: str) -> str:
    """Map PostgreSQL column type to DataHub SchemaField type."""
    t = pg_type.lower()
    if "int" in t or "numeric" in t or "decimal" in t or "float" in t or "double" in t or "real" in t:
        return "NUMBER"
    if "bool" in t:
        return "BOOLEAN"
    if "date" in t or "time" in t or "timestamp" in t:
        return "DATE"
    if "json" in t or "struct" in t:
        return "STRUCT"
    return "STRING"


def _last_segment(field_path: str) -> str:
    """Return the final camelCase segment of a dotted field path."""
    return field_path.split(".")[-1]


def _score_field(field_path: str) -> int:
    """Score a field by mandatory-field keyword priority (lower index = higher priority)."""
    seg = _last_segment(field_path).lower()
    for i, kw in enumerate(MANDATORY_FIELD_KEYWORDS):
        if kw in seg:
            return i
    return len(MANDATORY_FIELD_KEYWORDS)


def _pick_mandatory_fields(fields: list, n: int = 3) -> list:
    """Select up to n fields most likely to be mandatory identifiers."""
    scored = sorted(range(len(fields)), key=lambda i: _score_field(fields[i]["field_path"]))
    selected = []
    seen_segs = set()
    for idx in scored:
        seg = _last_segment(fields[idx]["field_path"]).lower()
        if seg not in seen_segs:
            selected.append(fields[idx])
            seen_segs.add(seg)
        if len(selected) >= n:
            break
    return selected


def _field_description(field_path: str, table_desc: str) -> str:
    seg = _last_segment(field_path)
    # Human-readable expansion of common ISO 20022 abbreviations
    expansions = {
        "MsgId":              "Message ID",
        "NbOfTxs":            "Number of Transactions",
        "EndToEndId":         "End-to-End ID",
        "InstrId":            "Instruction ID",
        "TxId":               "Transaction ID",
        "IntrBkSttlmAmt":     "Interbank Settlement Amount",
        "IntrBkSttlmDt":      "Interbank Settlement Date",
        "TtlIntrBkSttlmAmt":  "Total Interbank Settlement Amount",
        "CtrlSum":            "Control Sum",
        "InstdAmt":           "Instructed Amount",
        "TxAmt":              "Transaction Amount",
        "TradAmt":            "Trade Amount",
        "ReqdExctnDt":        "Requested Execution Date",
        "ReqdColltnDt":       "Requested Collection Date",
        "AcctId":             "Account ID",
        "ISIN":               "ISIN Identifier",
    }
    human = expansions.get(seg, seg)
    return f"{human} must never be null on {table_desc} records"


def _sql_description(field_path: str, table: str, table_desc: str) -> str:
    seg = _last_segment(field_path)
    expansions = {
        "MsgId":          "Zero tolerance for null message IDs",
        "NbOfTxs":        "Number of transactions must always be present",
        "EndToEndId":     "End-to-end identifier must be set on every record",
        "IntrBkSttlmAmt": "All records must carry a non-null interbank settlement amount",
        "TtlIntrBkSttlmAmt": "Total interbank settlement amount must be present",
        "InstdAmt":       "Instructed amount must be present on all payment records",
        "TxId":           "Transaction identifier must be set on every record",
        "InstrId":        "Instruction ID must be present for audit traceability",
    }
    rule_desc = expansions.get(seg, f"Null values not permitted in {seg} column")
    return f"{rule_desc} - {table_desc}"


def build_table_entry(entry: dict, fields: list, prefix: str) -> dict:
    """Build a single table config dict for assertions_config.json."""
    name = entry["name"]
    business_area = entry["business_area"]
    db = BUSINESS_AREA_DB.get(business_area, "data_db")
    snake_name = _snake(name)
    pg_table = f"{db}.public.{snake_name}"
    urn = f"urn:li:dataset:(urn:li:dataPlatform:postgres,{pg_table},PROD)"

    # Schema fields: all fields emitted by emit_entities.py _pg_schema_fields().
    # emit_entities.py uses all flat_fields when no top-level (dot-free) fields exist,
    # which is always the case for ISO 20022 messages.
    schema_fields_raw = fields
    schema_fields = [
        {"path": f["field_path"], "type": _pg_type_to_dh(f.get("pg_type", "text"))}
        for f in schema_fields_raw
    ]

    # Volume threshold
    vol_cfg = VOLUME_CONFIG.get(prefix)
    volume = {"min_rows": vol_cfg[0], "severity": vol_cfg[1]} if vol_cfg else None

    # Key: replace dots with dashes for Terraform map keys
    key = prefix.replace(".", "-")

    family = prefix.split(".")[0]
    description = f"{name} (ISO 20022 {prefix})"

    # Field rules: pick 2-3 mandatory identifier fields from all fields
    mandatory = _pick_mandatory_fields(schema_fields_raw, n=3)
    field_rules = []
    for mf in mandatory:
        fp = mf["field_path"]
        dh_type = _pg_type_to_dh(mf.get("pg_type", "text"))
        rule_id = _last_segment(fp).lower().replace("_", "")
        field_rules.append({
            "id": rule_id,
            "field_path": fp,
            "field_type": dh_type,
            "metric": "NULL_COUNT",
            "operator": "EQUAL_TO",
            "single_value": "0",
            "description": _field_description(fp, description),
        })

    # SQL rules: IS NULL violation count check on the top 2 mandatory fields
    sql_mandatory = mandatory[:2]
    sql_rules = []
    for mf in sql_mandatory:
        fp = mf["field_path"]
        # Double-quote column names containing dots (required for PostgreSQL)
        col = f'"{fp}"'
        severity = vol_cfg[1] if vol_cfg else "MEDIUM"
        rule_id = "sql_null_" + _last_segment(fp).lower().replace("_", "")
        sql_rules.append({
            "id": rule_id,
            "severity": severity,
            "description": _sql_description(fp, pg_table, description),
            "statement": (
                f"SELECT COUNT(*) FROM {pg_table} WHERE {col} IS NULL"
            ),
        })

    return {
        "key": key,
        "urn": urn,
        "table": pg_table,
        "description": description,
        "area": business_area,
        "family": family,
        "schema_fields": schema_fields,
        "volume": volume,
        "field_rules": field_rules,
        "sql_rules": sql_rules,
    }


def main() -> None:
    if not os.path.exists(MANIFEST_PATH):
        print(f"ERROR: {MANIFEST_PATH} not found. Run 'make iso-data' first.")
        sys.exit(1)

    with open(MANIFEST_PATH) as fh:
        manifest = json.load(fh)

    # Build a map from prefix to latest version entry
    prefix_to_entry: dict[str, dict] = {}
    for entry in manifest:
        msg_id = entry["id"]
        prefix = ".".join(msg_id.split(".")[:2])
        if prefix in TARGET_PREFIXES:
            # Keep the latest version (lexicographic sort is sufficient for ISO 20022 IDs)
            if prefix not in prefix_to_entry or msg_id > prefix_to_entry[prefix]["id"]:
                prefix_to_entry[prefix] = entry

    tables = []
    missing = []
    for prefix in TARGET_PREFIXES:
        if prefix not in prefix_to_entry:
            missing.append(prefix)
            continue

        entry = prefix_to_entry[prefix]
        msg_id = entry["id"]
        fields_path = os.path.join(AVRO_DIR, f"{msg_id}.fields.json")

        if not os.path.exists(fields_path):
            print(f"WARNING: fields file missing for {msg_id} -- skipping")
            continue

        with open(fields_path) as fh:
            flat_fields = json.load(fh)

        if not flat_fields:
            print(f"WARNING: no fields for {msg_id} -- skipping")
            continue

        table_entry = build_table_entry(entry, flat_fields, prefix)
        tables.append(table_entry)
        print(
            f"  {prefix:12s} -> {msg_id:26s}  "
            f"schema={len(table_entry['schema_fields']):2d} fields  "
            f"field_rules={len(table_entry['field_rules'])}  "
            f"sql_rules={len(table_entry['sql_rules'])}  "
            f"volume={table_entry['volume']['min_rows'] if table_entry['volume'] else 'none'}"
        )

    if missing:
        print(f"WARNING: prefixes not found in manifest: {missing}")

    output = {"tables": tables}
    with open(OUTPUT_PATH, "w") as fh:
        json.dump(output, fh, indent=2)
        fh.write("\n")

    schema_count = sum(1 for _ in tables)
    volume_count = sum(1 for t in tables if t["volume"])
    field_count = sum(len(t["field_rules"]) for t in tables)
    sql_count = sum(len(t["sql_rules"]) for t in tables)
    total = schema_count + volume_count + field_count + sql_count

    print(f"\nWrote {OUTPUT_PATH}")
    print(f"  {len(tables)} tables")
    print(f"  {schema_count} schema assertions (1 per table)")
    print(f"  {volume_count} volume assertions")
    print(f"  {field_count} field-metric assertions")
    print(f"  {sql_count} SQL assertions (PASSIVE)")
    print(f"  {total} assertions total")


if __name__ == "__main__":
    main()
