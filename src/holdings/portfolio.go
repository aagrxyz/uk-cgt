package holdings

import (
	"fmt"
	"math"
	"strings"
	"time"

	"aagr.xyz/trades/db"
	"aagr.xyz/trades/marketdata"
	"aagr.xyz/trades/record"
	"github.com/jedib0t/go-pretty/v6/table"
	log "github.com/sirupsen/logrus"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

var tf = func(val interface{}) string {
	return fmt.Sprintf("%.2f", val)
}

func presentQuote(ticker string, market *marketdata.Service) (*marketdata.Quote, error) {
	meta, err := db.TickerMeta(ticker)
	if err != nil {
		return nil, fmt.Errorf("cannot get the metadata for ticker %s: %v", ticker, err)
	}
	if meta.AssetType == record.FOREX_ASSET {
		return presentForex(record.NewCurrency(ticker), market)
	}
	return market.GetQuote(ticker, meta.Currency, meta.Metadata)
}

func presentForex(currency record.Currency, market *marketdata.Service) (*marketdata.Quote, error) {
	price, err := market.GetForex(currency)
	if err != nil {
		return nil, err
	}
	return &marketdata.Quote{RegularMarketPrice: price}, nil
}

func assetType(ticker string) string {
	var res string
	meta, err := db.TickerMeta(ticker)
	if err != nil {
		log.Errorf("Cannot get metadata about ticker, so setting type as N/A")
		return "N/A"
	}
	res = string(meta.AssetType)
	if meta.AssetType == record.ETF_ASSET && meta.ETFType != "" {
		res = meta.ETFType
	}
	if ticker == "GOOGL" || ticker == "GOOG" {
		res = "GOOG"
	}
	return res
}

func AccountRows(byAccount map[record.Account]*Account, market *marketdata.Service) (map[record.Account][]*TickerRow, error) {
	res := make(map[record.Account][]*TickerRow)
	for name, act := range byAccount {
		var actRows []*TickerRow
		for ticker, pos := range act.positions {
			if math.Abs(pos.quantity-0.0) <= epsilon {
				continue
			}
			quote, err := presentQuote(ticker, market)
			if err != nil {
				return nil, fmt.Errorf("cannot get quote: %v", err)
			}
			meta, err := db.TickerMeta(ticker)
			if err != nil {
				return nil, fmt.Errorf("cannot get metadata about ticker %s: %v", ticker, err)
			}
			forex, err := presentForex(record.Currency(meta.Currency), market)
			if err != nil {
				return nil, fmt.Errorf("cannot get present forex: %v", err)
			}
			assetType := assetType(ticker)
			// This is incorrect as we are using present forex rather than the forex at which we bought it
			// TODO(aagr): Fix this
			gbpPos := &position{
				quantity:  pos.quantity,
				totalCost: pos.totalCost * forex.RegularMarketPrice,
			}
			tr := &TickerRow{
				Name:             ticker,
				AssetType:        assetType,
				Currency:         string(meta.Currency),
				Quantity:         pos.quantity,
				Forex:            forex.RegularMarketPrice,
				BasePriceMetrics: NewPriceMetrics(pos, quote.RegularMarketPrice, quote.TodayPercentChange),
				GBPPriceMetrics:  NewPriceMetrics(gbpPos, quote.RegularMarketPrice*forex.RegularMarketPrice, quote.TodayPercentChange),
			}
			actRows = append(actRows, tr)
		}
		res[name] = actRows
	}
	return res, nil
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

func AccountTable(byAccount map[record.Account]*Account, market *marketdata.Service) (map[record.Account]table.Writer, error) {
	actRows, err := AccountRows(byAccount, market)
	if err != nil {
		return nil, fmt.Errorf("cannot get account rows: %v", err)
	}
	var res = make(map[record.Account]table.Writer)
	for act, rows := range actRows {
		res[act] = accountTable(act, rows)
	}
	return res, nil
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

func PortfolioRows(holdings map[string]*Holding, market *marketdata.Service) ([]*TickerRow, error) {
	var res []*TickerRow
	for ticker, h := range holdings {
		quote, err := presentQuote(ticker, market)
		if err != nil {
			return nil, fmt.Errorf("cannot get quote: %v", err)
		}
		forex, err := presentForex(h.currency, market)
		if err != nil {
			return nil, fmt.Errorf("cannot get forex: %v", err)
		}
		assetType := assetType(ticker)
		baseRec := TickerRow{
			Name:      ticker,
			AssetType: assetType,
			Currency:  string(h.currency),
			Forex:     forex.RegularMarketPrice,
		}
		price, dayChange := quote.RegularMarketPrice, quote.TodayPercentChange
		if math.Abs(h.taxable.gbp.quantity-0.0) > epsilon {
			tax := baseRec
			tax.Taxable = "Y"
			tax.Quantity = h.taxable.base.quantity
			tax.BasePriceMetrics = NewPriceMetrics(h.taxable.base, price, dayChange)
			tax.GBPPriceMetrics = NewPriceMetrics(h.taxable.gbp, price*forex.RegularMarketPrice, dayChange)
			res = append(res, &tax)
		}
		if math.Abs(h.cgtExempt.gbp.quantity-0.0) > epsilon {
			exempt := baseRec
			exempt.Taxable = "N"
			exempt.Quantity = h.cgtExempt.base.quantity
			exempt.BasePriceMetrics = NewPriceMetrics(h.cgtExempt.base, price, dayChange)
			exempt.GBPPriceMetrics = NewPriceMetrics(h.cgtExempt.gbp, price*forex.RegularMarketPrice, dayChange)
			res = append(res, &exempt)
		}
	}
	return res, nil
}

// Portfolio prints a report for the open positions
func PortfolioTable(holdings map[string]*Holding, market *marketdata.Service) (table.Writer, error) {
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
	rows, err := PortfolioRows(holdings, market)
	if err != nil {
		return nil, fmt.Errorf("cannot get portfolio rows: %v", err)
	}
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
	return t, nil
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
