# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
"""
Convert ISO 20022 XSD schemas to Avro schema JSON and a flat fields list.

Reads:   .iso-cache/xsd/{id}.xsd  and  .iso-cache/manifest.json
Writes:  .iso-cache/avro/{id}.avsc       -- Avro schema (record)
         .iso-cache/avro/{id}.fields.json -- flat [{field_path, avro_type, doc}]

Usage:
    python3 scripts/iso20022/xsd_to_avro.py [--force]
"""

import argparse
import json
import os
import re
import sys

try:
    import xmlschema
except ImportError:
    print("ERROR: xmlschema not installed. Run: pip install xmlschema")
    sys.exit(1)

CACHE_DIR = ".iso-cache"
XSD_DIR = os.path.join(CACHE_DIR, "xsd")
AVRO_DIR = os.path.join(CACHE_DIR, "avro")
MANIFEST_PATH = os.path.join(CACHE_DIR, "manifest.json")

MAX_DEPTH = 3  # expand nested types to this depth; collapse beyond

# XSD built-in type -> Avro type
XSD_TYPE_MAP = {
    "string": "string",
    "token": "string",
    "normalizedString": "string",
    "NMTOKEN": "string",
    "ID": "string",
    "IDREF": "string",
    "anyURI": "string",
    "QName": "string",
    "language": "string",
    "decimal": "double",
    "float": "double",
    "double": "double",
    "integer": "long",
    "long": "long",
    "int": "long",
    "short": "long",
    "byte": "long",
    "nonNegativeInteger": "long",
    "positiveInteger": "long",
    "unsignedLong": "long",
    "unsignedInt": "long",
    "boolean": "boolean",
    "base64Binary": "bytes",
    "hexBinary": "bytes",
    "date": "string",
    "dateTime": "string",
    "time": "string",
    "duration": "string",
    "gYear": "string",
    "gYearMonth": "string",
    "gMonth": "string",
    "gMonthDay": "string",
    "gDay": "string",
    "anyType": "string",
    "anySimpleType": "string",
}

# Map Avro type back to a representative PostgreSQL native type for emit_entities
AVRO_TO_PG = {
    "string": "text",
    "double": "numeric(19,4)",
    "long": "bigint",
    "boolean": "boolean",
    "bytes": "bytea",
    "null": "text",
}


def _doc_text(component) -> str:
    """Extract xs:documentation text from an XSD component."""
    try:
        if hasattr(component, "annotation") and component.annotation:
            docs = []
            for child in component.annotation:
                if hasattr(child, "text") and child.text:
                    docs.append(child.text.strip())
            return " ".join(docs)
    except Exception:
        pass
    return ""


def _safe_name(name: str) -> str:
    """Convert an XSD element name to a valid Avro field name."""
    name = re.sub(r"[^A-Za-z0-9_]", "_", name)
    if name and name[0].isdigit():
        name = "_" + name
    return name or "_field"


def _xsd_type_to_avro(xsd_type, depth: int, seen_types: set) -> object:
    """Recursively convert an xmlschema type to an Avro type descriptor."""
    if depth > MAX_DEPTH:
        return {"type": "string", "doc": "(nested content truncated for demo)"}

    local_name = getattr(xsd_type, "local_name", None) or ""

    # Primitive / simple types
    if hasattr(xsd_type, "primitive_type") and xsd_type.primitive_type is not None:
        primitive = getattr(xsd_type.primitive_type, "local_name", "string")
        avro = XSD_TYPE_MAP.get(primitive, "string")
        # Enum restriction
        if hasattr(xsd_type, "enumeration") and xsd_type.enumeration:
            symbols = [_safe_name(str(s)) for s in xsd_type.enumeration]
            avro_name = _safe_name(local_name) if local_name else "Enum"
            return {"type": "enum", "name": avro_name, "symbols": symbols or ["UNKNOWN"]}
        return avro

    # Check built-in fallback
    if local_name in XSD_TYPE_MAP:
        return XSD_TYPE_MAP[local_name]

    # Complex type -> Avro record
    if hasattr(xsd_type, "content") or hasattr(xsd_type, "attributes"):
        # Guard against infinite recursion via recursive type references
        type_key = id(xsd_type)
        if type_key in seen_types:
            return "string"
        seen_types = seen_types | {type_key}

        avro_name = _safe_name(local_name) if local_name else f"Record{depth}"
        fields = _extract_fields(xsd_type, depth, seen_types)
        if not fields:
            return "string"
        return {"type": "record", "name": avro_name, "fields": fields}

    return "string"


