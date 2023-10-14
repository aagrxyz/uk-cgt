package parser

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"aagr.xyz/trades/src/record"
)

type defaultParser struct{}

func NewDefault() *defaultParser {
	return &defaultParser{}
}

func (p *defaultParser) ValidateHeader(contents []string) error {
	want := map[int]string{
		0:  "Timestamp",
		1:  "Broker",
		2:  "Action",
		3:  "Ticker",
		4:  "Name",
		5:  "Quantity",
		6:  "Price",
		7:  "Currency",
		8:  "ExchangeRate",
		9:  "Commission",
		10: "Total",
		11: "Description",
		12: "CGTExempt",
	}
	return headerMatches(want, contents)
}

func (p *defaultParser) ToRecord(contents []string) ([]*record.Record, error) {
	action := record.NewTransactionType(contents[2])
	if action.IsMetadataEvent() {
		return p.metadataRecord(contents)
	} else if action.IsUnknown() {
		return nil, fmt.Errorf("unknown transaction type: %v", contents)
	}
	cgtExempt := false
	if contents[12] != "" {
		boolVal, err := strconv.ParseBool(contents[12])
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to bool: %v", contents[12], err)
		}
		cgtExempt = boolVal
	}
	r := &record.Record{
		Broker: record.Account{Name: strings.ToUpper(contents[1]), CGTExempt: cgtExempt},
		Action: action,
		Ticker: contents[3],
		Name:   contents[4],
	}
	var err error
	// fill up timestamp
	r.Timestamp, err = time.Parse(timeFmt, contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse timestamp: %v", err)
	}

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
		return nil, fmt.Errorf("cannot convert %v to exchange rate as float: %v", contents[8], err)
	}
	// fill up exchange rate
	r.Commission, err = strconv.ParseFloat(contents[9], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to commission as float: %v", contents[9], err)
	}
	// fill up total price
	r.Total, err = strconv.ParseFloat(contents[10], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to total as float: %v", contents[10], err)
	}
	return []*record.Record{r}, nil
}

func (p *defaultParser) metadataRecord(contents []string) ([]*record.Record, error) {
	r := &record.Record{Broker: record.GlobalBroker}
	var err error
	r.Timestamp, err = time.Parse(timeFmt, contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse date %s: %v", contents[0], err)
	}
	r.Ticker = contents[3]
	r.Action = record.NewTransactionType(contents[2])
	r.Description = contents[11]
	return []*record.Record{r}, nil
}
