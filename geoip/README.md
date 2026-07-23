# JCM GeoIP

`jcm-geoip` is a shared, signed GeoIP lookup service for B2C projects. It reads
local DB-IP Lite City and ASN MMDB files, so a lookup does not consume a
commercial API request or delay storefront page rendering. DB-IP Lite is
distributed under CC BY-SA 4.0; retain provider attribution in internal service
documentation and dashboards that display this data.

## Start

Set the following values in `.env` on the server. Do not commit them.

```bash
JCM_GEOIP_HMAC_SECRET=<long-random-secret>
JCM_GEOIP_ALLOWED_PROJECTS=vebou,another-store
DBIP_UPDATE_INTERVAL_HOURS=720
```

Create the persistent data directory and start the profile:

```bash
mkdir -p geoip/data
docker compose --profile geoip up -d --build jcm-geoip dbip-update
```

For an existing WordPress stack that should not have its main Compose file
changed, copy this directory to `/root/jcm-geoip`, create a local `.env`, and
run the standalone deployment file. It joins the existing `wpdo5_default`
network by default so the existing Nginx container can reach `jcm-geoip`.

```bash
mkdir -p /root/jcm-geoip/data
cd /root/jcm-geoip
docker compose -f docker-compose.production.yml up -d --build
```

The DB-IP Lite updater downloads the current month immediately, then refreshes
it on the configured interval. After the City and ASN files are present, copy
`nginx/geoip-vhost.conf.example` to `website/http.d/ip.dnooo.com.conf`, obtain
the certificate, and reload Nginx.

## Request signing

Only server-side callers may query the service. The request body is JSON and
must include a public IP address. The caller sends these headers:

```text
X-JCM-Project: vebou
X-JCM-Timestamp: 2026-07-22T22:00:00Z
X-JCM-Nonce: random-unique-value
X-JCM-Signature: hex(HMAC-SHA256(secret, project + "\n" + timestamp + "\n" + nonce + "\n" + sha256(body)))
```

`POST /v1/lookup` accepts `{"ip":"203.0.113.1"}`. `POST /v1/lookup/batch`
accepts `{"ips":["203.0.113.1"]}`. The service rejects private addresses,
expired signatures, replayed nonces, unsigned calls, and excessive traffic.

The service only returns GeoIP data. It has no public arbitrary-IP endpoint and
does not log the request body. Store exact IP values in the commerce analytics
layer only when that project has explicitly enabled encrypted IP retention.
