.PHONY: fibo-deps fibo-data fibo-data-provisional fibo-update clean-fibo \
        iso-deps iso-data iso-avro iso-emit iso-assertions-config iso-tag \
        iso-assertion-results iso-all clean-iso \
        .iso-cache/xsd .iso-cache/avro

# Install Python dependencies required for glossary term extraction.
# Run this once before fibo-data (fibo-data depends on it automatically).
fibo-deps:
	python3 -m pip install --quiet -r requirements.txt

# Generate (or reuse cached) FIBO hierarchy + glossary JSON.
# Run this once before terraform apply. The clone is ~40-60 MB and lives in
# .fibo-cache/ (gitignored). Provisional-maturity terms are excluded by
# default; use fibo-data-provisional to include them.
fibo-data: fibo-deps .fibo-cache/fibo.json

.fibo-cache/fibo.json: scripts/generate-fibo-data.py .fibo-cache/fibo-repo
	python3 scripts/generate-fibo-data.py --repo .fibo-cache/fibo-repo --output .fibo-cache/fibo.json

.fibo-cache/fibo-repo:
	@mkdir -p .fibo-cache
	git clone --depth 1 --branch master \
	  https://github.com/edmcouncil/fibo.git .fibo-cache/fibo-repo

# Generate including Provisional-maturity terms (larger glossary).
fibo-data-provisional: fibo-deps .fibo-cache/fibo-repo
	python3 scripts/generate-fibo-data.py \
	  --repo .fibo-cache/fibo-repo \
	  --output .fibo-cache/fibo.json \
	  --force \
	  --include-provisional

# Force regeneration without re-cloning (e.g. after script changes).
fibo-update: fibo-deps
	python3 scripts/generate-fibo-data.py --repo .fibo-cache/fibo-repo --output .fibo-cache/fibo.json --force

# Remove the cache entirely. Next fibo-data run will re-clone.
clean-fibo:
	rm -rf .fibo-cache

# -----------------------------------------------------------------------
# ISO 20022 financial pipeline demo
# -----------------------------------------------------------------------
# Install Python dependencies (shared with fibo-deps; both call pip install).
iso-deps:
	python3 -m pip install --quiet -r requirements.txt

# Download ISO 20022 XSD schemas from iso20022.org into .iso-cache/xsd/.
# Uses the business-area bulk download endpoint which returns a zip per family.
# Schemas are cached for 30 days; pass FORCE=1 to bypass: make iso-data FORCE=1
.iso-cache/xsd:
	python3 scripts/iso20022/download.py $(if $(FORCE),--force,)

iso-data: iso-deps .iso-cache/xsd

# Convert downloaded XSDs to Avro schemas in .iso-cache/avro/.
.iso-cache/avro: scripts/iso20022/xsd_to_avro.py .iso-cache/xsd
	python3 scripts/iso20022/xsd_to_avro.py $(if $(FORCE),--force,)

iso-avro: iso-deps .iso-cache/avro

# Emit Kafka / PostgreSQL / Looker entities and lineage to DataHub.
# Requires: DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN env vars.
iso-emit: iso-deps iso-avro
	python3 scripts/iso20022/emit_entities.py

# Generate .iso-cache/assertions_config.json from the Avro field cache.
# Run after iso-avro and before 'terraform apply' (assertions.tf reads it).
# The output is a gitignored generated artifact, not committed.
iso-assertions-config: iso-deps .iso-cache/avro
	python3 scripts/iso20022/generate_assertions_config.py

# LLM-based FIBO domain and glossary term tagging.
# Requires: ANTHROPIC_API_KEY, DATAHUB_GMS_URL, DATAHUB_GMS_TOKEN env vars.
# Run 'make fibo-data' first so FIBO context is available.
iso-tag: iso-deps
	python3 scripts/iso20022/auto_tag.py $(if $(FORCE),--force,)

# Emit synthetic assertion run results so DataHub Observe shows evaluated
# assertions (mostly passing, with a deliberate few failing). Run AFTER
# 'terraform apply' has created the assertions. Requires DATAHUB_GMS_URL
# and DATAHUB_GMS_TOKEN.
iso-assertion-results: iso-deps
	python3 scripts/iso20022/emit_assertion_results.py

# Run the full ISO 20022 pipeline end-to-end.
iso-all: iso-deps iso-data iso-avro iso-emit iso-assertions-config iso-tag

# Remove the ISO 20022 cache entirely.
clean-iso:
	rm -rf .iso-cache
