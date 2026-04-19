#!/bin/sh
set -e
export PGHOST="${PGHOST:-postgres}"
export PGUSER="${PGUSER:-bond_user}"
export PGPASSWORD="${PGPASSWORD:-bond_pass}"
export PGDATABASE="${PGDATABASE:-bond_bot}"

echo "[entrypoint] Waiting for PostgreSQL..."
until pg_isready -h "$PGHOST" -U "$PGUSER" -d "$PGDATABASE"; do sleep 1; done

echo "[entrypoint] Applying migrations..."
psql "postgres://${PGUSER}:${PGPASSWORD}@${PGHOST}:5432/${PGDATABASE}?sslmode=disable" -f /app/migrations/001_init.sql

echo "[entrypoint] Starting server..."
exec "$@"
