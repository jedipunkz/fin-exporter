# fin-exporter

Prometheus exporter for US stocks, indices, FX rates and Japanese mutual funds. No API key required.

## Data sources

| Kind | Source | Update | Notes |
|---|---|---|---|
| US stocks | Cboe delayed quotes | 15 min delay | Undocumented API (the JSON behind cboe.com quote pages). May change without notice |
| Indices (VIX, TNX, ...) | Cboe delayed quotes | 15 min delay | Same as above. TNX is yield x 10 (52.44 = 5.244%) |
| FX | [Frankfurter](https://frankfurter.dev/) | Daily (ECB reference rates) | |
| Japanese mutual funds | [Toushin Sogo Kensaku Library](https://toushin-lib.fwg.ne.jp/) | Daily | NAV per 10,000 units in JPY |

## Metrics

| Name | Labels |
|---|---|
| `fin_stock_price_usd` | `symbol` |
| `fin_index_value` | `symbol` |
| `fin_forex_rate` | `base`, `quote` |
| `fin_fund_nav_jpy` | `name`, `isin` |
| `fin_fetch_success` | `kind`, `id` |

On a failed fetch the previous value is kept and `fin_fetch_success` is set to 0. A series is not exported until its first successful fetch.

## Configuration

```bash
cp fin-exporter.yaml.example fin-exporter.yaml
```

```yaml
listen: 127.0.0.1:9102
# web_config_file: web-config.yml
interval: 5m

stocks:
  - symbol: SPCX
indices:
  - symbol: VIX
  - symbol: TNX
forex:
  - base: USD
    quote: JPY
funds:
  - name: emaxis-slim-all-country
    isin: JP90C000H1T1
    code: "0331418A"
```

| Key | Description |
|---|---|
| `listen` | Default `127.0.0.1:9102`. `web_config_file` is required for a non-loopback address |
| `web_config_file` | Basic auth / TLS settings. See [exporter-toolkit web configuration](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md) |
| `interval` | Fetch interval. Default `5m`, minimum `1m` |
| `stocks[].symbol` / `indices[].symbol` | Uppercase letters and digits only. No `^` for indices |
| `forex[].base` / `quote` | ISO 4217 currency code. Supported currencies: `https://api.frankfurter.dev/v1/currencies` |
| `funds[].isin` / `code` | ISIN and fund association code, shown on the fund detail page of the Toushin Sogo Kensaku Library. `name` is any label value |

## Run

### Binary

```bash
go build -o fin-exporter .
./fin-exporter -config fin-exporter.yaml
curl -s 127.0.0.1:9102/metrics | grep '^fin_'
```

### Docker Compose

```bash
docker compose up -d --build
curl -s 127.0.0.1:9102/metrics | grep '^fin_'
```

The container runs with `network_mode: host` and listens on the host's `127.0.0.1:9102`. Scrape it from a Prometheus on the same host:

```yaml
scrape_configs:
  - job_name: fin-exporter
    scrape_interval: 1m  # values change every interval (default 5m)
    static_configs:
      - targets: ["localhost:9102"]
```

To scrape from another host or from a Prometheus container, set `listen: 0.0.0.0:9102` and `web_config_file` (basic auth / TLS).

## Test

```bash
go test ./...
```
