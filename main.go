// fin-exporter exposes US stock prices and indices such as VIX / TNX (Cboe
// delayed quotes), FX rates (Frankfurter) and Japanese mutual fund NAVs
// (投信総合検索ライブラリー) as Prometheus metrics. No API key is needed.
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"go.yaml.in/yaml/v3"
)

type config struct {
	Listen        string        `yaml:"listen"`
	WebConfigFile string        `yaml:"web_config_file"`
	Interval      time.Duration `yaml:"interval"`
	Stocks        []struct {
		Symbol string `yaml:"symbol"` // e.g. SPCX, AAPL
	} `yaml:"stocks"`
	Forex []struct {
		Base  string `yaml:"base"`
		Quote string `yaml:"quote"`
	} `yaml:"forex"`
	Funds []struct {
		Name string `yaml:"name"`
		ISIN string `yaml:"isin"`
		Code string `yaml:"code"` // 協会コード
	} `yaml:"funds"`
	Indices []struct {
		Symbol string `yaml:"symbol"` // Cboe index symbol without "^", e.g. VIX, TNX
	} `yaml:"indices"`
}

var (
	stockPrice = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fin_stock_price_usd", Help: "US stock price from Cboe delayed quotes (15 min delay).",
	}, []string{"symbol"})
	forexRate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fin_forex_rate", Help: "Daily ECB reference rate from Frankfurter (1 base = N quote).",
	}, []string{"base", "quote"})
	fundNAV = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fin_fund_nav_jpy", Help: "Latest mutual fund NAV per 10,000 units.",
	}, []string{"name", "isin"})
	indexValue = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fin_index_value", Help: "Index value from Cboe delayed quotes (15 min delay). TNX is yield x 10.",
	}, []string{"symbol"})
	fetchOK = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "fin_fetch_success", Help: "1 if the last fetch succeeded, 0 otherwise.",
	}, []string{"kind", "id"})

	client = &http.Client{Timeout: 15 * time.Second}

	cboeSymbol = regexp.MustCompile(`^[A-Z0-9]+$`)
)

func main() {
	path := flag.String("config", "fin-exporter.yaml", "path to config file")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := loadConfig(*path)
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}

	go func() {
		for {
			collect(cfg, logger)
			time.Sleep(cfg.Interval)
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	logger.Info("listening", "addr", cfg.Listen)
	err = web.ListenAndServe(srv, &web.FlagConfig{
		WebListenAddresses: &[]string{cfg.Listen},
		WebSystemdSocket:   new(false),
		WebConfigFile:      &cfg.WebConfigFile,
	}, logger)
	logger.Error("server stopped", "err", err)
	os.Exit(1)
}

func loadConfig(path string) (*config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &config{Listen: "127.0.0.1:9101", Interval: 5 * time.Minute}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, err
	}
	if cfg.Interval < time.Minute {
		return nil, errors.New("interval must be >= 1m")
	}
	// Metrics have no auth unless web_config_file sets it, so only allow that on loopback.
	host, _, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	if ip := net.ParseIP(host); cfg.WebConfigFile == "" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("web_config_file (basic auth / TLS) is required when listen is not a loopback address")
	}
	// Cboe symbols go into the URL path, so allow only plain tickers.
	for _, s := range cfg.Stocks {
		if !cboeSymbol.MatchString(s.Symbol) {
			return nil, fmt.Errorf("stocks: invalid symbol %q (use uppercase like SPCX)", s.Symbol)
		}
	}
	for _, f := range cfg.Forex {
		if f.Base == "" || f.Quote == "" {
			return nil, errors.New("forex: base and quote are required")
		}
	}
	for _, f := range cfg.Funds {
		if f.Name == "" || f.ISIN == "" || f.Code == "" {
			return nil, errors.New("funds: name, isin and code are required")
		}
	}
	for _, i := range cfg.Indices {
		if !cboeSymbol.MatchString(i.Symbol) {
			return nil, fmt.Errorf("indices: invalid symbol %q (use uppercase like VIX, TNX)", i.Symbol)
		}
	}
	return cfg, nil
}

