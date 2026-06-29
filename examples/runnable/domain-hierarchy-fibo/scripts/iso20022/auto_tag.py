# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
"""
Auto-tag ISO 20022 DataHub entities with FIBO domain and glossary terms using an LLM.

For each ISO 20022 message, sends field documentation to Claude claude-haiku-4-5 and
asks it to match each field to a FIBO domain and (where confident) a glossary term.
Results are cached to .iso-cache/tags/ to avoid redundant API calls.

Requires env vars:
    ANTHROPIC_API_KEY  Anthropic API key
    DATAHUB_GMS_URL    e.g. http://localhost:8080
    DATAHUB_GMS_TOKEN  DataHub access token (or empty string for no auth)

Reads:
    .iso-cache/manifest.json
    .iso-cache/avro/{id}.fields.json
    .fibo-cache/fibo.json

Writes:
    .iso-cache/tags/{id}.json   -- cached LLM decisions

Usage:
    python3 scripts/iso20022/auto_tag.py [--force] [--dry-run]
"""

import argparse
import concurrent.futures
import json
import os
import sys
import threading
import time

try:
    import anthropic
except ImportError:
    print("ERROR: anthropic not installed. Run: pip install anthropic")
    sys.exit(1)

try:
    from datahub.emitter.rest_emitter import DatahubRestEmitter
    from datahub.emitter.mce_builder import make_dataset_urn
    from datahub.emitter.mcp import MetadataChangeProposalWrapper
    from datahub.metadata.schema_classes import (
        AuditStampClass,
        DomainsClass,
        GlossaryTermAssociationClass,
        GlossaryTermsClass,
    )
except ImportError:
    print("ERROR: acryl-datahub not installed. Run: pip install 'acryl-datahub>=0.14'")
    sys.exit(1)

CACHE_DIR = ".iso-cache"
TAGS_DIR = os.path.join(CACHE_DIR, "tags")
MANIFEST_PATH = os.path.join(CACHE_DIR, "manifest.json")
AVRO_DIR = os.path.join(CACHE_DIR, "avro")
FIBO_CACHE_PATH = os.path.join(".fibo-cache", "fibo.json")

MODEL = "claude-haiku-4-5"
FIELDS_PER_CALL = 20  # batch size per LLM call
DOMAIN_CONFIDENCE_THRESHOLD = 0.5
TERM_CONFIDENCE_THRESHOLD = 0.6
ENV = "PROD"
ACTOR_URN = "urn:li:corpuser:datahub"

# ISO 20022 business area -> most likely FIBO domain codes (for prompt narrowing).
# Empty list means show all FIBO domains without preferencing (e.g. cards, admin).
AREA_TO_FIBO_DOMAINS = {
    "payments":          ["FBC", "FND"],
    "cash_management":   ["FBC", "FND"],
    "securities":        ["SEC", "DER", "MD"],
    "foreign_exchange":  ["DER", "FBC"],
    "trade_finance":     ["FBC", "LOAN"],
    "collateral":        ["SEC", "FBC"],
    "account_management":["FBC", "BE"],
    "reference_data":    ["FBC", "FND"],
    "authorities":       ["FBC", "BE"],
    "cards":             [],
    "administration":    [],
}


def _audit_stamp() -> AuditStampClass:
    return AuditStampClass(time=int(time.time() * 1000), actor=ACTOR_URN)


def _build_term_lookup(fibo: dict) -> dict:
    """Build a lowercase name -> term_id map from all FIBO terms.

    The LLM returns plausible term_names but fabricates term_ids. This
    lookup resolves those names to the actual IDs used in DataHub
    (which match the id field in fibo.json, e.g. 'tf-fibo-fbc-debtor').
    """
    lookup: dict[str, str] = {}
    root = fibo.get("root", {})
    for domain in root.get("domains", []):
        for module in domain.get("modules", []):
            for leaf in module.get("leaves", []):
                for term in leaf.get("terms", []):
                    name = term.get("name", "").lower().strip()
                    tid = term.get("id", "")
                    if name and tid:
                        lookup[name] = tid
    return lookup


