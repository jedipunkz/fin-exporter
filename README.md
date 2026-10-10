# fin-exporter

米国株・指数・為替・投資信託の価格を Prometheus メトリクスとして公開する exporter。API キー不要。

## データソース

| 種別 | 取得元 | 更新頻度 | 備考 |
|---|---|---|---|
| 米国株 | Cboe delayed quotes | 15 分遅延 | 非公開 API（cboe.com の相場ページが使う JSON）。予告なく変わりうる |
| 指数 (VIX, TNX 等) | Cboe delayed quotes | 15 分遅延 | 同上。TNX は利回り x 10（52.44 = 5.244%） |
| 為替 | [Frankfurter](https://frankfurter.dev/) | 日次 (ECB 参照レート) | |
| 投資信託 | [投信総合検索ライブラリー](https://toushin-lib.fwg.ne.jp/) | 日次 | 基準価額（1 万口あたり、円） |

## メトリクス

| 名前 | ラベル |
|---|---|
| `fin_stock_price_usd` | `symbol` |
| `fin_index_value` | `symbol` |
| `fin_forex_rate` | `base`, `quote` |
| `fin_fund_nav_jpy` | `name`, `isin` |
| `fin_fetch_success` | `kind`, `id` |

取得に失敗した場合は前回値を保持し、`fin_fetch_success` を 0 にする。一度も成功していない系列は出力しない。

## 設定

`fin-exporter.yaml`:

```yaml
listen: 127.0.0.1:9101
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

| キー | 説明 |
|---|---|
| `listen` | 既定 `127.0.0.1:9101`。loopback 以外を指定する場合は `web_config_file` が必須 |
| `web_config_file` | basic auth / TLS 設定。形式は [exporter-toolkit web configuration](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md) |
| `interval` | 取得間隔。既定 `5m`、最小 `1m` |
| `stocks[].symbol` / `indices[].symbol` | 英大文字と数字のみ。指数は `^` を付けない |
| `forex[].base` / `quote` | ISO 4217 通貨コード。対応通貨は `https://api.frankfurter.dev/v1/currencies` |
| `funds[].isin` / `code` | ISIN と協会コード。投信総合検索ライブラリーのファンド詳細で確認できる。`name` はラベル用の任意の名前 |

## 実行

### バイナリ

```bash
go build -o fin-exporter .
./fin-exporter -config fin-exporter.yaml
curl -s 127.0.0.1:9101/metrics | grep '^fin_'
```

### Docker Compose

```bash
docker compose up -d --build
curl -s 127.0.0.1:9101/metrics | grep '^fin_'
```

`network_mode: host` で起動し、ホストの `127.0.0.1:9101` で listen する。同じホストの Prometheus から次の設定で scrape する。

```yaml
scrape_configs:
  - job_name: fin-exporter
    scrape_interval: 1m  # 値の更新は interval (既定 5m) ごと
    static_configs:
      - targets: ["localhost:9101"]
```

別ホストやコンテナ内の Prometheus から scrape する場合は、`listen: 0.0.0.0:9101` と `web_config_file`（basic auth / TLS）を設定する。

## テスト

```bash
go test ./...
```
