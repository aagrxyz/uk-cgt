package holdings

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"aagr.xyz/trades/src/record"
	"golang.org/x/exp/maps"
)

const epsilon = 1e-5

// position stores the quantity and the total cost i.e. purchase price at a given time
// this changes as and when we add or dispose holding
type position struct {
	quantity, totalCost float64
}

func (p *position) averageCost() float64 {
	if p.quantity == 0.0 {
		return 0.0
	}
	return p.totalCost / p.quantity
}

func (p *position) buy(qty, cost float64) {
	p.quantity += qty
	p.totalCost += cost
}

func (p *position) sell(qty float64) {
	average := p.averageCost()
	p.quantity -= qty
	p.totalCost -= average * qty
}

func (p *position) split(newCt, oldCt int64) {
	ratio := float64(newCt) / float64(oldCt)
	p.quantity *= ratio
}

type stats struct {
	realizedGain, disposed float64
}

// pool stores the position in both the base currency of the ticker
// and translated to GBP
type pool struct {
	base, gbp *position
	// yearStats store the stats of this pool in a given tax year
	// Since this computes gain and disposed amount, this is in GBP
	yearStats map[string]*stats
}

func newPool() *pool {
	return &pool{
		base:      &position{},
		gbp:       &position{},
		yearStats: make(map[string]*stats),
	}
}

// Each holding is identified by a ticker and has 2 set of pools.
// One is the taxable pools and other is the non-taxable i.e. CGT exempt
type Holding struct {
	ticker   string
	currency record.Currency
	// the current pool of open position, split by taxable account and non-taxable
	taxable   *pool
	cgtExempt *pool
	debug     string
}

func copyAndSortRecords(ticker string, records_orig []*record.Record) ([]*record.Record, error) {
	var records []*record.Record
	for _, r := range records_orig {
		if r.Ticker != ticker {
			return nil, fmt.Errorf("invalid ticker in record")
		}
		r_cpy := *r
		r_cpy.Timestamp = r_cpy.Timestamp.Truncate(24 * time.Hour)
		records = append(records, &r_cpy)
	}

	sort.Slice(records, func(i, j int) bool {
		x := records[i].Timestamp
		y := records[j].Timestamp
		// if same date then put split transaction first and then sell
		if x.Year() == y.Year() && x.YearDay() == y.YearDay() {
			return record.TransactionOrder[records[i].Action] < record.TransactionOrder[records[j].Action]
		}
		return x.Before(y)
	})
	return records, nil
}

func handleSplit(taxable, isa *pool, r *record.Record) error {
	var newCt, oldCt int64
	_, err := fmt.Sscanf(r.Description, "%d FOR %d", &newCt, &oldCt)
	if err != nil {
		return fmt.Errorf("error in parsing transaction %s", r.String())
	}
	// this will happen in both pools
	taxable.base.split(newCt, oldCt)
	taxable.gbp.split(newCt, oldCt)
	isa.base.split(newCt, oldCt)
	isa.gbp.split(newCt, oldCt)
	return nil
}