def _load_fibo(fibo_path: str) -> dict:
    if not os.path.exists(fibo_path):
        return {}
    with open(fibo_path) as fh:
        return json.load(fh)


def _build_domain_summary(fibo: dict, preferred_codes: list[str]) -> str:
    """Build a compact domain summary for the LLM prompt."""
    if not fibo:
        return "(FIBO data not available)"

    root = fibo.get("root", {})
    lines = []
    for domain in root.get("domains", []):
        code = domain.get("code", "")
        # Show all domains but highlight preferred ones for this business area
        priority = "(preferred)" if code in preferred_codes else ""
        lines.append(
            f"- domain_id={domain['id']!r} code={code!r} name={domain['name']!r} {priority}"
        )
        desc = domain.get("description", "")
        if desc:
            lines.append(f"  {desc[:120]}")
        for module in domain.get("modules", [])[:5]:
            lines.append(
                f"  - module_id={module['id']!r} name={module['name']!r}"
            )
    return "\n".join(lines)


def _build_term_summary(fibo: dict, domain_id: str) -> str:
    """Build a compact glossary term list for a given domain."""
    root = fibo.get("root", {})
    for domain in root.get("domains", []):
        if domain.get("id") != domain_id:
            continue
        lines = []
        for module in domain.get("modules", []):
            for leaf in module.get("leaves", []):
                for term in leaf.get("terms", [])[:10]:
                    lines.append(
                        f"- term_id={term['id']!r} name={term['name']!r}"
                    )
                    defn = term.get("definition", "")
                    if defn:
                        lines.append(f"  {defn[:100]}")
        return "\n".join(lines[:80]) or "(no terms available)"
    return "(domain not found)"


def _call_llm(
    client: anthropic.Anthropic,
    message_name: str,
    message_description: str,
    fields: list[dict],
    domain_summary: str,
) -> list[dict]:
    """Call the LLM to tag a batch of fields. Returns list of tag decisions."""
    field_lines = []
    for i, field in enumerate(fields):
        field_lines.append(
            f"{i+1}. field_path={field['field_path']!r}"
            f" type={field['avro_type']!r}"
            f" doc={field.get('doc', '')[:120]!r}"
        )

    prompt = f"""You are a financial data expert mapping ISO 20022 message fields to FIBO (Financial Industry Business Ontology) taxonomy nodes.

Message: {message_name}
Description: {message_description}

Fields to classify:
{chr(10).join(field_lines)}

FIBO domains and modules:
{domain_summary}

For each numbered field, return a JSON object with:
- "field_path": the exact field_path string
- "domain_id": the best matching FIBO domain_id string (e.g. "tf-example-fibo-sec")
- "domain_confidence": float 0.0-1.0
- "term_id": best matching glossary term_id if confident (omit if confidence < {TERM_CONFIDENCE_THRESHOLD})
- "term_name": human name of the term (omit if no term)
- "term_confidence": float 0.0-1.0 (omit if no term)

Return a JSON array only -- no explanation, no markdown, no code block. Example:
[{{"field_path": "Amt", "domain_id": "tf-example-fibo-fbc", "domain_confidence": 0.85}}]"""

    try:
        response = client.messages.create(
            model=MODEL,
            max_tokens=8192,
            messages=[{"role": "user", "content": prompt}],
        )
        raw = response.content[0].text.strip()
        # Strip accidental markdown fences
        if raw.startswith("```"):
            raw = raw.split("```")[1]
            if raw.startswith("json"):
                raw = raw[4:]
        decisions = json.loads(raw)
        if not isinstance(decisions, list):
            decisions = []
        return decisions
    except Exception as exc:
        print(f"      LLM call failed: {exc}")
        return []


