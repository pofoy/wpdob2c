#!/bin/bash
set -Eeuo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "$ROOT_DIR/env.sample"
PREFIX="wpdob2c-smoke-$$"
NETWORK="$PREFIX"
MEMCACHED_CONTAINER="$PREFIX-memcached"
DB_CONTAINER="$PREFIX-db"
WEB_CONTAINER="$PREFIX-web"
SITE_DIR=$(mktemp -d)

cleanup() {
    if docker inspect "$WEB_CONTAINER" >/dev/null 2>&1; then
        docker exec "$WEB_CONTAINER" chown -R "$(id -u):$(id -g)" \
            /wwwroot/smoke.test >/dev/null 2>&1 || true
    fi
    docker rm -f "$WEB_CONTAINER" "$DB_CONTAINER" "$MEMCACHED_CONTAINER" >/dev/null 2>&1 || true
    docker network rm "$NETWORK" >/dev/null 2>&1 || true
    rm -rf "$SITE_DIR"
}
trap cleanup EXIT

docker network create "$NETWORK" >/dev/null
docker run -d --name "$MEMCACHED_CONTAINER" --network "$NETWORK" --network-alias memcached \
    memcached:1.6-alpine memcached -m 64 -I 2m -c 128 >/dev/null
docker run -d --name "$DB_CONTAINER" --network "$NETWORK" --network-alias mysql \
    -e MARIADB_ROOT_PASSWORD=smoke-root \
    -e MARIADB_DATABASE=wordpress \
    -e MARIADB_USER=wordpress \
    -e MARIADB_PASSWORD=smoke-pass \
    mariadb:11.4 >/dev/null
docker run -d --name "$WEB_CONTAINER" --network "$NETWORK" \
    -v "$SITE_DIR:/wwwroot/smoke.test" \
    -v "$ROOT_DIR/php83/php.ini:/usr/local/etc/php/php.ini:ro" \
    skisscc/nginx:php83 >/dev/null

for _attempt in $(seq 1 45); do
    if docker exec "$DB_CONTAINER" mariadb-admin ping -uroot -psmoke-root --silent >/dev/null 2>&1; then
        break
    fi
    sleep 2
done
docker exec "$DB_CONTAINER" mariadb-admin ping -uroot -psmoke-root --silent >/dev/null

WP=(docker exec "$WEB_CONTAINER" wp --path=/wwwroot/smoke.test --allow-root)
"${WP[@]}" core download --locale=en_US --quiet
"${WP[@]}" config create \
    --dbname=wordpress --dbuser=wordpress --dbpass=smoke-pass --dbhost=mysql:3306 --quiet
"${WP[@]}" core install \
    --url=https://smoke.test --title=Smoke --admin_user=admin \
    --admin_password=smoke-password --admin_email=admin@smoke.test --skip-email --quiet
"${WP[@]}" plugin install woocommerce --version="$WOOCOMMERCE_VERSION" --activate --quiet
"${WP[@]}" plugin is-active woocommerce
"${WP[@]}" plugin install object-cache-4-everyone \
    --version="$OBJECT_CACHE_PLUGIN_VERSION" --force --quiet
"${WP[@]}" config set WP_CACHE true --raw --type=constant --quiet
"${WP[@]}" config set WP_CACHE_KEY_SALT smoke.test: --type=constant --quiet
"${WP[@]}" config set OC4EVERYONE_MEMCACHED_SERVER \
    memcached:11211 --type=constant --quiet
"${WP[@]}" config set OC4EVERYONE_DISABLE_DISK_CACHE \
    true --raw --type=constant --quiet

docker exec "$WEB_CONTAINER" php -r '
    $source = "/wwwroot/smoke.test/wp-content/plugins/object-cache-4-everyone/object-cache-memcached-template.php";
    $target = "/wwwroot/smoke.test/wp-content/object-cache.php";
    $template = file_get_contents($source);
    if ($template === false) {
        exit(1);
    }
    $header = "<?php\n/**\n * Plugin Name: Object Cache 4 everyone - Memcached\n */\n?>";
    $template = $header . $template;
    $template .= "\ndefine(\"OC4EVERYONE_PREDEFINED_SERVER\", \"memcached:11211\");\n";
    if (file_put_contents($target, $template) === false) {
        exit(1);
    }
'
"${WP[@]}" plugin activate object-cache-4-everyone --quiet
"${WP[@]}" plugin is-active object-cache-4-everyone

docker exec "$WEB_CONTAINER" php -r '
    $cache = new Memcached();
    $cache->addServer("memcached", 11211);
    if (!$cache->set("wpdob2c-smoke", "ok", 30) || $cache->get("wpdob2c-smoke") !== "ok") {
        exit(1);
    }
    $cache->delete("wpdob2c-smoke");
'
"${WP[@]}" eval '
    if (!wp_using_ext_object_cache()) {
        exit(1);
    }
    if (!wp_cache_set("wpdob2c-smoke", "persistent", "wpdob2c", 30)) {
        exit(1);
    }
'
"${WP[@]}" eval '
    $found = false;
    $value = wp_cache_get("wpdob2c-smoke", "wpdob2c", false, $found);
    if (!$found || $value !== "persistent") {
        exit(1);
    }
    wp_cache_delete("wpdob2c-smoke", "wpdob2c");
'

echo "Memcached object-cache smoke test passed."
