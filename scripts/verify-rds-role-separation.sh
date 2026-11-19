#!/usr/bin/env bash
set -euo pipefail
umask 077

die() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

[[ "${EUID}" -eq 0 ]] || die "run as root on the isolated staging Control Plane host"
runtime_file="/etc/zeus/database-runtime.env"
migrate_file="/etc/zeus/database-migrate.env"
[[ -r "$runtime_file" && -r "$migrate_file" ]] || die "both database environment files must exist and be readable"
command -v psql >/dev/null 2>&1 || die "psql is required"
command -v mktemp >/dev/null 2>&1 || die "mktemp is required"

runtime_url="$(awk -F= '$1 == "ZEUS_DATABASE_URL" { sub(/^[^=]*=/, ""); print; exit }' "$runtime_file")"
migrate_url="$(awk -F= '$1 == "ZEUS_DATABASE_URL" { sub(/^[^=]*=/, ""); print; exit }' "$migrate_file")"
[[ -n "$runtime_url" && -n "$migrate_url" ]] || die "ZEUS_DATABASE_URL is missing from a generated file"

# The bootstrap script emits this deliberately narrow URI format with 40-character
# alphanumeric passwords. Parse it so passwords never enter psql argv.
parse_database_url() {
  local value="$1"
  [[ "$value" =~ ^postgres://([A-Za-z0-9_]+):([A-Za-z0-9]{40})@([A-Za-z0-9.-]+):([0-9]+)/([A-Za-z0-9_]+)\?sslmode=verify-full\&sslrootcert=([A-Za-z0-9_./-]+)$ ]] || return 1
  parsed_user="${BASH_REMATCH[1]}"
  parsed_password="${BASH_REMATCH[2]}"
  parsed_host="${BASH_REMATCH[3]}"
  parsed_port="${BASH_REMATCH[4]}"
  parsed_database="${BASH_REMATCH[5]}"
  parsed_ca="${BASH_REMATCH[6]}"
  [[ "$parsed_host" == *.rds.amazonaws.com && "$parsed_port" == "5432" && "$parsed_database" == "zeus_staging" ]] || return 1
  [[ -r "$parsed_ca" && -s "$parsed_ca" ]] || return 1
}

parse_database_url "$runtime_url" || die "runtime database URL has an unexpected format or invalid RDS/TLS settings"
runtime_user="$parsed_user"
runtime_password="$parsed_password"
runtime_host="$parsed_host"
runtime_port="$parsed_port"
runtime_database="$parsed_database"
runtime_ca="$parsed_ca"

parse_database_url "$migrate_url" || die "migration database URL has an unexpected format or invalid RDS/TLS settings"
migrate_user="$parsed_user"
migrate_password="$parsed_password"
migrate_host="$parsed_host"
migrate_port="$parsed_port"
migrate_database="$parsed_database"
migrate_ca="$parsed_ca"

[[ "$runtime_user" == "zeus_app" && "$migrate_user" == "zeus_migrator" ]] || die "database URLs do not use the expected runtime and migration roles"
[[ "$runtime_host" == "$migrate_host" && "$runtime_port" == "$migrate_port" && "$runtime_database" == "$migrate_database" && "$runtime_ca" == "$migrate_ca" ]] || die "runtime and migration URLs do not target the same RDS database and CA bundle"

temporary_dir="$(mktemp -d /run/zeus-db-role-check.XXXXXX)"
cleanup() {
  unset runtime_password migrate_password
  [[ -z "${temporary_dir:-}" ]] || rm -rf -- "$temporary_dir"
}
trap cleanup EXIT

pgpass_file="$temporary_dir/pgpass"
printf '%s:%s:%s:%s:%s\n' "$runtime_host" "$runtime_port" "$runtime_database" "$runtime_user" "$runtime_password" > "$pgpass_file"
printf '%s:%s:%s:%s:%s\n' "$migrate_host" "$migrate_port" "$migrate_database" "$migrate_user" "$migrate_password" >> "$pgpass_file"
chmod 0600 "$pgpass_file"
chown root:root "$pgpass_file"
export PGPASSFILE="$pgpass_file"
unset runtime_url migrate_url runtime_password migrate_password parsed_password

runtime_conn="host=$runtime_host port=$runtime_port dbname=$runtime_database user=$runtime_user sslmode=verify-full sslrootcert=$runtime_ca connect_timeout=10"
migrate_conn="host=$migrate_host port=$migrate_port dbname=$migrate_database user=$migrate_user sslmode=verify-full sslrootcert=$migrate_ca connect_timeout=10"

runtime_identity="$(psql "$runtime_conn" --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align --command='SELECT current_user')"
[[ "$runtime_identity" == "zeus_app" ]] || die "runtime connection did not authenticate as zeus_app"

runtime_privileges="$(psql "$runtime_conn" --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="
SELECT has_schema_privilege(current_user, 'public', 'USAGE')
  AND NOT has_schema_privilege(current_user, 'public', 'CREATE')
  AND NOT EXISTS (
    SELECT 1
    FROM pg_namespace n,
         LATERAL aclexplode(COALESCE(n.nspacl, acldefault('n', n.nspowner))) acl
    WHERE n.nspname = 'public'
      AND acl.grantee = 0
      AND acl.privilege_type = 'CREATE'
  )
  AND NOT pg_has_role(current_user, 'zeus_migrator', 'MEMBER')
  AND NOT (SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls OR rolinherit FROM pg_roles WHERE rolname = current_user)
  AND NOT has_database_privilege(current_user, current_database(), 'CREATE')
  AND NOT EXISTS (
    SELECT 1 FROM pg_auth_members m
    JOIN pg_roles member ON member.oid = m.member
    WHERE member.rolname IN ('zeus_app', 'zeus_migrator')
  )
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
        AND NOT has_sequence_privilege(current_user, format('%I.%I', s.schemaname, s.sequencename), 'UPDATE')
      )
  )
  AND NOT EXISTS (
    SELECT 1 FROM pg_tables t WHERE t.schemaname = 'public' AND t.tableowner <> 'zeus_migrator'
  )
