package holdings

import (
	"fmt"
	"math"
	"strings"
	"time"

	"aagr.xyz/trades/db"
	"aagr.xyz/trades/record"
	"aagr.xyz/trades/yahoo"
	"github.com/go-resty/resty/v2"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jellydator/ttlcache/v3"
	log "github.com/sirupsen/logrus"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

var tf = func(val interface{}) string {
	return fmt.Sprintf("%.2f", val)
}

func presentPrice(ticker string, yahooClient *resty.Client) float64 {
	meta, err := db.TickerMeta(ticker)
	if err != nil {
		log.Warningf("Cannot get the yahoo ticker for %s, so using 0 present value: %v", ticker, err)
		return 0.0
	}
	if meta.AssetType == db.ForexType {
		return presentForex(record.NewCurrency(ticker), yahooClient)
	}
	quote, err := yahoo.GetQuote(yahooClient, meta.YahooTicker)
	if err != nil {
		log.Warningf("Cannot get price from yahoo for ticker %s: %v", meta.YahooTicker, err)
		return 0.0
	}
	if quote.Currency != meta.YahooCurrency {
		log.Warningf("Stored yahoo ticker currency is different from present value in quote (stored:%s, got:%s)", meta.YahooCurrency, quote.Currency)
		return 0.0
	}
	// Handle different types of currencies i.e. GBP and GBX bull-shit
	if quote.Currency == "GBp" {
		if record.NewCurrency(meta.Currency) == record.GBX {
			return quote.Price()
		}
		if record.NewCurrency(meta.Currency) == record.GBP {
			return quote.Price() / 100.0
		}
	}
	if quote.Currency == "GBP" {
		if record.NewCurrency(meta.Currency) == record.GBP {
			return quote.Price()
		}
		if record.NewCurrency(meta.Currency) == record.GBX {
			return quote.Price() * 100.0
		}
	}
	// If it is any other currency typically USD, EUR, INR - this will be correct and match the ticker currency
	if record.NewCurrency(quote.Currency) != record.NewCurrency(meta.Currency) {
		log.Warningf("The currencies should match for ticker %s, but did not (got=%s, want=%s)", ticker, quote.Currency, meta.Currency)
		return 0.0
	}
	return quote.Price()
}

var cachedForex *ttlcache.Cache[record.Currency, float64]

func init() {
	cachedForex = ttlcache.New[record.Currency, float64](
		ttlcache.WithTTL[record.Currency, float64](10 * time.Minute),
	)
	go cachedForex.Start()
}

func presentForex(currency record.Currency, yahooClient *resty.Client) float64 {
	switch currency {
	case record.GBP:
		return 1.0
	case record.GBX:
		return 0.01
	}
	if cachedForex.Has(currency) {
		return cachedForex.Get(currency).Value()
	}
	symbol := fmt.Sprintf("%sGBP=X", currency)
	quote, err := yahoo.GetQuote(yahooClient, symbol)
	if err != nil {
		log.Warningf("cannot get forex for today for symbol %s from yahoo finance: %v", symbol, err)
		return 0.0
	}
	price := quote.Price()
	cachedForex.Set(currency, price, ttlcache.DefaultTTL)
	return price
}

func assetType(ticker string) string {
	var res string
	meta, err := db.TickerMeta(ticker)
	if err != nil {
		log.Errorf("Cannot get metadata about ticker, so setting type as N/A")
		return "N/A"
	}
	res = meta.AssetType
	if res == "ETF" && meta.ETFType != "" {
		res = meta.ETFType
	}
	if ticker == "GOOGL" || ticker == "GOOG" {
		res = "GOOG"
	}
	return res
}

func accountTable(act *Account, yahooClient *resty.Client) table.Writer {
	t := table.NewWriter()
	t.SetTitle(fmt.Sprintf("Account %s (currency=%s) Holdings", act.Name, act.Currency))
	t.SetStyle(table.StyleLight)
	t.AppendHeader(table.Row{
		"Account", "Ticker", "Asset Type", "Currency", "Quantity",
		"Avg Price", "Present Price", "Present Value",
		"P&L", "Present Value (GBP)", "P&L (GBP)",
	})
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 5, Transformer: tf},
		{Number: 6, Transformer: tf},
		{Number: 7, Transformer: tf},
		{Number: 8, Transformer: tf},
		{Number: 9, Transformer: tf},
		{Number: 10, Transformer: tf, TransformerFooter: tf},
		{Number: 11, Transformer: tf, TransformerFooter: tf},
	})
	t.SortBy([]table.SortBy{{Number: 1}})
	var totalValueGBP, totalGainGBP float64
	for ticker, pos := range act.positions {
		if math.Abs(pos.quantity-0.0) <= epsilon {
			continue
		}
		price := presentPrice(ticker, yahooClient)
		meta, err := db.TickerMeta(ticker)
		if err != nil {
			log.Errorf("Cannot get metadata about ticker %s: %v", ticker, err)
		}
		forex := presentForex(record.Currency(meta.Currency), yahooClient)
		assetType := assetType(ticker)
		presentValue := pos.quantity * price
		gain := presentValue - pos.totalCost
		t.AppendRow(table.Row{
			act.Account.Name,
			ticker,
			assetType,
			meta.Currency,
			pos.quantity,
			pos.averageCost(),
			price, presentValue,
			gain,
			presentValue * forex,
			gain * forex,
		})
		totalValueGBP += (presentValue * forex)
		totalGainGBP += (gain * forex)
	}
	t.AppendFooter(table.Row{
		act.Account.Name, "", "", "", "", "", "", "", "", totalValueGBP, totalGainGBP,
	})
	return t
}

