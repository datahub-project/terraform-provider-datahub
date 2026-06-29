# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
"""
Emit ISO 20022 financial pipeline entities to DataHub.

Creates:
  - ISO 20022 glossary hierarchy: root node -> business area nodes -> message type terms
  - ISO 20022 family tags (e.g. iso20022:pacs, iso20022:camt)
  - Kafka topics, PostgreSQL tables, Looker views for each message schema
  - 3-tier lineage: Kafka -> PostgreSQL -> Looker (with field-level lineage)
  - ISO 20022 glossary term and family tag associations on each dataset

No real systems are deployed; all entities are metadata-only.

Requires env vars:
    DATAHUB_GMS_URL    e.g. http://localhost:8080
    DATAHUB_GMS_TOKEN  DataHub access token (or empty string for no auth)

Reads:
    .iso-cache/manifest.json
    .iso-cache/avro/{id}.avsc
    .iso-cache/avro/{id}.fields.json

Usage:
    python3 scripts/iso20022/emit_entities.py [--dry-run]
"""

import argparse
import json
import logging
import os
import re
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

# The DataHub SDK logs a warning + full traceback for schemas with duplicate
# Avro type names. We already catch and tolerate those failures; suppress the
# noise so it doesn't swamp the progress output.
logging.getLogger("datahub.ingestion.extractor.schema_util").setLevel(logging.CRITICAL)

try:
    from datahub.emitter.rest_emitter import DatahubRestEmitter
    from datahub.emitter.mce_builder import (
        make_dataset_urn,
        make_data_platform_urn,
        make_schema_field_urn,
        make_tag_urn,
    )
    from datahub.emitter.mcp import MetadataChangeProposalWrapper
    from datahub.metadata.schema_classes import (
        AuditStampClass,
        BooleanTypeClass,
        BytesTypeClass,
        DatasetLineageTypeClass,
        DatasetPropertiesClass,
        FineGrainedLineageClass,
        FineGrainedLineageDownstreamTypeClass,
        FineGrainedLineageUpstreamTypeClass,
        GlobalTagsClass,
        GlossaryNodeInfoClass,
        GlossaryTermAssociationClass,
        GlossaryTermInfoClass,
        GlossaryTermsClass,
        NullTypeClass,
        NumberTypeClass,
        OtherSchemaClass,
        SchemaFieldClass,
        SchemaFieldDataTypeClass,
        SchemaMetadataClass,
        SchemalessClass,
        StringTypeClass,
        TagAssociationClass,
        TagPropertiesClass,
        UpstreamClass,
        UpstreamLineageClass,
    )
    from datahub.ingestion.extractor.schema_util import avro_schema_to_mce_fields
except ImportError:
    print("ERROR: acryl-datahub not installed. Run: pip install 'acryl-datahub>=0.14'")
    sys.exit(1)

CACHE_DIR = ".iso-cache"
AVRO_DIR = os.path.join(CACHE_DIR, "avro")
MANIFEST_PATH = os.path.join(CACHE_DIR, "manifest.json")

