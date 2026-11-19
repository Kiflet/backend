#!/usr/bin/env bash
# Restores a backup made by scripts/backup.sh: the database, the app's
# volume when the files archive is given, and the app's bucket when the
# objects archive is given too. Each replaces what is there now: the bucket
# is made the same as the archive, objects added since included. The app is
# stopped meanwhile.
#
#   scripts/restore.sh /var/backups/kiflet/kiflet_20261104_020000.dump \
#     /var/backups/kiflet/kiflet_20261104_020000_files.tar.gz \
#     /var/backups/kiflet/kiflet_20261104_020000_objects.tar.gz
#
# An encrypted backup (.age) needs AGE_IDENTITY, the file holding one of the
# private keys it was encrypted to. Each file is decrypted before anything is
# replaced, so a wrong key or a damaged copy changes nothing:
#
#   AGE_IDENTITY=~/kiflet-backup.key scripts/restore.sh \
#     kiflet_20261104_020000.dump.age kiflet_20261104_020000_files.tar.gz.age
#
# The settings archive isn't restored here: docs/deploy.md has how, for a
# new machine. COMPOSE_FILE and ENV_FILE choose the stack, as for
# backup.sh. It asks for the stack's name before replacing anything;
# CONFIRM=yes skips the question.
set -euo pipefail
umask 077

cd "$(dirname "$0")/.."
if [ $# -lt 1 ] || [ $# -gt 3 ]; then
  echo "usage: $0 <database.dump[.age]> [<files.tar.gz[.age]> [<objects.tar.gz[.age]>]]" >&2
  exit 2
fi
db="$1"
files="${2:-}"
objects="${3:-}"
encrypted=""
for f in "$db" ${files:+"$files"} ${objects:+"$objects"}; do
  [ -r "$f" ] || { echo "cannot read $f" >&2; exit 1; }
  case "$f" in *.age) encrypted=yes ;; esac
done
export COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker-compose.prod.yml}"
ENV_FILE="${ENV_FILE:-.env}"
compose() { docker compose --env-file "$ENV_FILE" "$@"; }

if [ -n "$encrypted" ]; then
  [ -n "${AGE_IDENTITY:-}" ] || { echo "The backup is encrypted: set AGE_IDENTITY to the file holding its private key." >&2; exit 2; }
  command -v age >/dev/null || { echo "The backup is encrypted, and age isn't installed (apt install age)." >&2; exit 1; }
  # Beside the backup, which has room for it, unlike a /tmp in memory.
  plain="$(mktemp -d "$(dirname "$db")/.restore.XXXXXX")"
  trap 'rm -rf "$plain"' EXIT
  decrypt() {
    case "$1" in
      *.age)
        local out
        out="$plain/$(basename "${1%.age}")"
        age -d -i "$AGE_IDENTITY" -o "$out" "$1" || { echo "Cannot decrypt $1 with $AGE_IDENTITY. Nothing changed." >&2; exit 1; }
        echo "$out"
        ;;
      *) echo "$1" ;;
    esac
  }
  db="$(decrypt "$db")"
  if [ -n "$files" ]; then files="$(decrypt "$files")"; fi
  if [ -n "$objects" ]; then objects="$(decrypt "$objects")"; fi
fi

if [ -n "$objects" ]; then
  # The bucket the app uses, from its own settings, read while it runs.
  app_env() { compose exec -T app printenv "$1" 2>/dev/null || true; }
  [ "$(app_env STORAGE_TYPE)" = s3 ] || { echo "The app isn't running with STORAGE_TYPE=s3, so there is no bucket to restore $3 to. Nothing changed." >&2; exit 1; }
  STORE_S3_ENDPOINT="$(app_env S3_ENDPOINT)" STORE_S3_REGION="$(app_env S3_REGION)"
  STORE_S3_ACCESS_KEY="$(app_env S3_ACCESS_KEY)" STORE_S3_SECRET_KEY="$(app_env S3_SECRET_KEY)"
  store_bucket="$(app_env S3_BUCKET)"
  STACK_NETWORK="$(compose config | sed -n 's/^name: //p')_default"
  export STORE_S3_ENDPOINT STORE_S3_REGION STORE_S3_ACCESS_KEY STORE_S3_SECRET_KEY STACK_NETWORK
  # Unpacked beside the archive before anything is replaced, so a damaged
  # one changes nothing.
  unpacked="$(mktemp -d "$(dirname "$objects")/.objects.XXXXXX")"
  trap 'rm -rf "$unpacked" ${plain:+"$plain"}' EXIT
  tar -C "$unpacked" -xzf "$objects" || { echo "Cannot unpack $3. Nothing changed." >&2; exit 1; }
fi

name="$(compose config | sed -n 's/^name: //p')"
if [ "${CONFIRM:-}" != yes ]; then
  what="the database"
  [ -n "$files" ] && what="the database and the app's files"
  [ -n "$objects" ] && what="the database, the app's files and its bucket"
  read -r -p "This replaces $what of \"$name\" with the backup. Type \"$name\" to go on: " answer
  [ "$answer" = "$name" ] || { echo "Nothing changed."; exit 1; }
fi

# The workers too, with the workers profile: nothing may write meanwhile.
compose stop app worker
# Into a new database, so nothing of the current one stays behind.
compose exec -T postgres psql -U logistics -d postgres -q -v ON_ERROR_STOP=1 \
  -c 'DROP DATABASE IF EXISTS logistics WITH (FORCE)' \
  -c 'CREATE DATABASE logistics OWNER logistics'
compose exec -T postgres pg_restore -U logistics -d logistics --no-owner --exit-on-error <"$db"
if [ -n "$files" ]; then
  # The app's own container has a read-only root; a one-off container of
  # its image writes to the volume.
  compose run --rm --no-deps -T --entrypoint sh app \
    -c 'find /data -mindepth 1 -delete && tar -C /data -xzf -' <"$files"
fi
if [ -n "$objects" ]; then
  COMPOSE_FILE="${BACKUP_COMPOSE_FILE:-deployments/docker-compose.backup.yml}" \
    docker compose --progress quiet --env-file /dev/null run --rm --no-deps -T \
    --user "$(id -u):$(id -g)" -v "$unpacked:/objects:ro" objects sync /objects "store:$store_bucket"
fi
compose start app worker
echo "Restored. The app is starting again: \`docker compose ps\` shows it healthy once it is."
