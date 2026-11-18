#!/usr/bin/env bash
set -euo pipefail
umask 077
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
[[ "${EUID}" -eq 0 ]] || die "run as root on the isolated staging Control Plane host through an approved SSM session"

region="" db_host="" master_secret_arn="" app_secret_arn="" migrator_secret_arn=""
ca_file="/etc/zeus/rds/global-bundle.pem"
runtime_output="/etc/zeus/database-runtime.env"
migrate_output="/etc/zeus/database-migrate.env"
while (($#)); do
  case "$1" in
    --region) (($# >= 2)) || die "--region requires a value"; region="$2"; shift 2 ;;
    --db-host) (($# >= 2)) || die "--db-host requires a value"; db_host="$2"; shift 2 ;;
    --master-secret-arn) (($# >= 2)) || die "--master-secret-arn requires a value"; master_secret_arn="$2"; shift 2 ;;
    --app-secret-arn) (($# >= 2)) || die "--app-secret-arn requires a value"; app_secret_arn="$2"; shift 2 ;;
    --migrator-secret-arn) (($# >= 2)) || die "--migrator-secret-arn requires a value"; migrator_secret_arn="$2"; shift 2 ;;
    --ca-file) (($# >= 2)) || die "--ca-file requires a value"; ca_file="$2"; shift 2 ;;
    --help) printf '%s\n' "Usage: $0 --region REGION --db-host RDS_ENDPOINT --master-secret-arn ARN --app-secret-arn ARN --migrator-secret-arn ARN [--ca-file PATH]"; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ -n "$region" && -n "$db_host" && -n "$master_secret_arn" && -n "$app_secret_arn" && -n "$migrator_secret_arn" ]] || die "region, DB host, and all three secret ARNs are required"
[[ "$region" =~ ^[a-z]{2}(-gov)?-[a-z]+-[0-9]+$ ]] || die "invalid AWS region format"
[[ "$db_host" =~ ^[A-Za-z0-9.-]+$ && "$db_host" == *.rds.amazonaws.com ]] || die "DB host must be the exact RDS endpoint hostname"
[[ "$ca_file" =~ ^/[A-Za-z0-9._/-]+$ ]] || die "CA file path must be absolute and contain only URL-safe path characters"
[[ -r "$ca_file" && -s "$ca_file" ]] || die "RDS CA bundle is missing or unreadable: $ca_file"
[[ -d /etc/zeus ]] || die "/etc/zeus must exist before running this script"
[[ ! -e "$runtime_output" && ! -e "$migrate_output" ]] || die "one or both database environment files already exist; preserve and review them before rotation"
for c in aws jq psql mktemp ln rm chmod chown; do command -v "$c" >/dev/null 2>&1 || die "required command not found: $c"; done

master_json="$(aws secretsmanager get-secret-value --region "$region" --secret-id "$master_secret_arn" --query SecretString --output text)"
app_json="$(aws secretsmanager get-secret-value --region "$region" --secret-id "$app_secret_arn" --query SecretString --output text)"
migrator_json="$(aws secretsmanager get-secret-value --region "$region" --secret-id "$migrator_secret_arn" --query SecretString --output text)"
master_username="$(jq -er '.username | strings | select(length > 0)' <<<"$master_json")"
master_password="$(jq -er '.password | strings | select(length > 0)' <<<"$master_json")"
master_engine="$(jq -er '.engine | strings | select(length > 0)' <<<"$master_json")"
master_host="$(jq -er '.host | strings | select(length > 0)' <<<"$master_json")"
master_port="$(jq -er '.port | tostring' <<<"$master_json")"
app_username="$(jq -er '.username | strings | select(length > 0)' <<<"$app_json")"
app_password="$(jq -er '.password | strings | select(length > 0)' <<<"$app_json")"
migrator_username="$(jq -er '.username | strings | select(length > 0)' <<<"$migrator_json")"
migrator_password="$(jq -er '.password | strings | select(length > 0)' <<<"$migrator_json")"
[[ "$master_username" == "zeus_stage_admin" ]] || die "master secret is not for the expected staging database user"
[[ "$master_engine" == "postgres" && "$master_host" == "$db_host" && "$master_port" == "5432" ]] || die "master secret metadata does not match the supplied PostgreSQL RDS endpoint"
[[ "$app_username" == "zeus_app" && "$migrator_username" == "zeus_migrator" ]] || die "database secret usernames do not match the expected roles"
[[ "$app_password" =~ ^[A-Za-z0-9]{40}$ && "$migrator_password" =~ ^[A-Za-z0-9]{40}$ ]] || die "generated role passwords must be 40 alphanumeric characters"

# RDS master credentials are passed only through PGPASSWORD. Generated passwords
# are validated as alphanumeric before being interpolated into SQL.
PGPASSWORD="$master_password" psql --no-psqlrc --set=ON_ERROR_STOP=1 --quiet \
  "host=$db_host port=5432 dbname=zeus_staging user=$master_username sslmode=verify-full sslrootcert=$ca_file connect_timeout=10" <<SQL
SELECT format('CREATE ROLE zeus_migrator LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT', '$migrator_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'zeus_migrator')
\\gexec
SELECT format('ALTER ROLE zeus_migrator WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT', '$migrator_password')
\\gexec
SELECT format('CREATE ROLE zeus_app LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT', '$app_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'zeus_app')
\\gexec
SELECT format('ALTER ROLE zeus_app WITH LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT', '$app_password')
\\gexec
GRANT CONNECT ON DATABASE zeus_staging TO zeus_migrator, zeus_app;
GRANT USAGE, CREATE ON SCHEMA public TO zeus_migrator;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO zeus_app;
REVOKE CREATE ON SCHEMA public FROM zeus_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO zeus_app;
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO zeus_app;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO zeus_app;
ALTER ROLE zeus_migrator SET search_path = public;
ALTER ROLE zeus_app SET search_path = public;
SQL

# Set default privileges as the role that will own future migration-created objects.
PGPASSWORD="$migrator_password" psql --no-psqlrc --set=ON_ERROR_STOP=1 --quiet \
  "host=$db_host port=5432 dbname=zeus_staging user=zeus_migrator sslmode=verify-full sslrootcert=$ca_file connect_timeout=10" <<'SQL'
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO zeus_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO zeus_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO zeus_app;
SQL

temporary_dir="$(mktemp -d /etc/zeus/.database-bootstrap.XXXXXX)"
runtime_linked=0 migrate_linked=0
cleanup() {
  if (( runtime_linked )); then rm -f "$runtime_output"; fi
  if (( migrate_linked )); then rm -f "$migrate_output"; fi
  [[ -z "$temporary_dir" ]] || rm -rf "$temporary_dir"
}
trap cleanup EXIT
printf 'ZEUS_DATABASE_URL=postgres://zeus_app:%s@%s:5432/zeus_staging?sslmode=verify-full&sslrootcert=%s\n' "$app_password" "$db_host" "$ca_file" > "$temporary_dir/runtime.env"
printf 'ZEUS_DATABASE_URL=postgres://zeus_migrator:%s@%s:5432/zeus_staging?sslmode=verify-full&sslrootcert=%s\n' "$migrator_password" "$db_host" "$ca_file" > "$temporary_dir/migrate.env"
chmod 0600 "$temporary_dir/runtime.env" "$temporary_dir/migrate.env"
chown root:root "$temporary_dir/runtime.env" "$temporary_dir/migrate.env"
ln -- "$temporary_dir/runtime.env" "$runtime_output" || die "$runtime_output appeared during bootstrap; no existing file was replaced"
runtime_linked=1
ln -- "$temporary_dir/migrate.env" "$migrate_output" || die "$migrate_output appeared during bootstrap; rollback of the first link will be attempted"
migrate_linked=1
runtime_linked=0 migrate_linked=0
rm -rf "$temporary_dir"
temporary_dir=""
trap - EXIT
unset master_json app_json migrator_json master_password app_password migrator_password
printf 'PASS: zeus_app and zeus_migrator are configured with separate credentials\n'
printf 'PASS: runtime and migration environment files are root-owned mode 0600\n'
printf 'NOTE: generated passwords were not printed\n'