def process_message(
    entry: dict,
    flat_fields: list,
    fibo: dict,
    term_lookup: dict,
    client: "anthropic.Anthropic | None",
    emitter: "DatahubRestEmitter | None",
    dry_run: bool,
    force: bool,
) -> bool:
    message_id = entry["id"]
    business_area = entry.get("business_area", "")
    tags_path = os.path.join(TAGS_DIR, f"{message_id}.json")

    if not force and os.path.exists(tags_path):
        print(f"  CACHED  {message_id}")
        with open(tags_path) as fh:
            decisions = json.load(fh)
    else:
        if not flat_fields:
            print(f"  SKIP    {message_id} (no fields)")
            return True

        preferred = AREA_TO_FIBO_DOMAINS.get(business_area, [])
        domain_summary = _build_domain_summary(fibo, preferred)

        decisions = []
        batches = [
            flat_fields[i : i + FIELDS_PER_CALL]
            for i in range(0, len(flat_fields), FIELDS_PER_CALL)
        ]
        print(
            f"  TAG     {message_id} ({len(flat_fields)} fields, {len(batches)} LLM call(s))"
        )
        for batch in batches:
            batch_decisions = _call_llm(
                client,
                entry["name"],
                entry["description"],
                batch,
                domain_summary,
            )
            decisions.extend(batch_decisions)
            time.sleep(0.1)  # brief pause between batches within a message

        with open(tags_path, "w") as fh:
            json.dump(decisions, fh, indent=2)

    # Aggregate domain tag: pick the domain with the highest average confidence
    domain_scores: dict[str, list[float]] = {}
    for d in decisions:
        did = d.get("domain_id", "")
        conf = float(d.get("domain_confidence", 0))
        if did and conf >= DOMAIN_CONFIDENCE_THRESHOLD:
            domain_scores.setdefault(did, []).append(conf)

    best_domain = None
    if domain_scores:
        best_domain = max(domain_scores, key=lambda k: sum(domain_scores[k]) / len(domain_scores[k]))

    if dry_run:
        print(f"    [dry-run] domain={best_domain or 'none'}, {len(decisions)} field decisions")
        return True

    if not emitter:
        return True

    kafka_urn = make_dataset_urn(
        platform="kafka",
        name=f"iso20022.{entry['family']}.{message_id}",
        env=ENV,
    )
    pg_urn = make_dataset_urn(
        platform="postgres",
        name=f"{_area_db(business_area)}.public.{_snake(entry['name'])}",
        env=ENV,
    )

    stamp = _audit_stamp()

    if best_domain:
        # Domain URN must match the tf-example-fibo-{code} format used by Terraform.
        domain_urn = f"urn:li:domain:tf-example-fibo-{best_domain}"
        for dataset_urn in [kafka_urn, pg_urn]:
            try:
                emitter.emit_mcp(
                    MetadataChangeProposalWrapper(
                        entityUrn=dataset_urn,
                        aspect=DomainsClass(domains=[domain_urn]),
                    )
                )
            except Exception as exc:
                print(f"    WARNING: failed to apply domain tag: {exc}")

    # Always include the ISO 20022 message type term so it survives this aspect write.
    # emit_entities.py sets GlossaryTermsClass to [iso22 term]; this replaces it with
    # [iso22 term + FIBO terms] so both coexist after auto_tag runs.
    msg_prefix = ".".join(message_id.split(".")[:2])
    iso22_term_urn = f"urn:li:glossaryTerm:iso20022.{msg_prefix}"
    kafka_terms = [GlossaryTermAssociationClass(urn=iso22_term_urn, actor=stamp.actor)]

    # Resolve LLM-returned term_names to actual FIBO term IDs via the lookup.
    # The LLM fabricates term_ids; only term_name is reliable. The lookup maps
    # lowercase names to the tf-fibo-* IDs stored in DataHub.
    seen_term_ids: set[str] = set()
    for d in decisions:
        term_name = d.get("term_name", "").lower().strip()
        term_conf = float(d.get("term_confidence", 0))
        if not term_name or term_conf < TERM_CONFIDENCE_THRESHOLD:
            continue
        resolved_id = term_lookup.get(term_name)
        if not resolved_id or resolved_id in seen_term_ids:
            continue
        seen_term_ids.add(resolved_id)
        kafka_terms.append(
            GlossaryTermAssociationClass(
                urn=f"urn:li:glossaryTerm:{resolved_id}",
                actor=stamp.actor,
            )
        )

    if kafka_terms:
        for dataset_urn in [kafka_urn, pg_urn]:
            try:
                emitter.emit_mcp(
                    MetadataChangeProposalWrapper(
                        entityUrn=dataset_urn,
                        aspect=GlossaryTermsClass(
                            terms=kafka_terms,
                            auditStamp=stamp,
                        ),
                    )
                )
            except Exception as exc:
                print(f"    WARNING: failed to apply glossary terms: {exc}")

    print(
        f"    applied: domain={best_domain or 'none'}, {len(kafka_terms)} glossary term(s)"
    )
    return True