func collect(cfg *config, logger *slog.Logger) {
	for _, s := range cfg.Stocks {
		v, err := cboeQuote(s.Symbol)
		record(logger, "stock", s.Symbol, v, err, stockPrice, s.Symbol)
	}
	for _, f := range cfg.Forex {
		v, err := frankfurterRate(f.Base, f.Quote)
		record(logger, "forex", f.Base+f.Quote, v, err, forexRate, f.Base, f.Quote)
	}
	for _, f := range cfg.Funds {
		v, err := fundLatestNAV(f.ISIN, f.Code)
		record(logger, "fund", f.Name, v, err, fundNAV, f.Name, f.ISIN)
	}
	for _, i := range cfg.Indices {
		v, err := cboeQuote("_" + i.Symbol) // Cboe prefixes indices with "_"
		record(logger, "index", i.Symbol, v, err, indexValue, i.Symbol)
	}
}

// record keeps the previous value on error (no series until the first success,
// so a failed fetch never shows up as price 0); fin_fetch_success tells it is stale.
func record(logger *slog.Logger, kind, id string, v float64, err error, g *prometheus.GaugeVec, labels ...string) {
	if err != nil {
		logger.Warn("fetch failed", "kind", kind, "id", id, "err", err)
		fetchOK.WithLabelValues(kind, id).Set(0)
		return
	}
	g.WithLabelValues(labels...).Set(v)
	fetchOK.WithLabelValues(kind, id).Set(1)
}

func get(req *http.Request) ([]byte, error) {
	req.Header.Set("User-Agent", "fin-exporter")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s: %s", req.URL.Host, resp.Status, bytes.TrimSpace(b[:min(len(b), 200)]))
	}
	return b, nil
}

func frankfurterRate(base, quote string) (float64, error) {
	req, err := http.NewRequest("GET", "https://api.frankfurter.dev/v1/latest?"+url.Values{"base": {base}, "symbols": {quote}}.Encode(), nil)
	if err != nil {
		return 0, err
	}
	b, err := get(req)
	if err != nil {
		return 0, err
	}
	var r struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return 0, err
	}
	v, ok := r.Rates[quote]
	if !ok {
		return 0, fmt.Errorf("no rate for %s/%s", base, quote)
	}
	return v, nil
}

// cboeQuote uses the JSON behind cboe.com quote pages. It is not a documented
// API and may change without notice.
func cboeQuote(symbol string) (float64, error) {
	req, err := http.NewRequest("GET", "https://cdn.cboe.com/api/global/delayed_quotes/quotes/"+symbol+".json", nil)
	if err != nil {
		return 0, err
	}
	b, err := get(req)
	if err != nil {
		return 0, err
	}
	var r struct {
		Data struct {
			CurrentPrice float64 `json:"current_price"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return 0, err
	}
	if r.Data.CurrentPrice == 0 {
		return 0, fmt.Errorf("no value for %s", symbol)
	}
	return r.Data.CurrentPrice, nil
}

func fundLatestNAV(isin, code string) (float64, error) {
	req, err := http.NewRequest("GET", "https://toushin-lib.fwg.ne.jp/FdsWeb/FDST030000/csv-file-download?"+url.Values{"isinCd": {isin}, "associFundCd": {code}}.Encode(), nil)
	if err != nil {
		return 0, err
	}
	b, err := get(req)
	if err != nil {
		return 0, err
	}
	return parseNAV(b)
}

// parseNAV returns 基準価額 of the last row. The CSV is Shift_JIS, but only the
// header and date column contain non-ASCII and SJIS bytes never include ',', so
// no decoding is needed for the numeric column.
func parseNAV(b []byte) (float64, error) {
	rows, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		return 0, err
	}
	if len(rows) < 2 || len(rows[len(rows)-1]) < 2 {
		return 0, errors.New("no NAV rows (wrong isin/code?)")
	}
	return strconv.ParseFloat(rows[len(rows)-1][1], 64)
}
