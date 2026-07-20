# wpdob2c

Single-server WordPress deployment stack for B2C WooCommerce sites. It combines
Nginx/PHP 8.3, MySQL, Certbot, WooCommerce-safe FastCGI caching, and a private
Memcached network with per-site persistent object-cache configuration.

## Install

Run these commands from the directory where the `wpdob2c/` folder should be
created:

```bash
sudo -i
apt update && apt install git -y
git clone --depth 1 https://github.com/pofoy/wpdob2c.git wpdob2c
bash wpdob2c/install.sh
```

The installer preserves an existing `.env` and MySQL password when rerun.

## Create A Store

```bash
vhost.sh
```

Choose **Create site** and enable WordPress installation. A fresh store receives:

- WooCommerce at the pinned version in `.env`.
- Object Cache 4 everyone backed by Memcached, with disk fallback disabled and
  a domain-specific `WP_CACHE_KEY_SALT`.
- Production WordPress debug and file-editor defaults.
- Post-name permalinks and an object-cache connectivity check.

Use **Configure/check B2C site** for a restored or uploaded WordPress site.

## Cache Safety

Anonymous `GET`/`HEAD` storefront pages may use FastCGI cache. Cart, Checkout,
My Account, order endpoints, REST/Store API, webhooks, POST/query requests,
logged-in users, and WooCommerce session/cart cookies always bypass it. Responses
that set cookies or prohibit caching are not stored.

MySQL and Memcached are not published to the host network. phpMyAdmin is bound
to `127.0.0.1:8081`; access it remotely through an SSH tunnel.

## Validate Changes

```bash
bash tests/validate.sh
```

An optional Docker smoke test creates an isolated temporary WordPress site and
verifies a real Memcached-backed object-cache drop-in:

```bash
bash tests/object-cache-smoke.sh
```

## Upstream

This repository is adapted from [mina998/wpdo5](https://github.com/mina998/wpdo5)
for WooCommerce storefront deployments.
