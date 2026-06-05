.PHONY: fibo-data fibo-update clean-fibo

# Generate (or reuse cached) FIBO hierarchy JSON. Run this once before
# terraform apply. The clone is ~40 MB and lives in .fibo-cache/ (gitignored).
fibo-data: .fibo-cache/fibo.json

.fibo-cache/fibo.json: scripts/generate-fibo-data.py .fibo-cache/fibo-repo
	python3 scripts/generate-fibo-data.py --repo .fibo-cache/fibo-repo --output .fibo-cache/fibo.json

.fibo-cache/fibo-repo:
	@mkdir -p .fibo-cache
	git clone --depth 1 --branch master \
	  https://github.com/edmcouncil/fibo.git .fibo-cache/fibo-repo

# Force regeneration without re-cloning (e.g. after script changes).
fibo-update:
	python3 scripts/generate-fibo-data.py --repo .fibo-cache/fibo-repo --output .fibo-cache/fibo.json --force

# Remove the cache entirely. Next fibo-data run will re-clone.
clean-fibo:
	rm -rf .fibo-cache