# PostgreSQL database per business area slug
BUSINESS_AREA_DB = {
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

# Display names for each business area slug (used in glossary node names)
ISO20022_AREA_NAMES = {
    "payments":          "Payments",
    "cash_management":   "Cash Management",
    "securities":        "Securities",
    "foreign_exchange":  "Foreign Exchange",
    "trade_finance":     "Trade Finance",
    "collateral":        "Collateral Management",
    "account_management":"Account Management",
    "reference_data":    "Reference Data",
    "authorities":       "Authorities and Regulatory Reporting",
    "cards":             "Cards and ATM",
    "administration":    "Administration",
}

ISO20022_GLOSSARY_ROOT_URN = "urn:li:glossaryNode:iso20022-root"
ISO20022_TAG_PREFIX = "iso20022"

ACTOR_URN = "urn:li:corpuser:datahub"
ENV = "PROD"
EMIT_WORKERS = 12

# DatahubRestEmitter uses requests.Session which is not thread-safe.
# Each worker thread gets its own emitter via thread-local storage.
_tl = threading.local()


def _thread_emitter(gms_url: str, gms_token: str):
    if not hasattr(_tl, "emitter"):
        _tl.emitter = DatahubRestEmitter(gms_server=gms_url, token=gms_token or None)
    return _tl.emitter


def _audit_stamp() -> AuditStampClass:
    return AuditStampClass(time=int(time.time() * 1000), actor=ACTOR_URN)


def _snake(name: str) -> str:
    s = re.sub(r"(?<=[a-z0-9])(?=[A-Z])", "_", name)
    return s.lower()


def _area_db(business_area: str) -> str:
    return BUSINESS_AREA_DB.get(business_area, "data_db")


def _iso22_node_urn(area_slug: str) -> str:
    return f"urn:li:glossaryNode:iso20022-{area_slug.replace('_', '-')}"


def _iso22_term_urn(message_id: str) -> str:
    prefix = ".".join(message_id.split(".")[:2])
    return f"urn:li:glossaryTerm:iso20022.{prefix}"


def _avro_type_to_datahub(avro_type: str) -> SchemaFieldDataTypeClass:
    mapping = {
        "string":  StringTypeClass(),
        "double":  NumberTypeClass(),
        "long":    NumberTypeClass(),
        "boolean": BooleanTypeClass(),
        "bytes":   BytesTypeClass(),
        "null":    NullTypeClass(),
    }
    return SchemaFieldDataTypeClass(type=mapping.get(avro_type, StringTypeClass()))


def _pg_schema_fields(flat_fields: list) -> list:
    stamp = _audit_stamp()
    top_level = [f for f in flat_fields if "." not in f["field_path"]]
    if not top_level:
        top_level = flat_fields[:20]
    return [
        SchemaFieldClass(
            fieldPath=f["field_path"],
            type=_avro_type_to_datahub(f["avro_type"]),
            nativeDataType=f.get("pg_type", "text"),
            description=f.get("doc", ""),
            lastModified=stamp,
        )
        for f in top_level
    ]


def _looker_schema_fields(flat_fields: list) -> list:
    stamp = _audit_stamp()
    dims = [
        f for f in flat_fields
        if "." not in f["field_path"] and f["avro_type"] in ("string", "long")
    ][:5]
    fields = [
        SchemaFieldClass(
            fieldPath=f["field_path"],
            type=_avro_type_to_datahub(f["avro_type"]),
            nativeDataType="dimension",
            description=f.get("doc", ""),
            lastModified=stamp,
        )
        for f in dims
    ]
    fields.append(
        SchemaFieldClass(
            fieldPath="count",
            type=SchemaFieldDataTypeClass(type=NumberTypeClass()),
            nativeDataType="measure",
            description="Count of records.",
            lastModified=stamp,
        )
    )
    return fields


def emit_iso20022_glossary(manifest: list, emitter, dry_run: bool) -> dict:
    """Emit the ISO 20022 glossary hierarchy and return a message_id -> term_urn map."""
    stamp = _audit_stamp()
    mcps = []

    # Root node
    mcps.append(MetadataChangeProposalWrapper(
        entityUrn=ISO20022_GLOSSARY_ROOT_URN,
        aspect=GlossaryNodeInfoClass(
            name="ISO 20022 Financial Messaging Standards",
            definition=(
                "International standard for electronic data interchange between "
                "financial institutions. Covers payments, securities, trade finance, "
                "foreign exchange, and card transactions."
            ),
        ),
    ))

    # Group messages by business area (preserve first-seen order)
    seen_areas: dict[str, list] = {}
    for entry in manifest:
        area = entry["business_area"]
        seen_areas.setdefault(area, []).append(entry)

    # Business area nodes
    for area_slug in seen_areas:
        area_display = ISO20022_AREA_NAMES.get(
            area_slug, area_slug.replace("_", " ").title()
        )
        node_urn = _iso22_node_urn(area_slug)
        mcps.append(MetadataChangeProposalWrapper(
            entityUrn=node_urn,
            aspect=GlossaryNodeInfoClass(
                name=area_display,
                definition=f"ISO 20022 messages covering {area_display.lower()}.",
                parentNode=ISO20022_GLOSSARY_ROOT_URN,
            ),
        ))

    # Message type terms (one per unique prefix, e.g. pacs.008 regardless of version)
    term_map: dict[str, str] = {}
    seen_prefixes: set[str] = set()
    for area_slug, entries in seen_areas.items():
        node_urn = _iso22_node_urn(area_slug)
        for entry in entries:
            msg_id = entry["id"]
            prefix = ".".join(msg_id.split(".")[:2])
            term_urn = _iso22_term_urn(msg_id)
            term_map[msg_id] = term_urn
            if prefix in seen_prefixes:
                continue
            seen_prefixes.add(prefix)
            mcps.append(MetadataChangeProposalWrapper(
                entityUrn=term_urn,
                aspect=GlossaryTermInfoClass(
                    name=f"{entry['name']} ({prefix})",
                    definition=entry["description"],
                    termSource="EXTERNAL",
                    sourceRef="ISO 20022 Registration Authority",
                    sourceUrl="https://www.iso20022.org",
                    parentNode=node_urn,
                ),
            ))

    n_areas = len(seen_areas)
    n_terms = len(seen_prefixes)
    if dry_run:
        print(f"  [dry-run] ISO 20022 glossary: 1 root + {n_areas} area nodes + {n_terms} message terms")
        return term_map

    print(f"  Emitting 1 root + {n_areas} area nodes + {n_terms} message terms ({len(mcps)} MCPs)...")
    for i, mcp in enumerate(mcps, 1):
        emitter.emit_mcp(mcp)
        if i % 50 == 0 or i == len(mcps):
            print(f"  {i}/{len(mcps)} MCPs emitted")
    print(f"  Glossary done.")
    return term_map


def emit_iso20022_tags(manifest: list, emitter, dry_run: bool) -> None:
    """Emit one tag entity per ISO 20022 message family (e.g. iso20022:pacs)."""
    families = sorted({entry["family"] for entry in manifest})
    mcps = [
        MetadataChangeProposalWrapper(
            entityUrn=make_tag_urn(f"{ISO20022_TAG_PREFIX}:{fam}"),
            aspect=TagPropertiesClass(
                name=f"{ISO20022_TAG_PREFIX}:{fam}",
                description=f"ISO 20022 message family: {fam}.",
            ),
        )
        for fam in families
    ]
    if dry_run:
        print(f"  [dry-run] would emit {len(families)} family tags: {', '.join(families)}")
        return
    for mcp in mcps:
        emitter.emit_mcp(mcp)
    print(f"  Tags: {len(families)} ISO 20022 family tags emitted ({', '.join(families)}).")


def emit_message(
    entry: dict,
    avro_schema: dict,
    flat_fields: list,
    iso22_term_urn: str,
    emitter,
    dry_run: bool,
) -> tuple:
    """Emit Kafka topic, PostgreSQL table, Looker view, lineage, and metadata.

    Returns (kafka_urn, pg_urn, looker_urn).
    """
    message_id = entry["id"]
    family = entry["family"]
    business_area = entry["business_area"]
    name = entry["name"]
    description = entry["description"]
    db = _area_db(business_area)
    snake_name = _snake(name)

    kafka_topic = f"iso20022.{family}.{message_id}"
    pg_table = f"{db}.public.{snake_name}"
    looker_view = f"{business_area}_analytics.{snake_name}_view"

    kafka_urn = make_dataset_urn(platform="kafka", name=kafka_topic, env=ENV)
    pg_urn = make_dataset_urn(platform="postgres", name=pg_table, env=ENV)
    looker_urn = make_dataset_urn(platform="looker", name=looker_view, env=ENV)

    stamp = _audit_stamp()
    tag_urn = make_tag_urn(f"{ISO20022_TAG_PREFIX}:{family}")
    mcps = []

    # --- Kafka topic ---
    # Some complex ISO 20022 schemas have duplicate Avro type names; the SDK
    # raises SchemaParseException for those. We tolerate the failure and emit
    # empty schema fields. The logger for schema_util is set to CRITICAL at
    # module level so the SDK's internal traceback warning is suppressed.
    try:
        kafka_fields = avro_schema_to_mce_fields(
            avro_schema=avro_schema, is_key_schema=False, default_nullable=True,
        )
    except Exception:
        kafka_fields = []

    mcps.append(MetadataChangeProposalWrapper(
        entityUrn=kafka_urn,
        aspect=DatasetPropertiesClass(
            name=kafka_topic,
            description=description,
            customProperties={
                "iso20022_id": message_id,
                "iso20022_family": family,
                "business_area": business_area,
                "source": "https://www.iso20022.org",
            },
        ),
    ))
    mcps.append(MetadataChangeProposalWrapper(
        entityUrn=kafka_urn,
        aspect=SchemaMetadataClass(
            schemaName=kafka_topic,
            platform=make_data_platform_urn("kafka"),
            version=0,
            hash="",
            fields=kafka_fields,
            platformSchema=SchemalessClass(),
            lastModified=stamp,
        ),
    ))

    # --- PostgreSQL table ---
    pg_fields = _pg_schema_fields(flat_fields)
    mcps.append(MetadataChangeProposalWrapper(
        entityUrn=pg_urn,
        aspect=DatasetPropertiesClass(
            name=snake_name,
            description=f"Processed ISO 20022 {name} messages stored in {db}.",
            customProperties={
                "iso20022_id": message_id,
                "source_topic": kafka_topic,
                "database": db,
            },
        ),
    ))
    mcps.append(MetadataChangeProposalWrapper(
        entityUrn=pg_urn,
        aspect=SchemaMetadataClass(
            schemaName=snake_name,
            platform=make_data_platform_urn("postgres"),
            version=0,
            hash="",
            fields=pg_fields,
            platformSchema=OtherSchemaClass(rawSchema=""),
            lastModified=stamp,
        ),
    ))

    # --- Looker view ---
    looker_fields = _looker_schema_fields(flat_fields)
    mcps.append(MetadataChangeProposalWrapper(
        entityUrn=looker_urn,
        aspect=DatasetPropertiesClass(
            name=f"{snake_name}_view",
            description=(
                f"Looker LookML view over {db}.public.{snake_name} "
                f"for {business_area.replace('_', ' ')} analytics."
            ),
            customProperties={
                "iso20022_id": message_id,
                "source_table": pg_table,
                "looker_model": f"{business_area}_analytics",
            },
        ),
    ))
    mcps.append(MetadataChangeProposalWrapper(
        entityUrn=looker_urn,
        aspect=SchemaMetadataClass(
            schemaName=f"{snake_name}_view",
            platform=make_data_platform_urn("looker"),
            version=0,
            hash="",
            fields=looker_fields,
            platformSchema=OtherSchemaClass(rawSchema=""),
            lastModified=stamp,
        ),
    ))

    # --- Lineage: Kafka -> PostgreSQL (with field-level lineage) ---
    kafka_field_names = {f["field_path"] for f in flat_fields if "." not in f["field_path"]}
    pg_field_names = {f.fieldPath for f in pg_fields}
    shared = kafka_field_names & pg_field_names
    fine_grained = [
        FineGrainedLineageClass(
            upstreamType=FineGrainedLineageUpstreamTypeClass.FIELD_SET,
            upstreams=[make_schema_field_urn(kafka_urn, col)],
            downstreamType=FineGrainedLineageDownstreamTypeClass.FIELD,
            downstreams=[make_schema_field_urn(pg_urn, col)],
        )
        for col in sorted(shared)
    ]
    mcps.append(MetadataChangeProposalWrapper(
        entityUrn=pg_urn,
        aspect=UpstreamLineageClass(
            upstreams=[UpstreamClass(
                dataset=kafka_urn,
                type=DatasetLineageTypeClass.TRANSFORMED,
            )],
            fineGrainedLineages=fine_grained,
        ),
    ))

    # --- Lineage: PostgreSQL -> Looker ---
    mcps.append(MetadataChangeProposalWrapper(
        entityUrn=looker_urn,
        aspect=UpstreamLineageClass(upstreams=[UpstreamClass(
            dataset=pg_urn,
            type=DatasetLineageTypeClass.VIEW,
        )]),
    ))

    # --- ISO 20022 glossary term association (Kafka + PG) ---
    if iso22_term_urn:
        glossary_aspect = GlossaryTermsClass(
            terms=[GlossaryTermAssociationClass(urn=iso22_term_urn, actor=stamp.actor)],
            auditStamp=stamp,
        )
        for dataset_urn in [kafka_urn, pg_urn]:
            mcps.append(MetadataChangeProposalWrapper(
                entityUrn=dataset_urn,
                aspect=glossary_aspect,
            ))

    # --- ISO 20022 family tag (all three tiers) ---
    tag_aspect = GlobalTagsClass(tags=[TagAssociationClass(tag=tag_urn)])
    for dataset_urn in [kafka_urn, pg_urn, looker_urn]:
        mcps.append(MetadataChangeProposalWrapper(
            entityUrn=dataset_urn,
            aspect=tag_aspect,
        ))

    if dry_run:
        for mcp in mcps:
            print(f"    [dry-run] {mcp.entityUrn} / {type(mcp.aspect).__name__}")
    else:
        for mcp in mcps:
            emitter.emit_mcp(mcp)

    return kafka_urn, pg_urn, looker_urn


def main(dry_run: bool = False) -> None:
    gms_url = os.environ.get("DATAHUB_GMS_URL", "")
    gms_token = os.environ.get("DATAHUB_GMS_TOKEN", "")

    if not gms_url and not dry_run:
        print("ERROR: DATAHUB_GMS_URL is not set. Export it or use --dry-run.")
        sys.exit(1)

    if not os.path.exists(MANIFEST_PATH):
        print(f"ERROR: {MANIFEST_PATH} not found. Run download.py first.")
        sys.exit(1)

    with open(MANIFEST_PATH) as fh:
        manifest = json.load(fh)

    emitter = (
        None if dry_run
        else DatahubRestEmitter(gms_server=gms_url, token=gms_token or None)
    )

    # Step 1: emit glossary hierarchy and family tags
    print("\n--- ISO 20022 Glossary ---")
    term_map = emit_iso20022_glossary(manifest, emitter, dry_run)
    print("\n--- ISO 20022 Tags ---")
    emit_iso20022_tags(manifest, emitter, dry_run)

    # Step 2: prepare work items (read files in main thread; emit in parallel)
    print("\n--- Dataset Entities ---")
    work = []
    for entry in manifest:
        message_id = entry["id"]
        avsc_path = os.path.join(AVRO_DIR, f"{message_id}.avsc")
        fields_path = os.path.join(AVRO_DIR, f"{message_id}.fields.json")

        if not os.path.exists(avsc_path):
            print(f"  SKIP    {message_id} (no .avsc -- run xsd_to_avro.py first)")
            continue

        with open(avsc_path) as fh:
            avro_schema_dict = json.load(fh)
        avro_str = json.dumps(avro_schema_dict)

        flat_fields = []
        if os.path.exists(fields_path):
            with open(fields_path) as fh:
                flat_fields = json.load(fh)

        iso22_term_urn = term_map.get(message_id, _iso22_term_urn(message_id))
        work.append((entry, avro_str, flat_fields, iso22_term_urn))

    total = len(work)
    ok = 0
    failed = 0
    lock = threading.Lock()

    def _emit_one(item):
        entry, avro_str, flat_fields, iso22_term_urn = item
        e = emitter if dry_run else _thread_emitter(gms_url, gms_token)
        return emit_message(entry, avro_str, flat_fields, iso22_term_urn, e, dry_run)

    workers = 1 if dry_run else EMIT_WORKERS
    print(f"  {total} messages, {workers} worker(s)")
    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = {pool.submit(_emit_one, item): item[0] for item in work}
        for i, future in enumerate(as_completed(futures), 1):
            entry = futures[future]
            message_id = entry["id"]
            try:
                future.result()
                with lock:
                    ok += 1
                    if i % 50 == 0 or i == total:
                        print(f"  {i}/{total} done (last: {message_id})")
            except Exception as exc:
                with lock:
                    failed += 1
                    print(f"  FAILED [{i}/{total}] {message_id}: {exc}")

    print(f"\nDone: {ok} messages emitted, {failed} failed.")
    if failed:
        sys.exit(1)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Emit ISO 20022 pipeline entities to DataHub."
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print what would be emitted without contacting DataHub.",
    )
    args = parser.parse_args()
    main(dry_run=args.dry_run)
