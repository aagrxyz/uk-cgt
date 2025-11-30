package parser

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"aagr.xyz/trades/db"
	"aagr.xyz/trades/record"
)

type ibkrDividendParser struct {
	act record.Account
}

func NewIBKRDividend(account record.Account) *ibkrDividendParser {
	return &ibkrDividendParser{act: account}
}

func (p *ibkrDividendParser) ValidateHeader(contents []string) error {
	want := map[int]string{
		0: "Date/Time",
		2: "CurrencyPrimary",
		3: "FXRateToBase",
		4: "Symbol",
		5: "Amount",
		7: "Type",
	}
	return headerMatches(want, contents)
}

func (p *ibkrDividendParser) ToRecord(contents []string) ([]*record.Record, error) {
	r := &record.Record{
		Broker: p.act,
		Action: record.Dividend,
	}
	var err error
	// fill up timestamp
	r.Timestamp, err = time.Parse(timeFmt, contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse timestamp: %v", err)
	}
	r.Ticker = contents[4]
	r.Currency = record.NewCurrency(contents[2])
	if r.Currency == "" {
		return nil, fmt.Errorf("invalid currency type: %q", contents[2])
	}
	r.ShareCount, err = strconv.ParseFloat(contents[5], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to share count as float: %v", contents[5], err)
	}
	r.ShareCount = math.Abs(r.ShareCount)
	r.PricePerShare = 1.0

	r.ExchangeRate, err = strconv.ParseFloat(contents[3], 64)
	if err != nil {
		switch r.Currency {
		case record.GBP:
			r.ExchangeRate = 1.0
		case record.GBX:
			r.ExchangeRate = 0.01
		default:
			return nil, fmt.Errorf("exchange rate invalid for %v, currency %v, please enter", r.Timestamp, r.Currency)
		}
	}
	// We use IBKR as authoritative source
	db.AddForex(r.Timestamp, r.Currency, r.ExchangeRate)
	r.Total = r.ShareCount * r.ExchangeRate * r.PricePerShare
	if contents[7] == "Withholding Tax" {
		r.Action = record.WitholdingTax
	}
	return []*record.Record{r}, nil
}
