#!/usr/bin/env python3
"""
Generate a structured JSON hierarchy from the FIBO (Financial Industry
Business Ontology) GitHub repository.

Usage:
    python3 scripts/generate-fibo-data.py [--force] [--repo PATH] [--output PATH]

Outputs .fibo-cache/fibo.json by default. Re-uses an existing output file if
it is less than CACHE_MAX_AGE_DAYS old unless --force is supplied.

License: FIBO is MIT-licensed by the EDM Council.
  Copyright (c) 2020 Enterprise Data Management Council
  https://github.com/edmcouncil/fibo/blob/master/LICENSE
"""

import argparse
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

FIBO_REPO_URL = "https://github.com/edmcouncil/fibo.git"
FIBO_BRANCH = "master"

DEFAULT_REPO_DIR = Path(".fibo-cache/fibo-repo")
DEFAULT_OUTPUT = Path(".fibo-cache/fibo.json")
CACHE_MAX_AGE_DAYS = 7

# Domains that are ontology scaffolding rather than meaningful business areas.
SKIP_DOMAINS = {"FND", "BP"}

# Filename prefixes / substrings to exclude at all levels.
SKIP_PREFIXES = ("All", "Metadata", "About")
SKIP_CONTAINS = ("Individuals",)


# ---------------------------------------------------------------------------
# RDF metadata extraction (regex-based; avoids entity-reference issues)
# ---------------------------------------------------------------------------

def _extract_text(content: str, tag: str) -> str:
    """Return the text content of the first matching XML element, stripped."""
    pattern = rf"<{tag}(?:\s[^>]*)?>(.+?)</{tag}>"
    m = re.search(pattern, content, re.DOTALL)
    return m.group(1).strip() if m else ""


def _extract_attr(content: str, element: str, attr: str) -> str:
    """Return an attribute value from the first matching element."""
    pattern = rf"<{element}(?:\s[^>]*?\s{attr}=\"([^\"]+)\"[^>]*)?\s*/?>|<{element}[^>]*?\s{attr}=\"([^\"]+)\""
    m = re.search(pattern, content)
    if m:
        return m.group(1) or m.group(2) or ""
    # Simpler fallback: just find attr="..." anywhere near the element
    pattern2 = rf"{attr}=\"([^\"]+)\""
    m2 = re.search(pattern2, content)
    return m2.group(1) if m2 else ""


def _version_date(content: str) -> str:
    """Extract YYYY-MM-DD from an owl:versionIRI rdf:resource attribute."""
    # Match the rdf:resource URL containing an 8-digit date segment
    m = re.search(r'rdf:resource="[^"]*?/(\d{8})/', content)
    if m:
        d = m.group(1)
        return f"{d[:4]}-{d[4:6]}-{d[6:]}"
    return ""


def read_metadata(path: Path) -> dict:
    """
    Parse key metadata from an OWL/RDF-XML file.
    Returns a dict with keys: alt_label, label, title, abstract, version.
    """
    if not path.exists():
        return {}
    try:
        content = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return {}

    # Try each name source in priority order.
    alt_label = _extract_text(content, "skos:altLabel")
    label = _extract_text(content, "rdfs:label")
    title = _extract_text(content, "dct:title")
    abstract = _extract_text(content, "dct:abstract")
    version = _version_date(content)

    return {
        "alt_label": alt_label,
        "label": label,
        "title": title,
        "abstract": abstract,
        "version": version,
    }


def best_name(*candidates: str) -> str:
    """Return the first non-empty candidate string."""
    for c in candidates:
        c = c.strip()
        if c:
            return c
    return ""


def strip_ontology_suffix(name: str) -> str:
    """'Bonds Ontology' -> 'Bonds', 'Credit Default Swaps' unchanged."""
    return re.sub(r"\s+[Oo]ntology$", "", name).strip()


def build_description(abstract: str, version: str) -> str:
    """Compose a description from abstract and optional version date."""
    parts = [abstract] if abstract else []
    if version:
        parts.append(f"Version: {version}")
    return "\n\n".join(parts)


