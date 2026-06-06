#!/usr/bin/env python3
"""
Generate a structured JSON hierarchy from the FIBO (Financial Industry
Business Ontology) GitHub repository.

Usage:
    python3 scripts/generate-fibo-data.py [--force] [--repo PATH] [--output PATH]
                                          [--include-provisional]

Outputs .fibo-cache/fibo.json by default. Re-uses an existing output file if
it is less than CACHE_MAX_AGE_DAYS old unless --force is supplied.

The script extracts two layers of FIBO content:

  Domain hierarchy (regex-based, no extra deps):
    Domain -> Module -> Leaf ontology node

  Glossary terms (rdflib-based):
    Each leaf ontology .rdf file is parsed for owl:Class definitions. Each
    class becomes a glossary term nested under its leaf node. Run
    `make fibo-deps` first to install rdflib.

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
# Optional rdflib import (required for glossary term extraction)
# ---------------------------------------------------------------------------

try:
    from rdflib import Graph, RDF, RDFS, OWL, URIRef
    from rdflib.namespace import SKOS
    RDFLIB_AVAILABLE = True
except ImportError:
    RDFLIB_AVAILABLE = False

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

# Maximum length of a datahub_glossary_term term_id (DataHub GlossaryTermUrn).
TERM_ID_MAX_LEN = 56

# Maturity-level predicate IRIs used across old and new FIBO releases.
_MATURITY_PREDS = (
    "https://www.omg.org/spec/Commons/AnnotationVocabulary/hasMaturityLevel",
    "https://spec.edmcouncil.org/fibo/ontology/FND/Utilities/AnnotationVocabulary/hasMaturityLevel",
)
_PROVISIONAL_URIS = frozenset({
    "https://www.omg.org/spec/Commons/AnnotationVocabulary/Provisional",
    "https://spec.edmcouncil.org/fibo/ontology/FND/Utilities/AnnotationVocabulary/Provisional",
})

# Supplementary annotation predicates that enrich the composed definition string.
_EXPLANATORY_NOTE_PREDS = (
    "https://www.omg.org/spec/Commons/AnnotationVocabulary/explanatoryNote",
    "https://spec.edmcouncil.org/fibo/ontology/FND/Utilities/AnnotationVocabulary/explanatoryNote",
)


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
# OWL class extraction (rdflib-based -- requires `pip install rdflib`)
# ---------------------------------------------------------------------------

def _compose_definition(base: str, notes: list[str], example: str, scope: str) -> str:
    """
    Compose the full definition string from a base definition and supplementary
    FIBO annotations. Returns a single clean string with double-newline paragraph
    separators and short plain-text labels for each supplementary section.

    Plain-text labels ("Note:", "Example:", "Scope:") are used rather than
    Markdown headers so the text renders consistently in both tooltip and
    full-view contexts in the DataHub UI.
    """
    parts = []
    if base:
        parts.append(base)
    for note in notes:
        if note:
            parts.append(f"Note: {note}")
    if example:
        parts.append(f"Example: {example}")
    if scope:
        parts.append(f"Scope: {scope}")
    return "\n\n".join(parts)


def collect_owl_classes(rdf_path: Path, domain_code: str, include_provisional: bool) -> list:
    """
    Extract owl:Class definitions from a single FIBO ontology .rdf file.

    Each class becomes one glossary term dict:
      {"id": "<term_id>", "name": "<rdfs:label>", "definition": "<skos:definition>"}

    The term_id is pre-computed as "tf-fibo-{domain_code_lower}-{local_slug}"
    and is guaranteed to be <= TERM_ID_MAX_LEN characters.

    Maturity filtering: if the ontology file is marked Provisional and
    include_provisional is False, the entire file is skipped (all classes
    within it are treated as provisional). Classes without a maturity
    annotation are always included.
    """
    if not RDFLIB_AVAILABLE:
        return []

    g = Graph()
    try:
        g.parse(str(rdf_path), format="xml")
    except Exception as exc:
        print(f"    [warn] could not parse {rdf_path.name}: {exc}", file=sys.stderr)
        return []

    # Locate the ontology node (subject of rdf:type owl:Ontology).
    ontology_iri = None
    for s in g.subjects(RDF.type, OWL.Ontology):
        ontology_iri = str(s)
        if not include_provisional:
            # Check file-level maturity -- FIBO attaches it to the ontology,
            # not to individual classes.
            for pred_str in _MATURITY_PREDS:
                maturity = g.value(s, URIRef(pred_str))
                if maturity and str(maturity) in _PROVISIONAL_URIS:
                    return []  # entire file is provisional, skip it
        break  # there is exactly one owl:Ontology per file

    if ontology_iri is None:
        return []  # not a proper ontology file

    # Build the term_id prefix for this domain.
    prefix = f"tf-fibo-{domain_code.lower()}-"
    max_slug_len = TERM_ID_MAX_LEN - len(prefix)
    if max_slug_len <= 0:
        return []  # domain code too long to form valid term IDs

    terms = []
    seen_ids: set[str] = set()

    for cls in sorted(g.subjects(RDF.type, OWL.Class)):
        cls_str = str(cls)

        # Skip blank nodes and anonymous class expressions.
        if not cls_str.startswith("http"):
            continue

        # Skip deprecated classes -- they have been superseded and should not
        # appear in the business glossary.
        if g.value(cls, OWL.deprecated):
            continue

        # Only include classes whose IRI is declared in THIS ontology file.
        # Imported classes have IRIs from other namespaces; skip them.
        if not cls_str.startswith(ontology_iri):
            continue

        # Extract the local name from the IRI (after the last '/' or '#').
        local_name = cls_str.rsplit("/", 1)[-1].rsplit("#", 1)[-1]
        if not local_name:
            continue

        # Name: skos:prefLabel -> rdfs:label.
        name_val = g.value(cls, SKOS.prefLabel) or g.value(cls, RDFS.label)
        if not name_val:
            continue
        name = str(name_val).strip()
        if len(name) < 3:
            continue

        # Definition: skos:definition -> rdfs:comment (base definition).
        defn_val = g.value(cls, SKOS.definition) or g.value(cls, RDFS.comment)
        base_defn = str(defn_val).strip() if defn_val else ""

        # Supplementary annotations -- collected and composed into the definition
        # string so DataHub users see richer context without a provider change.
        notes: list[str] = []
        for pred_str in _EXPLANATORY_NOTE_PREDS:
            for note_val in sorted(g.objects(cls, URIRef(pred_str))):
                note = str(note_val).strip()
                if note and note not in notes:
                    notes.append(note)
        example_val = g.value(cls, SKOS.example)
        scope_val = g.value(cls, SKOS.scopeNote)
        definition = _compose_definition(
            base_defn,
            notes,
            str(example_val).strip() if example_val else "",
            str(scope_val).strip() if scope_val else "",
        )

        # Build term_id: truncate the slug to stay within the 56-char limit.
        local_slug = slugify(camel_to_words(local_name))
        term_id = (prefix + local_slug[:max_slug_len]).rstrip("-")

        if term_id in seen_ids:
            # Collision within the same domain -- append a disambiguator from
            # the leaf stem so the id remains unique. This is rare in FIBO.
            leaf_abbr = slugify(rdf_path.stem)[:8]
            candidate = (prefix + leaf_abbr + "-" + local_slug)[: TERM_ID_MAX_LEN].rstrip("-")
            if candidate in seen_ids:
                continue  # give up on this class rather than produce a wrong id
            term_id = candidate

        seen_ids.add(term_id)
        terms.append({"id": term_id, "name": name, "definition": definition})

    return terms


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


def process_domain(domain_code: str, repo_dir: Path, include_provisional: bool) -> dict | None:
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
        module = process_module(domain_code, item.name, item, include_provisional)
        if module:
            modules.append(module)

    # If no modules found, treat direct .rdf files as leaves (e.g. ACTUS)
    if not modules:
        leaves = collect_leaves(domain_dir, domain_code, include_provisional)
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


def process_module(domain_code: str, module_name: str, module_dir: Path, include_provisional: bool) -> dict | None:
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

    leaves = collect_leaves(module_dir, domain_code, include_provisional)
    if not leaves:
        return None  # Nothing here, skip

    return {
        "id": slugify(module_name),
        "name": name,
        "description": description,
        "leaves": leaves,
    }


def collect_leaves(directory: Path, domain_code: str, include_provisional: bool) -> list:
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
        terms = collect_owl_classes(f, domain_code, include_provisional)
        leaves.append({
            "id": slugify(f.stem),
            "stem": f.stem,  # kept for collision resolution
            "name": name,
            "description": description,
            "terms": terms,
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
    parser.add_argument(
        "--include-provisional",
        action="store_true",
        help="Include FIBO classes marked with Provisional maturity in the glossary terms. "
             "By default only Release and unlabelled classes are included, matching the "
             "DataHub FIBO ingestion source default.",
    )
    args = parser.parse_args()

    if not RDFLIB_AVAILABLE:
        print(
            "ERROR: rdflib is not installed. Glossary term extraction requires rdflib.\n"
            "Install it with:  pip3 install rdflib\n"
            "Or run:           make fibo-deps",
            file=sys.stderr,
        )
        sys.exit(1)

    if not args.force and cache_is_fresh(args.output, CACHE_MAX_AGE_DAYS):
        print(f"Cache is fresh (< {CACHE_MAX_AGE_DAYS} days old). Use --force to regenerate.", file=sys.stderr)
        print(f"Output: {args.output}", file=sys.stderr)
        return

    ensure_repo(args.repo)
    repo_dir = args.repo

    include_provisional: bool = args.include_provisional
    prov_note = " (including Provisional)" if include_provisional else ""
    print(f"Processing FIBO metadata and glossary terms{prov_note} ...", file=sys.stderr)

    # Root
    root_meta = read_metadata(repo_dir / "MetadataFIBO.rdf")
    root_name = best_name(root_meta.get("alt_label", ""), root_meta.get("title", ""), "FIBO")
    root_desc = build_description(root_meta.get("abstract", ""), root_meta.get("version", ""))

    # Domains
    domains = []
    for item in sorted(repo_dir.iterdir()):
        if not item.is_dir() or item.name.startswith("."):
            continue
        domain = process_domain(item.name, repo_dir, include_provisional)
        if domain:
            domains.append(domain)
            n_leaves = sum(len(m["leaves"]) for m in domain["modules"])
            n_terms = sum(len(l["terms"]) for m in domain["modules"] for l in m["leaves"])
            print(f"  {domain['code']}: {len(domain['modules'])} modules, "
                  f"{n_leaves} leaves, {n_terms} terms", file=sys.stderr)

    total_leaves = sum(len(m["leaves"]) for d in domains for m in d["modules"])
    total_modules = sum(len(d["modules"]) for d in domains)
    total_terms = sum(len(l["terms"]) for d in domains for m in d["modules"] for l in m["leaves"])
    print(
        f"Total: {len(domains)} domains, {total_modules} modules, "
        f"{total_leaves} leaves, {total_terms} glossary terms",
        file=sys.stderr,
    )

    output = {
        "generated": datetime.now(timezone.utc).isoformat(),
        "source": "https://github.com/edmcouncil/fibo",
        "license": "MIT - Copyright (c) 2020 Enterprise Data Management Council",
        "include_provisional": include_provisional,
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
