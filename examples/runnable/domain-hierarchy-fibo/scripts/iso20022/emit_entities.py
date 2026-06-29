# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
"""
Emit ISO 20022 financial pipeline entities to DataHub.

Creates synthetic metadata for a Kafka -> PostgreSQL -> Looker pipeline
and wires lineage between the three tiers. No real systems are deployed;
all entities are metadata-only.

Requires env vars:
    DATAHUB_GMS_URL    e.g. http://localhost:8080
    DATAHUB_GMS_TOKEN  DataHub access token (or empty string for no auth)

Reads:
    .iso-cache/manifest.json
    .iso-cache/avro/{id}.avsc
    .iso-cache/avro/{id}.fields.json
    .fibo-cache/fibo.json   (optional, for domain URN lookup)

Usage:
    python3 scripts/iso20022/emit_entities.py [--dry-run]
"""

import argparse
import json
import os
import sys
import time

try:
    from datahub.emitter.rest_emitter import DatahubRestEmitter
    from datahub.emitter.mce_builder import (
        make_dataset_urn,
        make_data_platform_urn,
        make_schema_field_urn,
    )
    from datahub.emitter.mcp import MetadataChangeProposalWrapper
    from datahub.metadata.schema_classes import (
        AuditStampClass,
        BooleanTypeClass,
        BytesTypeClass,
        DatasetLineageTypeClass,
        DatasetPropertiesClass,
        DomainsClass,
        EnumTypeClass,
        FineGrainedLineageClass,
        FineGrainedLineageDownstreamTypeClass,
        FineGrainedLineageUpstreamTypeClass,
        NullTypeClass,
        NumberTypeClass,
        OtherSchemaClass,
        SchemaFieldClass,
        SchemaFieldDataTypeClass,
        SchemaMetadataClass,
        SchemalessClass,
        StringTypeClass,
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
FIBO_CACHE_PATH = os.path.join(".fibo-cache", "fibo.json")

# PostgreSQL database per business area
BUSINESS_AREA_DB = {
    "payments": "payments_db",
    "cash_management": "payments_db",
    "securities": "securities_db",
    "foreign_exchange": "fx_db",
    "trade_finance": "trade_finance_db",
}

ACTOR_URN = "urn:li:corpuser:datahub"
ENV = "PROD"


def _audit_stamp() -> AuditStampClass:
    return AuditStampClass(time=int(time.time() * 1000), actor=ACTOR_URN)


def _avro_type_to_datahub(avro_type: str) -> SchemaFieldDataTypeClass:
    mapping = {
        "string": StringTypeClass(),
        "double": NumberTypeClass(),
        "long": NumberTypeClass(),
        "boolean": BooleanTypeClass(),
        "bytes": BytesTypeClass(),
        "null": NullTypeClass(),
    }
    return SchemaFieldDataTypeClass(type=mapping.get(avro_type, StringTypeClass()))


def _pg_schema_fields(flat_fields: list) -> list[SchemaFieldClass]:
    """Build DataHub schema fields for a PostgreSQL table from flattened Avro fields."""
    stamp = _audit_stamp()
    result = []
    # Use only top-level fields (no dot in path) for a realistic SQL table schema
    top_level = [f for f in flat_fields if "." not in f["field_path"]]
    if not top_level:
        top_level = flat_fields[:20]  # fallback: first 20 flat fields

    for field in top_level:
        result.append(
            SchemaFieldClass(
                fieldPath=field["field_path"],
                type=_avro_type_to_datahub(field["avro_type"]),
                nativeDataType=field.get("pg_type", "text"),
                description=field.get("doc", ""),
                lastModified=stamp,
            )
        )
    return result


def _looker_schema_fields(flat_fields: list) -> list[SchemaFieldClass]:
    """Build a minimal Looker view schema: key dimensions + a synthetic measure."""
    stamp = _audit_stamp()
    # Pick up to 5 top-level string/long fields as dimensions
    dims = [
        f
        for f in flat_fields
        if "." not in f["field_path"] and f["avro_type"] in ("string", "long")
    ][:5]
    fields = []
    for field in dims:
        fields.append(
            SchemaFieldClass(
                fieldPath=field["field_path"],
                type=_avro_type_to_datahub(field["avro_type"]),
                nativeDataType="dimension",
                description=field.get("doc", ""),
                lastModified=stamp,
            )
        )
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


def _snake(name: str) -> str:
    import re
    s = re.sub(r"(?<=[a-z0-9])(?=[A-Z])", "_", name)
    return s.lower()


def emit_message(
    entry: dict,
    avro_schema: dict,
    flat_fields: list,
    emitter: "DatahubRestEmitter | None",
    dry_run: bool,
) -> tuple[str, str, str]:
    """Emit Kafka topic, PostgreSQL table, Looker view, and lineage for one message.

    Returns (kafka_urn, pg_urn, looker_urn).
    """
    message_id = entry["id"]
    family = entry["family"]
    business_area = entry["business_area"]
    name = entry["name"]
    description = entry["description"]
    db = BUSINESS_AREA_DB.get(business_area, "data_db")
    snake_name = _snake(name)

    kafka_topic = f"iso20022.{family}.{message_id}"
    pg_table = f"{db}.public.{snake_name}"
    looker_view = f"{business_area}_analytics.{snake_name}_view"

    kafka_urn = make_dataset_urn(platform="kafka", name=kafka_topic, env=ENV)
    pg_urn = make_dataset_urn(platform="postgres", name=pg_table, env=ENV)
    looker_urn = make_dataset_urn(platform="looker", name=looker_view, env=ENV)

    mcps = []

    # --- Kafka topic ---
    stamp = _audit_stamp()
    try:
        kafka_fields = avro_schema_to_mce_fields(
            avro_schema=avro_schema,
            is_key_schema=False,
            default_nullable=True,
        )
    except Exception:
        kafka_fields = []

    mcps.append(
        MetadataChangeProposalWrapper(
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
        )
    )
    mcps.append(
        MetadataChangeProposalWrapper(
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
        )
    )

    # --- PostgreSQL table ---
    pg_fields = _pg_schema_fields(flat_fields)
    mcps.append(
        MetadataChangeProposalWrapper(
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
        )
    )
    mcps.append(
        MetadataChangeProposalWrapper(
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
        )
    )

    # --- Looker view ---
    looker_fields = _looker_schema_fields(flat_fields)
    mcps.append(
        MetadataChangeProposalWrapper(
            entityUrn=looker_urn,
            aspect=DatasetPropertiesClass(
                name=f"{snake_name}_view",
                description=f"Looker LookML view over {db}.public.{snake_name} for {business_area} analytics.",
                customProperties={
                    "iso20022_id": message_id,
                    "source_table": pg_table,
                    "looker_model": f"{business_area}_analytics",
                },
            ),
        )
    )
    mcps.append(
        MetadataChangeProposalWrapper(
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
        )
    )

    # --- Lineage: Kafka -> PostgreSQL ---
    kafka_upstream = UpstreamClass(
        dataset=kafka_urn,
        type=DatasetLineageTypeClass.TRANSFORMED,
    )
    # Field-level lineage for top-level fields that appear in both schemas
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
    mcps.append(
        MetadataChangeProposalWrapper(
            entityUrn=pg_urn,
            aspect=UpstreamLineageClass(
                upstreams=[kafka_upstream],
                fineGrainedLineages=fine_grained,
            ),
        )
    )

    # --- Lineage: PostgreSQL -> Looker ---
    pg_upstream = UpstreamClass(
        dataset=pg_urn,
        type=DatasetLineageTypeClass.VIEW,
    )
    mcps.append(
        MetadataChangeProposalWrapper(
            entityUrn=looker_urn,
            aspect=UpstreamLineageClass(upstreams=[pg_upstream]),
        )
    )

    if dry_run:
        for mcp in mcps:
            print(f"    [dry-run] would emit {mcp.entityUrn} / {type(mcp.aspect).__name__}")
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

    emitter = None if dry_run else DatahubRestEmitter(gms_server=gms_url, token=gms_token or None)

    ok = 0
    failed = 0
    for entry in manifest:
        message_id = entry["id"]
        avsc_path = os.path.join(AVRO_DIR, f"{message_id}.avsc")
        fields_path = os.path.join(AVRO_DIR, f"{message_id}.fields.json")

        if not os.path.exists(avsc_path):
            print(f"  SKIP    {message_id} (no .avsc -- run xsd_to_avro.py first)")
            failed += 1
            continue

        with open(avsc_path) as fh:
            avro_schema_dict = json.load(fh)

        try:
            import avro.schema as avro_schema_mod
            avro_parsed = avro_schema_mod.parse(json.dumps(avro_schema_dict))
        except Exception:
            avro_parsed = avro_schema_dict  # avro_schema_to_mce_fields also accepts dict

        flat_fields = []
        if os.path.exists(fields_path):
            with open(fields_path) as fh:
                flat_fields = json.load(fh)

        action = "DRY-RUN" if dry_run else "EMIT"
        print(f"  {action}    {message_id}")
        try:
            kafka_urn, pg_urn, looker_urn = emit_message(
                entry, avro_parsed, flat_fields, emitter, dry_run
            )
            ok += 1
            if dry_run:
                print(f"    kafka:    {kafka_urn}")
                print(f"    postgres: {pg_urn}")
                print(f"    looker:   {looker_urn}")
        except Exception as exc:
            print(f"    FAILED: {exc}")
            failed += 1

    print(f"\nDone: {ok} messages emitted, {failed} failed.")
    if failed:
        sys.exit(1)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Emit ISO 20022 pipeline entities to DataHub.")
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print what would be emitted without contacting DataHub.",
    )
    args = parser.parse_args()
    main(dry_run=args.dry_run)