def slugify(name: str) -> str:
    """Convert a name to a lowercase hyphenated slug suitable for domain_id."""
    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")


def camel_to_words(stem: str) -> str:
    """'ACTUSTermApplicabilityMapping' -> 'ACTUS Term Applicability Mapping'"""
    # Insert space before uppercase runs that follow lowercase, or before a
    # capital that starts a new word in an all-caps prefix (e.g. ACTUSContract).
    spaced = re.sub(r"(?<=[a-z0-9])(?=[A-Z])|(?<=[A-Z]{2})(?=[A-Z][a-z])", " ", stem)
    return spaced.strip()


def deduplicate_names(leaves: list) -> list:
    """
    Detect name collisions within a leaf list (DataHub enforces unique names
    per parent domain) and fall back to the camelCase-split file stem for any
    conflicting entries. The stem is stored during leaf collection as 'stem'.
    """
    from collections import Counter
    counts = Counter(l["name"] for l in leaves)
    result = []
    for leaf in leaves:
        if counts[leaf["name"]] > 1:
            # Two or more leaves share this name -- use the filename as fallback
            stem_name = strip_ontology_suffix(camel_to_words(leaf.get("stem", leaf["id"])))
            leaf = {**leaf, "name": stem_name}
        result.append(leaf)
    return result


# ---------------------------------------------------------------------------
# Filesystem walk
# ---------------------------------------------------------------------------

def should_skip_file(filename: str) -> bool:
    """Return True if this .rdf filename should be excluded."""
    stem = filename[:-4] if filename.endswith(".rdf") else filename
    if any(stem.startswith(p) for p in SKIP_PREFIXES):
        return True
    if any(s in stem for s in SKIP_CONTAINS):
        return True
    return False


def process_domain(domain_code: str, repo_dir: Path) -> dict | None:
    """Build domain dict including its modules and leaves."""
    if domain_code in SKIP_DOMAINS:
        return None

    domain_dir = repo_dir / domain_code
    if not domain_dir.is_dir():
        return None

    meta_path = domain_dir / f"Metadata{domain_code}.rdf"
    meta = read_metadata(meta_path)

    name = best_name(meta.get("alt_label", ""), meta.get("title", ""), domain_code)
    description = build_description(meta.get("abstract", ""), meta.get("version", ""))

    modules = []
    # Collect module directories (subdirectories that are not hidden/special)
    for item in sorted(domain_dir.iterdir()):
        if not item.is_dir() or item.name.startswith("."):
            continue
        module = process_module(domain_code, item.name, item)
        if module:
            modules.append(module)

    # If no modules found, treat direct .rdf files as leaves (e.g. ACTUS)
    if not modules:
        leaves = collect_leaves(domain_dir)
        if leaves:
            modules = [{
                "id": slugify(domain_code),
                "name": domain_code,
                "description": "",
                "leaves": leaves,
            }]

    if not modules:
        return None  # Empty domain, skip

    return {
        "id": slugify(domain_code),
        "code": domain_code,
        "name": name,
        "description": description,
        "modules": modules,
    }


def process_module(domain_code: str, module_name: str, module_dir: Path) -> dict | None:
    """Build module dict including its leaf ontology nodes."""
    # Try to find metadata file: Metadata{DOMAIN}{MODULE}.rdf
    meta_filename = f"Metadata{domain_code}{module_name}.rdf"
    meta_path = module_dir / meta_filename
    meta = read_metadata(meta_path)

    # For modules, prefer directory name as the display name (clean and readable)
    # since skos:altLabel is typically absent at this level.
    title_short = ""
    if meta.get("title"):
        # "FIBO SEC Debt Module" -> extract just the module part if possible
        m = re.match(r"FIBO\s+\w+\s+(.+?)\s+Module\b", meta["title"])
        title_short = m.group(1) if m else meta["title"]

    name = best_name(title_short, module_name)
    description = build_description(meta.get("abstract", ""), meta.get("version", ""))

    leaves = collect_leaves(module_dir)
    if not leaves:
        return None  # Nothing here, skip

    return {
        "id": slugify(module_name),
        "name": name,
        "description": description,
        "leaves": leaves,
    }


