#!/bin/bash
# backfill-embedding-model.sh
# Adds embedding_model column and backfills existing memories with the current model name.
# Usage: ./scripts/backfill-embedding-model.sh [model_name]
# Default model: BAAI/bge-base-en-v1.5

set -e

MODEL="${1:-BAAI/bge-base-en-v1.5}"
DB="${HOME}/.lth/memory.db"

if [ ! -f "$DB" ]; then
  echo "Database not found at $DB"
  exit 1
fi

echo "Stopping lth daemon..."
"${HOME}/bin/lth" watch stop 2>/dev/null || true
sleep 1

echo "Running migration and backfill on $DB..."
echo "  Model: $MODEL"

python3 - <<EOF
import sqlite3

db = sqlite3.connect("$DB")
cur = db.cursor()

# Add column if not exists (idempotent)
try:
    cur.execute("ALTER TABLE memories ADD COLUMN embedding_model TEXT NOT NULL DEFAULT ''")
    print("  Added embedding_model column")
except sqlite3.OperationalError as e:
    if "duplicate column name" in str(e):
        print("  Column already exists, skipping ALTER")
    else:
        raise

# Backfill all rows that have embeddings with the model name
cur.execute("""
UPDATE memories
SET embedding_model = ?
WHERE (embedding_model = '' OR embedding_model IS NULL)
  AND (embedding IS NOT NULL AND length(embedding) > 0)
""", ("$MODEL",))
updated = cur.rowcount
print(f"  Backfilled {updated} rows with model '$MODEL'")

# Count rows without embeddings (those stay empty string — correct)
cur.execute("SELECT COUNT(*) FROM memories WHERE embedding IS NULL OR length(embedding) = 0")
no_emb = cur.fetchone()[0]
print(f"  {no_emb} rows have no embedding (left as '')")

db.commit()
db.close()
print("Done.")
EOF

echo "Restarting daemon..."
"${HOME}/bin/lth" stats > /dev/null 2>&1
echo "Daemon restarted."
