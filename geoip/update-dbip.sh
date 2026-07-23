#!/bin/sh
set -eu

DATA_DIR="${DBIP_DATA_DIR:-/usr/share/GeoIP}"
INTERVAL_HOURS="${DBIP_UPDATE_INTERVAL_HOURS:-720}"

case "$INTERVAL_HOURS" in
  ''|*[!0-9]*)
    echo "DBIP_UPDATE_INTERVAL_HOURS must be a positive integer" >&2
    exit 1
    ;;
esac

if [ "$INTERVAL_HOURS" -lt 1 ] || [ "$INTERVAL_HOURS" -gt 8760 ]; then
  echo "DBIP_UPDATE_INTERVAL_HOURS must be between 1 and 8760" >&2
  exit 1
fi

mkdir -p "$DATA_DIR"

month_candidates() {
  current_year="$(date -u +%Y)"
  current_month="$(date -u +%m)"
  previous_year="$current_year"
  # BusyBox date does not support GNU date's "last month" syntax. Use expr
  # here because POSIX shell arithmetic treats values such as 08 as octal.
  previous_month="$(expr "$current_month" - 1)"

  if [ "$previous_month" -eq 0 ]; then
    previous_year="$(expr "$current_year" - 1)"
    previous_month=12
  fi

  printf '%s-%s\n' "$current_year" "$current_month"
  printf '%s-%02d\n' "$previous_year" "$previous_month"
}

download_dataset() {
  dataset="$1"
  destination="$2"
  temp_gz="$DATA_DIR/.${dataset}.mmdb.gz.$$"
  temp_mmdb="$DATA_DIR/.${dataset}.mmdb.$$"

  for month in $(month_candidates); do
    url="https://download.db-ip.com/free/dbip-${dataset}-lite-${month}.mmdb.gz"
    if curl --fail --location --retry 3 --connect-timeout 10 --max-time 180 \
      --output "$temp_gz" "$url"; then
      gzip -dc "$temp_gz" > "$temp_mmdb"
      test -s "$temp_mmdb"
      chmod 0644 "$temp_mmdb"
      mv "$temp_mmdb" "$destination"
      rm -f "$temp_gz"
      echo "Updated ${dataset} database from ${month}"
      return 0
    fi
  done

  rm -f "$temp_gz" "$temp_mmdb"
  echo "Unable to download DB-IP Lite ${dataset} database" >&2
  return 1
}

refresh() {
  download_dataset city "$DATA_DIR/GeoLite2-City.mmdb"
  download_dataset asn "$DATA_DIR/GeoLite2-ASN.mmdb"
}

while true; do
  if ! refresh; then
    echo "DB-IP update failed; retaining the last successful database files" >&2
  fi
  sleep "$((INTERVAL_HOURS * 3600))"
done
