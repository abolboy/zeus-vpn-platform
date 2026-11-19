#!/usr/bin/env bash
set -euo pipefail
umask 077

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

if [[ "${EUID}" -ne 0 ]]; then
  die "run as root on the isolated staging Control Plane host through an approved SSM session"
fi

region=""
db_host=""
master_secret_arn=""
app_secret_arn=""
ca_file="/etc/zeus/rds/global-bundle.pem"
output_file="/etc/zeus/database.env"

while (($#)); do
  case "$1" in
    --region) (($# >= 2)) || die "--region requires a value"; region="$2"; shift 2 ;;
    --db-host) (($# >= 2)) || die "--db-host requires a value"; db_host="$2"; shift 2 ;;
    --master-secret-arn) (($# >= 2)) || die "--master-secret-arn requires a value"; master_secret_arn="$2"; shift 2 ;;
    --app-secret-arn) (($# >= 2)) || die "--app-secret-arn requires a value"; app_secret_arn="$2"; shift 2 ;;
    --ca-file) (($# >= 2)) || die "--ca-file requires a value"; ca_file="$2"; shift 2 ;;
    --help)
      printf '%s\n' "Usage: $0 --region REGION --db-host RDS_ENDPOINT --master-secret-arn ARN --app-secret-arn ARN [--ca-file PATH]"
      exit 0
      ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ -n "$region" && -n "$db_host" && -n "$master_secret_arn" && -n "$app_secret_arn" ]] || die "region, DB host, and both secret ARNs are required"
[[ "$region" =~ ^[a-z]{2}(-gov)?-[a-z]+-[0-9]+$ ]] || die "invalid AWS region format"
[[ "$db_host" =~ ^[A-Za-z0-9.-]+$ && "$db_host" == *.rds.amazonaws.com ]] || die "DB host must be the exact RDS endpoint hostname"
[[ "$ca_file" =~ ^/[A-Za-z0-9._/-]+$ ]] || die "CA file path must be absolute and contain only URL-safe path characters"
[[ -r "$ca_file" && -s "$ca_file" ]] || die "RDS CA bundle is missing or unreadable: $ca_file"
[[ -d /etc/zeus ]] || die "/etc/zeus must exist before running this script"
[[ ! -e "$output_file" ]] || die "$output_file already exists; preserve it and review before replacing it"
for command_name in aws jq psql mktemp mv chmod chown; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

master_json="$(aws secretsmanager get-secret-value --region "$region" --secret-id "$master_secret_arn" --query SecretString --output text)"
app_json="$(aws secretsmanager get-secret-value --region "$region" --secret-id "$app_secret_arn" --query SecretString --output text)"
master_username="$(jq -er '.username | strings | select(length > 0)' <<<"$master_json")"
master_password="$(jq -er '.password | strings | select(length > 0)' <<<"$master_json")"
app_username="$(jq -er '.username | strings | select(length > 0)' <<<"$app_json")"
app_password="$(jq -er '.password | strings | select(length > 0)' <<<"$app_json")"

[[ "$master_username" =~ ^[A-Za-z][A-Za-z0-9_]{0,15}$ ]] || die "unexpected RDS master username format"
[[ "$app_username" == "zeus_app" ]] || die "application secret username must be zeus_app"
[[ "$app_password" =~ ^[A-Za-z0-9]{40}$ ]] || die "application secret password does not match the expected 40-character alphanumeric format"

# The app password is generated without punctuation and validated above before
# being inserted into SQL. The master password is passed only through PGPASSWORD.
PGPASSWORD="$master_password" psql --no-psqlrc --set=ON_ERROR_STOP=1 --quiet \
  "host=$db_host port=5432 dbname=zeus_staging user=$master_username sslmode=verify-full sslrootcert=$ca_file connect_timeout=10" <<SQL
SELECT format('CREATE ROLE zeus_app LOGIN PASSWORD %L', '$app_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'zeus_app')
\\gexec
SELECT format('ALTER ROLE zeus_app WITH LOGIN PASSWORD %L', '$app_password')
\\gexec
GRANT CONNECT ON DATABASE zeus_staging TO zeus_app;
GRANT USAGE, CREATE ON SCHEMA public TO zeus_app;
ALTER ROLE zeus_app SET search_path = public;
SQL

temporary_file="$(mktemp /etc/zeus/.database.env.XXXXXX)"
trap 'rm -f "$temporary_file"' EXIT
printf 'ZEUS_DATABASE_URL=postgres://zeus_app:%s@%s:5432/zeus_staging?sslmode=verify-full&sslrootcert=%s\n' \
  "$app_password" "$db_host" "$ca_file" > "$temporary_file"
chmod 0600 "$temporary_file"
chown root:root "$temporary_file"
mv -- "$temporary_file" "$output_file"
trap - EXIT

unset master_json app_json master_password app_password
printf 'PASS: zeus_app role is ready and %s was written with mode 0600\n' "$output_file"
printf 'NOTE: the generated application password was not printed\n'
