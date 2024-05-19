package parser

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"aagr.xyz/trades/record"
)

type trading212Parser struct {
	act record.Account
}

func NewT212(act record.Account) *trading212Parser {
	return &trading212Parser{act: act}
}

func (p *trading212Parser) ValidateHeader(contents []string) error {
	want := map[int]string{
		0:  "Action",
		1:  "Time",
		3:  "Ticker",
		4:  "Name",
		5:  "No. of shares",
		6:  "Price / share",
		7:  "Currency (Price / share)",
		8:  "Exchange rate",
		10: "Total (GBP)",
		11: "Stamp duty (GBP)",
		12: "Stamp duty reserve tax (GBP)",
		13: "Finra fee (GBP)",
	}
	return headerMatches(want, contents)
}

func (p *trading212Parser) ToRecord(contents []string) ([]*record.Record, error) {
	r := &record.Record{Broker: p.act}
	var err error
	// fill up timestamp
	r.Timestamp, err = time.Parse(timeFmt, contents[1])
	if err != nil {
		return nil, fmt.Errorf("cannot parse timestamp: %v", err)
	}
	// fill up action field
	if strings.Contains(contents[0], "buy") {
		r.Action = record.NewTransactionType("buy")
	} else if strings.Contains(contents[0], "sell") {
		r.Action = record.NewTransactionType("sell")
	} else {
		return nil, fmt.Errorf("invalid action type %s", contents[0])
	}
	r.Ticker = contents[3]
	r.Name = contents[4]
	// fill up share count
	r.ShareCount, err = strconv.ParseFloat(contents[5], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to share count as float: %v", contents[5], err)
	}
	// fill up price
	r.PricePerShare, err = strconv.ParseFloat(contents[6], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to price per share as float: %v", contents[5], err)
	}
	// fillup currency
	r.Currency = record.NewCurrency(contents[7])
	// fill up exchange rate
	r.ExchangeRate, err = strconv.ParseFloat(contents[8], 64)
	if err != nil {
		switch r.Currency {
		case record.GBP:
			r.ExchangeRate = 1.0
		case record.GBX:
			r.ExchangeRate = 100.0
		default:
			return nil, fmt.Errorf("exchange rate invalid for %v, currency %v, please enter", r.Timestamp, r.Currency)
		}
	}
	// take reciprocal exchange rate
	r.ExchangeRate = 1.0 / r.ExchangeRate

	r.Commission, err = p.calculateCommission(r, contents)
	if err != nil {
		return nil, fmt.Errorf("cannot calcuate commission: %v", err)
	}
	r.Total, err = strconv.ParseFloat(contents[10], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to total as GBP as float: %v", contents[10], err)
	}
	return []*record.Record{r}, nil
}

func (p *trading212Parser) calculateCommission(_ *record.Record, contents []string) (float64, error) {
	// calculate commission and stamp duty fees for normal trades
	var res float64
	for _, idx := range []int{11, 12, 13} {
		if len(contents[idx]) > 0 {
			sd, err := strconv.ParseFloat(contents[idx], 64)
			if err != nil {
				return 0.0, fmt.Errorf("cannot convert %v to commission: %v", contents[idx], err)
			}
			res += sd
		}
	}
	return res, nil
}