def _area_db(business_area: str) -> str:
    mapping = {
        "payments":          "payments_db",
        "cash_management":   "payments_db",
        "securities":        "securities_db",
        "foreign_exchange":  "fx_db",
        "trade_finance":     "trade_finance_db",
        "collateral":        "securities_db",
        "account_management":"accounts_db",
        "reference_data":    "reference_db",
        "authorities":       "regulatory_db",
        "cards":             "cards_db",
        "administration":    "admin_db",
    }
    return mapping.get(business_area, "data_db")


def _snake(name: str) -> str:
    import re
    s = re.sub(r"(?<=[a-z0-9])(?=[A-Z])", "_", name)
    return s.lower()


def main(force: bool = False, dry_run: bool = False, workers: int = 10) -> None:
    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    gms_url = os.environ.get("DATAHUB_GMS_URL", "")
    gms_token = os.environ.get("DATAHUB_GMS_TOKEN", "")

    if not api_key:
        print("ERROR: ANTHROPIC_API_KEY is not set.")
        sys.exit(1)
    if not gms_url and not dry_run:
        print("ERROR: DATAHUB_GMS_URL is not set. Export it or use --dry-run.")
        sys.exit(1)

    if not os.path.exists(MANIFEST_PATH):
        print(f"ERROR: {MANIFEST_PATH} not found. Run download.py first.")
        sys.exit(1)

    with open(MANIFEST_PATH) as fh:
        manifest = json.load(fh)

    fibo = _load_fibo(FIBO_CACHE_PATH)
    if not fibo:
        print("WARNING: .fibo-cache/fibo.json not found. Run 'make fibo-data' for domain context.")

    os.makedirs(TAGS_DIR, exist_ok=True)

    # Build lookup: lowercase term name -> actual term ID (tf-fibo-* format).
    # Used to resolve LLM-returned term_names to real DataHub URN suffixes.
    term_lookup = _build_term_lookup(fibo)
    print(f"Loaded {len(term_lookup)} FIBO terms for name resolution.")

    # anthropic.Anthropic client is thread-safe; share a single instance.
    client = anthropic.Anthropic(api_key=api_key)
    # DatahubRestEmitter uses requests.Session internally; create one per thread.
    def make_emitter():
        return None if dry_run else DatahubRestEmitter(gms_server=gms_url, token=gms_token or None)

    counter_lock = threading.Lock()
    ok = 0
    failed = 0
    total = len(manifest)

    def _process_one(entry: dict) -> bool:
        nonlocal ok, failed
        message_id = entry["id"]
        fields_path = os.path.join(AVRO_DIR, f"{message_id}.fields.json")
        flat_fields = []
        if os.path.exists(fields_path):
            with open(fields_path) as fh:
                flat_fields = json.load(fh)
        result = process_message(entry, flat_fields, fibo, term_lookup, client, make_emitter(), dry_run, force)
        with counter_lock:
            if result:
                ok += 1
            else:
                failed += 1
            done = ok + failed
            if done % 50 == 0 or done == total:
                print(f"  Progress: {done}/{total} ({ok} ok, {failed} failed)")
        return result

    print(f"Processing {total} messages with {workers} parallel workers...")
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        list(pool.map(_process_one, manifest))

    print(f"\nDone: {ok} messages tagged, {failed} failed.")
    if failed:
        sys.exit(1)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="LLM-based FIBO tagging of ISO 20022 entities.")
    parser.add_argument("--force", action="store_true", help="Re-tag even if cache exists.")
    parser.add_argument("--dry-run", action="store_true", help="Do not contact DataHub or Anthropic.")
    parser.add_argument("--workers", type=int, default=20, help="Parallel worker threads (default: 20).")
    args = parser.parse_args()
    main(force=args.force, dry_run=args.dry_run, workers=args.workers)
