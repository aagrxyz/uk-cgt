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

func priceAndDayChange(ticker string, yahooClient *resty.Client) (float64, float64) {
	meta, err := db.TickerMeta(ticker)
	if err != nil {
		log.Warningf("Cannot get the yahoo ticker for %s, so using 0 present value: %v", ticker, err)
		return 0.0, 0.0
	}
	if meta.AssetType == db.ForexType {
		return presentForex(record.NewCurrency(ticker), yahooClient), 0.0
	}
	quote, err := yahoo.GetQuote(yahooClient, meta.YahooTicker)
	if err != nil {
		log.Warningf("Cannot get price from yahoo for ticker %s: %v", meta.YahooTicker, err)
		return 0.0, 0.0
	}
	if quote.Currency != meta.YahooCurrency {
		log.Warningf("Stored yahoo ticker currency is different from present value in quote (stored:%s, got:%s)", meta.YahooCurrency, quote.Currency)
		return 0.0, 0.0
	}
	// Handle different types of currencies i.e. GBP and GBX bull-shit
	if quote.Currency == "GBp" {
		if record.NewCurrency(meta.Currency) == record.GBX {
			return quote.Price(), quote.TodayPercentChange()
		}
		if record.NewCurrency(meta.Currency) == record.GBP {
			return quote.Price() / 100.0, quote.TodayPercentChange()
		}
	}
	if quote.Currency == "GBP" {
		if record.NewCurrency(meta.Currency) == record.GBP {
			return quote.Price(), quote.TodayPercentChange()
		}
		if record.NewCurrency(meta.Currency) == record.GBX {
			return quote.Price() * 100.0, quote.TodayPercentChange()
		}
	}
	// If it is any other currency typically USD, EUR, INR - this will be correct and match the ticker currency
	if record.NewCurrency(quote.Currency) != record.NewCurrency(meta.Currency) {
		log.Warningf("The currencies should match for ticker %s, but did not (got=%s, want=%s)", ticker, quote.Currency, meta.Currency)
		return 0.0, 0.0
	}
	return quote.Price(), quote.TodayPercentChange()
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

func AccountRows(byAccount map[record.Account]*Account, yahooClient *resty.Client) map[record.Account][]*TickerRow {
	res := make(map[record.Account][]*TickerRow)
	for name, act := range byAccount {
		var actRows []*TickerRow
		for ticker, pos := range act.positions {
			if math.Abs(pos.quantity-0.0) <= epsilon {
				continue
			}
			price, dayChange := priceAndDayChange(ticker, yahooClient)
			meta, err := db.TickerMeta(ticker)
			if err != nil {
				log.Errorf("Cannot get metadata about ticker %s: %v", ticker, err)
			}
			forex := presentForex(record.Currency(meta.Currency), yahooClient)
			assetType := assetType(ticker)
			// This is incorrect as we are using present forex rather than the forex at which we bought it
			// TODO(aagr): Fix this
			gbpPos := &position{
				quantity:  pos.quantity,
				totalCost: pos.totalCost * forex,
			}
			tr := &TickerRow{
				Name:             ticker,
				AssetType:        assetType,
				Currency:         meta.Currency,
				Quantity:         pos.quantity,
				Forex:            forex,
				BasePriceMetrics: NewPriceMetrics(pos, price, dayChange),
				GBPPriceMetrics:  NewPriceMetrics(gbpPos, price*forex, dayChange),
			}
			actRows = append(actRows, tr)
		}
		res[name] = actRows
	}
	return res
}

func accountTable(act record.Account, rows []*TickerRow) table.Writer {
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
	for _, r := range rows {
		t.AppendRow(table.Row{
			act.Name,
			r.Name,
			r.AssetType,
			r.Currency,
			r.Quantity,
			r.BasePriceMetrics.AvgPrice,
			r.BasePriceMetrics.PresentPrice, r.BasePriceMetrics.TotalValue,
			r.BasePriceMetrics.TotalGain,
			r.GBPPriceMetrics.TotalValue,
			r.GBPPriceMetrics.TotalGain,
		})
		totalValueGBP += r.GBPPriceMetrics.TotalValue
		totalGainGBP += r.GBPPriceMetrics.TotalGain
	}
	t.AppendFooter(table.Row{
		act.Name, "", "", "", "", "", "", "", "", totalValueGBP, totalGainGBP,
	})
	return t
}

func AccountTable(byAccount map[record.Account]*Account, yahooClient *resty.Client) map[record.Account]table.Writer {
	actRows := AccountRows(byAccount, yahooClient)
	var res = make(map[record.Account]table.Writer)
	for act, rows := range actRows {
		res[act] = accountTable(act, rows)
	}
	return res
}

type PriceMetrics struct {
	AvgPrice, TotalCost            float64
	PresentPrice                   float64
	TotalValue, TotalGain          float64
	TotalGainPercentage            float64
	GainToday, GainTodayPercentage float64
}

func NewPriceMetrics(p *position, present float64, dayChangePercent float64) PriceMetrics {
	res := PriceMetrics{
		AvgPrice:            p.averageCost(),
		TotalCost:           p.totalCost,
		PresentPrice:        present,
		TotalValue:          p.quantity * present,
		GainTodayPercentage: dayChangePercent,
	}
	res.TotalGain = res.TotalValue - res.TotalCost
	res.TotalGainPercentage = res.TotalGain / res.TotalCost * 100.0
	return res
}

type TickerRow struct {
	Name      string
	AssetType string
	Currency  string
	// all of the fields below depend on the taxabale field
	Taxable          string
	Quantity         float64
	BasePriceMetrics PriceMetrics
	Forex            float64
	GBPPriceMetrics  PriceMetrics
}

func PortfolioRows(holdings map[string]*Holding, yahooClient *resty.Client) []*TickerRow {
	var res []*TickerRow
	for ticker, h := range holdings {
		price, dayChange := priceAndDayChange(ticker, yahooClient)
		forex := presentForex(h.currency, yahooClient)
		assetType := assetType(ticker)
		baseRec := TickerRow{
			Name:      ticker,
			AssetType: assetType,
			Currency:  string(h.currency),
			Forex:     forex,
		}
		if math.Abs(h.taxable.gbp.quantity-0.0) > epsilon {
			tax := baseRec
			tax.Taxable = "Y"
			tax.Quantity = h.taxable.base.quantity
			tax.BasePriceMetrics = NewPriceMetrics(h.taxable.base, price, dayChange)
			tax.GBPPriceMetrics = NewPriceMetrics(h.taxable.gbp, price*forex, dayChange)
			res = append(res, &tax)
		}
		if math.Abs(h.cgtExempt.gbp.quantity-0.0) > epsilon {
			exempt := baseRec
			exempt.Taxable = "N"
			exempt.Quantity = h.cgtExempt.base.quantity
			exempt.BasePriceMetrics = NewPriceMetrics(h.cgtExempt.base, price, dayChange)
			exempt.GBPPriceMetrics = NewPriceMetrics(h.cgtExempt.gbp, price*forex, dayChange)
			res = append(res, &exempt)
		}
	}
	return res
}

// Portfolio prints a report for the open positions
func PortfolioTable(holdings map[string]*Holding, yahooClient *resty.Client) table.Writer {
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
	rows := PortfolioRows(holdings, yahooClient)
	for _, r := range rows {
		t.AppendRow(table.Row{
			r.Name,
			r.AssetType,
			r.Taxable,
			r.Currency,
			r.Quantity,
			r.BasePriceMetrics.AvgPrice,
			r.BasePriceMetrics.TotalCost,
			r.GBPPriceMetrics.AvgPrice,
			r.GBPPriceMetrics.TotalCost,
			r.BasePriceMetrics.PresentPrice,
			r.BasePriceMetrics.TotalValue,
			r.GBPPriceMetrics.PresentPrice,
			r.GBPPriceMetrics.TotalValue,
		}, rowConfigAutoMerge)
		totalCost += r.GBPPriceMetrics.TotalCost
		presentValue += r.GBPPriceMetrics.TotalValue
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