func AccountStats(byAccount map[record.Account]*Account, yahooClient *resty.Client) map[record.Account]table.Writer {
	var res = make(map[record.Account]table.Writer)
	for act, pos := range byAccount {
		res[act] = accountTable(pos, yahooClient)
	}
	return res
}

// Portfolio prints a report for the open positions
func Portfolio(holdings map[string]*Holding, yahooClient *resty.Client) table.Writer {
	var totalCost, presentValue float64
	t := table.NewWriter()
	rowConfigAutoMerge := table.RowConfig{AutoMerge: true}
	t.SetTitle(fmt.Sprintf("Current Holdings @ %s", time.Now().Format("2006-01-02 15:04:05")))
	t.SetStyle(table.StyleLight)
	t.AppendHeader(table.Row{
		"Ticker", "Asset Type", "Taxable", "Currency", "Quantity",
		"Avg Price", "Total Cost",
		"Avg Price (GBP)", "Total Cost (GBP)",
		"Present Price", "Present Value",
		"Present Price (GBP)", "Present Value (GBP)",
	})
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 5, Transformer: tf},
		{Number: 6, Transformer: tf},
		{Number: 7, Transformer: tf},
		{Number: 8, Transformer: tf},
		{Number: 9, Transformer: tf, TransformerFooter: tf},
		{Number: 10, Transformer: tf},
		{Number: 11, Transformer: tf},
		{Number: 12, Transformer: tf},
		{Number: 13, Transformer: tf, TransformerFooter: tf},
	})
	t.SortBy([]table.SortBy{
		{Number: 1},
	})
	for ticker, h := range holdings {
		price := presentPrice(ticker, yahooClient)
		forex := presentForex(h.currency, yahooClient)
		assetType := assetType(ticker)

		if math.Abs(h.taxable.gbp.quantity-0.0) > epsilon {
			t.AppendRow(table.Row{
				ticker,
				assetType,
				"Y",
				h.currency, h.taxable.gbp.quantity,
				h.taxable.base.averageCost(), h.taxable.base.totalCost,
				h.taxable.gbp.averageCost(), h.taxable.gbp.totalCost,
				price, h.taxable.base.quantity * price,
				price * forex, h.taxable.gbp.quantity * price * forex,
			}, rowConfigAutoMerge)
			totalCost += h.taxable.gbp.totalCost
			presentValue += h.taxable.gbp.quantity * price * forex
		}
		if math.Abs(h.cgtExempt.gbp.quantity-0.0) > epsilon {
			t.AppendRow(table.Row{
				ticker,
				assetType,
				"N",
				h.currency, h.cgtExempt.gbp.quantity,
				h.cgtExempt.base.averageCost(), h.cgtExempt.base.totalCost,
				h.cgtExempt.gbp.averageCost(), h.cgtExempt.gbp.totalCost,
				price, h.cgtExempt.base.quantity * price,
				price * forex, h.cgtExempt.gbp.quantity * price * forex,
			}, rowConfigAutoMerge)
			totalCost += h.cgtExempt.gbp.totalCost
			presentValue += h.cgtExempt.gbp.quantity * price * forex
		}
	}
	t.AppendFooter(table.Row{
		"", "", "", "", "", "", "", "", totalCost, "", "", "", presentValue,
	})
	return t
}

// CGT returns a string containing report for CGT along with a debug string
func CGT(holdings map[string]*Holding) (map[string]table.Writer, string) {
	var tables map[string]table.Writer = make(map[string]table.Writer)
	var totalStats map[string]*stats = make(map[string]*stats)
	years := maps.Keys(taxYears)
	slices.Sort(years)
	for _, ty := range years {
		totalStats[ty] = &stats{}
		t := table.NewWriter()
		t.SetTitle(fmt.Sprintf("Tax year %s", ty))
		t.SetStyle(table.StyleLight)
		t.AppendHeader(table.Row{
			"Ticker", "Disposed (GBP)", "Gain (GBP)",
		})
		t.SetColumnConfigs([]table.ColumnConfig{
			{Number: 2, Transformer: tf, TransformerFooter: tf},
			{Number: 3, Transformer: tf, TransformerFooter: tf},
		})
		t.SortBy([]table.SortBy{
			{Number: 1},
		})
		tables[ty] = t
	}
	for ticker, h := range holdings {
		for ty, st := range h.taxable.yearStats {
			tables[ty].AppendRow(table.Row{
				ticker, st.disposed, st.realizedGain,
			})
			totalStats[ty].disposed += st.disposed
			totalStats[ty].realizedGain += st.realizedGain
		}
	}
	for _, ty := range years {
		tables[ty].AppendFooter(table.Row{
			"TOTAL", totalStats[ty].disposed, totalStats[ty].realizedGain,
		})
	}
	return tables, debugCGT(holdings)
}

func debugCGT(holdings map[string]*Holding) string {
	var sb strings.Builder
	for ticker, h := range holdings {
		if h.debug == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n\n****** Ticker = %s ******\n", ticker))
		sb.WriteString(h.debug)
	}
	return sb.String()
}
