#!/usr/bin/env bash
set -euo pipefail
die() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
[[ "${EUID}" -eq 0 ]] || die "run as root on the isolated staging Control Plane host"
runtime_file="/etc/zeus/database-runtime.env"
migrate_file="/etc/zeus/database-migrate.env"
[[ -r "$runtime_file" && -r "$migrate_file" ]] || die "both database environment files must exist and be readable"
command -v psql >/dev/null 2>&1 || die "psql is required"
runtime_url="$(awk -F= '$1 == "ZEUS_DATABASE_URL" { sub(/^[^=]*=/, ""); print; exit }' "$runtime_file")"
migrate_url="$(awk -F= '$1 == "ZEUS_DATABASE_URL" { sub(/^[^=]*=/, ""); print; exit }' "$migrate_file")"
[[ -n "$runtime_url" && -n "$migrate_url" ]] || die "ZEUS_DATABASE_URL is missing from a generated file"
runtime_identity="$(psql "$runtime_url" --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align --command='SELECT current_user')"
[[ "$runtime_identity" == "zeus_app" ]] || die "runtime connection did not authenticate as zeus_app"
runtime_privileges="$(psql "$runtime_url" --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="
SELECT has_schema_privilege(current_user, 'public', 'USAGE')
  AND NOT has_schema_privilege(current_user, 'public', 'CREATE')
  AND NOT pg_has_role(current_user, 'zeus_migrator', 'MEMBER')
  AND NOT (SELECT rolsuper OR rolcreatedb OR rolcreaterole FROM pg_roles WHERE rolname = current_user)
  AND NOT EXISTS (
    SELECT 1 FROM pg_tables t WHERE t.schemaname = 'public'
      AND NOT (
        has_table_privilege(current_user, format('%I.%I', t.schemaname, t.tablename), 'SELECT')
        AND has_table_privilege(current_user, format('%I.%I', t.schemaname, t.tablename), 'INSERT')
        AND has_table_privilege(current_user, format('%I.%I', t.schemaname, t.tablename), 'UPDATE')
        AND has_table_privilege(current_user, format('%I.%I', t.schemaname, t.tablename), 'DELETE')
      )
  )
  AND NOT EXISTS (
    SELECT 1 FROM pg_sequences s WHERE s.schemaname = 'public'
      AND NOT (
        has_sequence_privilege(current_user, format('%I.%I', s.schemaname, s.sequencename), 'USAGE')
        AND has_sequence_privilege(current_user, format('%I.%I', s.schemaname, s.sequencename), 'SELECT')
        AND has_sequence_privilege(current_user, format('%I.%I', s.schemaname, s.sequencename), 'UPDATE')
      )
  )
  AND NOT EXISTS (
    SELECT 1 FROM pg_tables t WHERE t.schemaname = 'public' AND t.tableowner <> 'zeus_migrator'
  )
")"
[[ "$runtime_privileges" == "t" ]] || die "runtime role has unexpected schema or table privileges"
if psql "$runtime_url" --no-psqlrc --set=ON_ERROR_STOP=1 --command='CREATE TABLE public.zeus_runtime_privilege_probe (id integer)' >/dev/null 2>&1; then
  psql "$runtime_url" --no-psqlrc --command='DROP TABLE IF EXISTS public.zeus_runtime_privilege_probe' >/dev/null 2>&1 || true
  die "zeus_app unexpectedly created a table"
fi
migrate_identity="$(psql "$migrate_url" --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align --command='SELECT current_user')"
[[ "$migrate_identity" == "zeus_migrator" ]] || die "migration connection did not authenticate as zeus_migrator"
migrate_privileges="$(psql "$migrate_url" --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="SELECT has_schema_privilege(current_user, 'public', 'USAGE') AND has_schema_privilege(current_user, 'public', 'CREATE')")"
[[ "$migrate_privileges" == "t" ]] || die "migration role cannot create schema objects"
psql "$migrate_url" --no-psqlrc --set=ON_ERROR_STOP=1 --command='BEGIN; CREATE TABLE public.zeus_migrator_privilege_probe (id integer); ROLLBACK' >/dev/null
printf '%s\n' "PASS: zeus_app has runtime privileges and cannot create tables" "PASS: zeus_migrator can create schema objects" "PASS: runtime and migration credentials are distinct"