def collect_leaves(directory: Path) -> list:
    """Collect leaf ontology nodes from .rdf files in a directory."""
    leaves = []
    for f in sorted(directory.iterdir()):
        if not f.is_file() or not f.name.endswith(".rdf"):
            continue
        if should_skip_file(f.name):
            continue
        meta = read_metadata(f)
        raw_label = best_name(meta.get("label", ""), meta.get("title", ""), f.stem)
        name = strip_ontology_suffix(raw_label)
        description = meta.get("abstract", "")
        leaves.append({
            "id": slugify(f.stem),
            "stem": f.stem,  # kept for collision resolution
            "name": name,
            "description": description,
        })
    return deduplicate_names(leaves)


# ---------------------------------------------------------------------------
# Repo management
# ---------------------------------------------------------------------------

def ensure_repo(repo_dir: Path) -> None:
    """Shallow-clone the FIBO repo if it doesn't already exist."""
    if repo_dir.exists():
        print(f"Reusing existing clone at {repo_dir}", file=sys.stderr)
        return
    repo_dir.parent.mkdir(parents=True, exist_ok=True)
    print(f"Shallow-cloning FIBO from {FIBO_REPO_URL} ...", file=sys.stderr)
    subprocess.run(
        ["git", "clone", "--depth", "1", "--branch", FIBO_BRANCH, FIBO_REPO_URL, str(repo_dir)],
        check=True,
    )
    print("Clone complete.", file=sys.stderr)


def cache_is_fresh(output: Path, max_age_days: int) -> bool:
    if not output.exists():
        return False
    age = datetime.now() - datetime.fromtimestamp(output.stat().st_mtime)
    return age.days < max_age_days


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--force", action="store_true", help="Regenerate even if cache is fresh")
    parser.add_argument("--repo", type=Path, default=DEFAULT_REPO_DIR, help="Local FIBO repo path")
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT, help="Output JSON path")
    args = parser.parse_args()

    if not args.force and cache_is_fresh(args.output, CACHE_MAX_AGE_DAYS):
        print(f"Cache is fresh (< {CACHE_MAX_AGE_DAYS} days old). Use --force to regenerate.", file=sys.stderr)
        print(f"Output: {args.output}", file=sys.stderr)
        return

    ensure_repo(args.repo)
    repo_dir = args.repo

    print("Processing FIBO metadata ...", file=sys.stderr)

    # Root
    root_meta = read_metadata(repo_dir / "MetadataFIBO.rdf")
    root_name = best_name(root_meta.get("alt_label", ""), root_meta.get("title", ""), "FIBO")
    root_desc = build_description(root_meta.get("abstract", ""), root_meta.get("version", ""))

    # Domains
    domains = []
    for item in sorted(repo_dir.iterdir()):
        if not item.is_dir() or item.name.startswith("."):
            continue
        domain = process_domain(item.name, repo_dir)
        if domain:
            domains.append(domain)
            print(f"  {domain['code']}: {len(domain['modules'])} modules, "
                  f"{sum(len(m['leaves']) for m in domain['modules'])} leaves", file=sys.stderr)

    total_leaves = sum(len(m["leaves"]) for d in domains for m in d["modules"])
    total_modules = sum(len(d["modules"]) for d in domains)
    print(f"Total: {len(domains)} domains, {total_modules} modules, {total_leaves} leaves", file=sys.stderr)

    output = {
        "generated": datetime.now(timezone.utc).isoformat(),
        "source": "https://github.com/edmcouncil/fibo",
        "license": "MIT - Copyright (c) 2020 Enterprise Data Management Council",
        "root": {
            "id": "fibo",
            "name": root_name,
            "description": root_desc,
            "domains": domains,
        },
    }

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(output, indent=2, ensure_ascii=False), encoding="utf-8")
    print(f"Written: {args.output}", file=sys.stderr)


if __name__ == "__main__":
    main()