func handleSell(poolActive *pool, records []*record.Record, presentIdx int, debug *strings.Builder) error {
	r := records[presentIdx]
	year := getTaxYear(r.Timestamp)
	if year == "" {
		return fmt.Errorf("cannot calculate tax year from record timestamp: %v", r.Timestamp)
	}
	if _, ok := poolActive.yearStats[year]; !ok {
		poolActive.yearStats[year] = &stats{}
	}
	poolActive.yearStats[year].disposed += r.Total
	toMatch := r.ShareCount
	debug.WriteString(fmt.Sprintf("\nSELL on %v, quantity %f, price %f %s, total disposed %f GBP\n",
		r.Timestamp.Format("2006-01-02"), toMatch, r.PricePerShare, r.Currency, r.Total))

	// Now match this SELL with future transactions according to bed and breakfast rule
	for j := presentIdx + 1; j < len(records) && toMatch > 0.0; j++ {
		// greater than 30 days, so ignore and break, records is sorted
		if records[j].Timestamp.Sub(r.Timestamp) > 30*24*time.Hour {
			break
		}
		// if sell and another transaction are not in same type, then skip
		// both should be tax exempt or both not tax exempt
		if r.Broker.CGTExempt != records[j].Broker.CGTExempt {
			continue
		}
		// a buy transaction within the 30 days period, match against it.
		if records[j].Action == record.Buy {
			// if this buy has been exhausted then just continue
			if math.Abs(records[j].ShareCount-0.0) < epsilon {
				continue
			}
			matched := math.Min(records[j].ShareCount, toMatch)
			// per share cost of selling - per share cost of buying.
			cost := matched * (records[j].Total / records[j].ShareCount)
			disposal := matched * (r.Total / r.ShareCount)
			gain := disposal - cost
			poolActive.yearStats[year].realizedGain += gain
			debug.WriteString(fmt.Sprintf("\t\tMatched %f against BUY on %v, gain: %f GBP\n", matched, records[j].Timestamp.Format("2006-01-02"), gain))
			records[j].ShareCount -= matched
			records[j].Total -= cost
			toMatch -= matched
		}
	}
	// if more shares are left to be matched, use the pool
	if toMatch > 0 {
		if poolActive.gbp.quantity < toMatch {
			return fmt.Errorf("invalid quantity remanining in the pool, want %v, got %v", toMatch, poolActive.gbp.quantity)
		}
		cost := poolActive.gbp.averageCost() * toMatch
		disposal := toMatch * (r.Total / r.ShareCount)
		gain := disposal - cost
		poolActive.yearStats[year].realizedGain += gain
		debug.WriteString(fmt.Sprintf("\t\tMatched %f against POOL, with average cost %f, gain: %f GBP\n", toMatch, poolActive.gbp.averageCost(), gain))
		poolActive.gbp.sell(toMatch)
		poolActive.base.sell(toMatch)
	}
	return nil
}

func calculateInternal(ticker string, records_orig []*record.Record) (*Holding, error) {
	records, err := copyAndSortRecords(ticker, records_orig)
	if err != nil {
		return nil, fmt.Errorf("cannot copy and sort records based on timestamp: %v", err)
	}

	var (
		taxable    = newPool()
		cgtExempt  = newPool()
		debug      = &strings.Builder{}
		poolActive *pool
	)

	for i, r := range records {
		poolActive = taxable
		if r.Broker.CGTExempt {
			poolActive = cgtExempt
		}
		switch r.Action {
		case record.Split:
			if err := handleSplit(taxable, cgtExempt, r); err != nil {
				return nil, err
			}
		case record.Rename:
			return nil, fmt.Errorf("rename record should not be present here")
		case record.Buy:
			// if this buy has been exhausted then just continue
			if math.Abs(r.ShareCount-0.0) < epsilon {
				continue
			}
			poolActive.gbp.buy(r.ShareCount, r.Total)
			poolActive.base.buy(r.ShareCount, r.Total/r.ExchangeRate)
		case record.Sell:
			if err := handleSell(poolActive, records, i, debug); err != nil {
				return nil, fmt.Errorf("cannot handle SELL: %v", err)
			}
		}
	}
	return &Holding{
		ticker:    ticker,
		currency:  records_orig[0].Currency,
		taxable:   taxable,
		cgtExempt: cgtExempt,
		debug:     debug.String(),
	}, nil
}

// Calculate takes in the records and calculates the present holding situation
func Calculate(records []*record.Record, fxIsAsset bool) (map[string]*Holding, error) {
	var byTicker map[string][]*record.Record = make(map[string][]*record.Record)
	for _, r := range records {
		// ignore cash positions if fx is not considered an asset
		if r.Broker == record.CashBroker && !fxIsAsset {
			continue
		}
		byTicker[r.Ticker] = append(byTicker[r.Ticker], r)
	}
	var keys = maps.Keys(byTicker)
	sort.Strings(keys)
	var holdings map[string]*Holding = make(map[string]*Holding)
	for _, ticker := range keys {
		holding, err := calculateInternal(ticker, byTicker[ticker])
		if err != nil {
			log.Fatal(fmt.Errorf("cannot calcuate holding stats for ticker %s: %v", ticker, err))
		}
		holdings[ticker] = holding
	}
	return holdings, nil
}
