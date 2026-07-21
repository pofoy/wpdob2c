#!/bin/bash
set -Eeuo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

bash -n install.sh common.sh vhost.sh
docker compose --env-file env.sample config --quiet

if grep -Eq '(3306:3306|11211:11211)' docker-compose.yml; then
    echo "Database or Memcached port must not be published." >&2
    exit 1
fi

if grep -q 'fastcgi_ignore_headers' nginx/nginx.conf; then
    echo "WooCommerce cache safety requires respecting upstream cache headers." >&2
    exit 1
fi

grep -q 'wp_woocommerce_session_' nginx/vhost.conf
grep -q 'if (\$arg_sign_key != "")' nginx/vhost.conf
grep -q 'fastcgi_no_cache \$skip_cache \$upstream_http_set_cookie' nginx/vhost.conf
grep -q 'WP_CACHE_KEY_SALT' vhost.sh
grep -q 'object-cache-4-everyone' vhost.sh
grep -q 'OC4EVERYONE_DISABLE_DISK_CACHE' vhost.sh
grep -q 'wp_using_ext_object_cache' vhost.sh

docker run --rm \
    -v "$ROOT_DIR/nginx/nginx.conf:/usr/local/nginx/conf/nginx.conf:ro" \
    -v "$ROOT_DIR/nginx/vhost.conf:/usr/local/nginx/conf.d/wpdob2c-test.conf:ro" \
    skisscc/nginx:php83 nginx -t

echo "wpdob2c validation passed."
