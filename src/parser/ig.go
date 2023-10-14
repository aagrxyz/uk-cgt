package parser

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"aagr.xyz/trades/src/record"
)

type igParser struct {
	act record.Account
}

func NewIG(account record.Account) *igParser {
	return &igParser{act: account}
}

func (p *igParser) ValidateHeader(contents []string) error {
	want := map[int]string{
		0:  "Date",
		1:  "Time",
		2:  "Activity",
		3:  "Market",
		4:  "Direction",
		5:  "Quantity",
		6:  "Price",
		7:  "Currency",
		8:  "Consideration",
		9:  "Commission",
		10: "Charges",
		11: "Cost/Proceeds",
		12: "Conversion rate",
	}
	return headerMatches(want, contents)
}

func (p *igParser) ToRecord(contents []string) ([]*record.Record, error) {
	if contents[2] != "TRADE" {
		return nil, fmt.Errorf("invalid activty type %v", contents)
	}

	r := &record.Record{Broker: p.act}
	var err error
	// fill up timestamp
	r.Timestamp, err = time.Parse("02/01/2006 15:04:05", fmt.Sprintf("%s %s", contents[0], contents[1]))
	if err != nil {
		// try the new date format
		r.Timestamp, err = time.Parse("02-01-2006 15:04:05", fmt.Sprintf("%s %s", contents[0], contents[1]))
		if err != nil {
			return nil, fmt.Errorf("cannot parse timestamp: %v", err)
		}
	}
	// fill up action field
	if strings.Contains(contents[4], "BUY") {
		r.Action = record.NewTransactionType("buy")
	} else if strings.Contains(contents[4], "SELL") {
		r.Action = record.NewTransactionType("sell")
	} else {
		return nil, fmt.Errorf("invalid action type %s", contents[4])
	}

	r.Name = contents[3]

	// fill up share count
	r.ShareCount, err = strconv.ParseFloat(contents[5], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to share count as float: %v", contents[5], err)
	}
	r.ShareCount = math.Abs(r.ShareCount)
	// fill up price
	r.PricePerShare, err = strconv.ParseFloat(contents[6], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to price per share as float: %v", contents[6], err)
	}
	// fillup currency
	r.Currency = record.NewCurrency(contents[7])
	// fill up exchange rate
	r.ExchangeRate, err = strconv.ParseFloat(contents[12], 64)
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

	consideration, err := strconv.ParseFloat(contents[8], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse consideration: %v", err)
	}
	consideration = math.Abs(consideration)
	// if the consideration is exactly 100 times the expected price, IG is stupid and does not
	// add a decimal point
	if math.Abs(((r.PricePerShare*r.ShareCount)/consideration)-100) <= 0.01 {
		r.PricePerShare /= 100.0
	}

	r.Commission, err = strconv.ParseFloat(contents[9], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot calcuate commission: %v", err)
	}
	r.Commission = math.Abs(r.Commission)
	charges, err := strconv.ParseFloat(contents[10], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot calcuate charges: %v", err)
	}
	r.Commission += math.Abs(charges)

	r.Total, err = strconv.ParseFloat(contents[11], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to total as GBP as float: %v", contents[10], err)
	}
	r.Total = math.Abs(r.Total)
	return []*record.Record{r}, nil
}