")"
[[ "$runtime_privileges" == "t" ]] || die "runtime role has unexpected schema or table privileges"

runtime_ddl_error="$temporary_dir/runtime-ddl.stderr"
if psql "$runtime_conn" --no-psqlrc --set=ON_ERROR_STOP=1 --command='CREATE TABLE public.zeus_runtime_privilege_probe (id integer)' >/dev/null 2>"$runtime_ddl_error"; then
  psql "$migrate_conn" --no-psqlrc --set=ON_ERROR_STOP=1 --command='DROP TABLE IF EXISTS public.zeus_runtime_privilege_probe' >/dev/null 2>&1 || true
  die "zeus_app unexpectedly created a table"
fi
grep -Fq 'permission denied for schema public' "$runtime_ddl_error" || die "runtime DDL probe failed for a reason other than the expected schema permission denial"

migrate_identity="$(psql "$migrate_conn" --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align --command='SELECT current_user')"
[[ "$migrate_identity" == "zeus_migrator" ]] || die "migration connection did not authenticate as zeus_migrator"

migrate_privileges="$(psql "$migrate_conn" --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align --command="
SELECT has_schema_privilege(current_user, 'public', 'USAGE')
  AND has_schema_privilege(current_user, 'public', 'CREATE')
  AND NOT (SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls OR rolinherit FROM pg_roles WHERE rolname = current_user)
  AND NOT has_database_privilege(current_user, current_database(), 'CREATE')
  AND NOT EXISTS (
    SELECT 1 FROM pg_auth_members m
    JOIN pg_roles member ON member.oid = m.member
    WHERE member.rolname IN ('zeus_app', 'zeus_migrator')
  )
")"
[[ "$migrate_privileges" == "t" ]] || die "migration role cannot create schema objects"

psql "$migrate_conn" --no-psqlrc --set=ON_ERROR_STOP=1 --command='BEGIN; CREATE TABLE public.zeus_migrator_privilege_probe (id integer); ROLLBACK' >/dev/null
printf '%s\n' "PASS: zeus_app has runtime privileges and cannot create tables" "PASS: zeus_migrator can create schema objects" "PASS: runtime and migration credentials are distinct" "PASS: database passwords are supplied through a root-only PGPASSFILE, not process arguments"
