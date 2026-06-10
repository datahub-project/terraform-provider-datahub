#!/usr/bin/env python3
# Seed the SQLite test database with N rows.
# Usage: python3 fixtures/seed.py <row_count>
# Default: 150 rows (assertion passes when threshold is >= 100).
import random
import sqlite3
import string
import sys
import pathlib

n = int(sys.argv[1]) if len(sys.argv) > 1 else 150
db = pathlib.Path(__file__).parent / "test.db"

con = sqlite3.connect(db)
con.execute("DROP TABLE IF EXISTS tf_test_data")
con.execute(
    "CREATE TABLE tf_test_data ("
    "  id    INTEGER PRIMARY KEY,"
    "  value TEXT NOT NULL"
    ")"
)
con.executemany(
    "INSERT INTO tf_test_data(value) VALUES (?)",
    [("".join(random.choices(string.ascii_letters, k=8)),) for _ in range(n)],
)
con.commit()
con.close()
print(f"Seeded {n} rows into {db}")
