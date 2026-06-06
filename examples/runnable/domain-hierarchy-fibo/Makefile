.PHONY: fibo-deps fibo-data fibo-data-provisional fibo-update clean-fibo

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