def _extract_fields(xsd_type, depth: int, seen_types: set) -> list:
    """Extract Avro fields from an XSD complex type."""
    fields = []
    try:
        elements = list(xsd_type.content_type_label if hasattr(xsd_type, "content_type_label") else [])
    except Exception:
        elements = []

    # Use iter_elements() when available -- covers sequence, choice, all
    try:
        elements = list(xsd_type.iter_elements())
    except Exception:
        try:
            elements = list(xsd_type.content)
        except Exception:
            elements = []

    seen_names: set = set()
    for elem in elements:
        elem_name = getattr(elem, "local_name", None) or getattr(elem, "name", None)
        if not elem_name:
            continue

        avro_field_name = _safe_name(elem_name)
        # Deduplicate (choice groups can repeat names)
        if avro_field_name in seen_names:
            continue
        seen_names.add(avro_field_name)

        elem_type = getattr(elem, "type", None)
        if elem_type is None:
            avro_type: object = "string"
        else:
            avro_type = _xsd_type_to_avro(elem_type, depth + 1, seen_types)

        optional = getattr(elem, "min_occurs", 1) == 0
        repeated = (getattr(elem, "max_occurs", 1) or 1) > 1

        if repeated:
            avro_type = {"type": "array", "items": avro_type}
        if optional:
            avro_type = ["null", avro_type]

        field: dict = {"name": avro_field_name, "type": avro_type}
        doc = _doc_text(elem)
        if doc:
            field["doc"] = doc
        if optional:
            field["default"] = None

        fields.append(field)

    return fields


def _flatten_fields(avro_schema: dict, prefix: str = "") -> list:
    """Walk the Avro schema and return a flat list of {field_path, avro_type, doc}."""
    result = []
    if not isinstance(avro_schema, dict) or avro_schema.get("type") != "record":
        return result

    for field in avro_schema.get("fields", []):
        name = field["name"]
        path = f"{prefix}.{name}" if prefix else name
        raw_type = field["type"]

        # Unwrap nullable union
        actual_type = raw_type
        if isinstance(raw_type, list):
            non_null = [t for t in raw_type if t != "null"]
            actual_type = non_null[0] if non_null else "null"

        # Unwrap array
        if isinstance(actual_type, dict) and actual_type.get("type") == "array":
            actual_type = actual_type["items"]

        if isinstance(actual_type, dict) and actual_type.get("type") == "record":
            # Recurse into nested records
            result.extend(_flatten_fields(actual_type, prefix=path))
        elif isinstance(actual_type, dict) and actual_type.get("type") == "enum":
            result.append(
                {
                    "field_path": path,
                    "avro_type": "string",
                    "pg_type": "text",
                    "doc": field.get("doc", ""),
                    "is_enum": True,
                    "enum_symbols": actual_type.get("symbols", []),
                }
            )
        else:
            type_str = actual_type if isinstance(actual_type, str) else str(actual_type)
            result.append(
                {
                    "field_path": path,
                    "avro_type": type_str,
                    "pg_type": AVRO_TO_PG.get(type_str, "text"),
                    "doc": field.get("doc", ""),
                    "is_enum": False,
                }
            )
    return result


def process_xsd(entry: dict, force: bool) -> bool:
    message_id = entry["id"]
    xsd_path = entry["xsd_path"]
    avsc_path = os.path.join(AVRO_DIR, f"{message_id}.avsc")
    fields_path = os.path.join(AVRO_DIR, f"{message_id}.fields.json")

    if not force and os.path.exists(avsc_path) and os.path.exists(fields_path):
        print(f"  CACHED  {message_id}")
        return True

    print(f"  PARSE   {message_id} ...", end=" ", flush=True)
    try:
        schema = xmlschema.XMLSchema(xsd_path)
    except Exception as exc:
        print(f"FAILED (parse: {exc})")
        return False

    # Find the top-level Document element (ISO 20022 convention)
    root_elem = None
    for name, elem in schema.elements.items():
        if name == "Document":
            root_elem = elem
            break
    if root_elem is None:
        # Fall back to first element
        for name, elem in schema.elements.items():
            root_elem = elem
            break

    if root_elem is None:
        print("FAILED (no root element)")
        return False

    message_name = entry["name"]
    avro_record: dict = {
        "type": "record",
        "name": message_name,
        "namespace": f"iso20022.{entry['family']}",
        "doc": entry["description"],
        "fields": [],
    }

    root_type = getattr(root_elem, "type", None)
    if root_type is not None:
        avro_record["fields"] = _extract_fields(root_type, depth=1, seen_types=set())

    if not avro_record["fields"]:
        # Fallback: single opaque bytes field
        avro_record["fields"] = [
            {"name": "raw_content", "type": "bytes", "doc": "Raw ISO 20022 message content."}
        ]

    with open(avsc_path, "w") as fh:
        json.dump(avro_record, fh, indent=2)

    flat = _flatten_fields(avro_record)
    with open(fields_path, "w") as fh:
        json.dump(flat, fh, indent=2)

    print(f"OK ({len(avro_record['fields'])} top-level fields, {len(flat)} flat fields)")
    return True


def main(force: bool = False) -> None:
    if not os.path.exists(MANIFEST_PATH):
        print(f"ERROR: {MANIFEST_PATH} not found. Run download.py first.")
        sys.exit(1)

    with open(MANIFEST_PATH) as fh:
        manifest = json.load(fh)

    os.makedirs(AVRO_DIR, exist_ok=True)

    ok = 0
    failed = 0
    for entry in manifest:
        if process_xsd(entry, force):
            ok += 1
        else:
            failed += 1

    print(f"\nDone: {ok} converted, {failed} failed.")
    if failed:
        sys.exit(1)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Convert ISO 20022 XSDs to Avro schemas.")
    parser.add_argument("--force", action="store_true", help="Reconvert even if cached.")
    args = parser.parse_args()
    main(force=args.force)
