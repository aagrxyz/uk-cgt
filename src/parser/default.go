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
		1:  "Account.Name",
		2:  "Account.Currency",
		3:  "Account.CGTExempt",
		4:  "Action",
		5:  "Ticker",
		6:  "Name",
		7:  "Quantity",
		8:  "Price",
		9:  "Currency",
		10: "ExchangeRate",
		11: "Commission",
		12: "Total",
		13: "Description",
	}
	return headerMatches(want, contents)
}

func (p *defaultParser) ToRecord(contents []string) ([]*record.Record, error) {
	action := record.NewTransactionType(contents[4])
	if action.IsMetadataEvent() {
		return p.metadataRecord(contents)
	} else if action.IsCashEvent() {
		return p.cashRecord(contents)
	} else if action.IsDividend() {
		return p.divdendRecord(contents)
	} else if action.IsUnknown() {
		return nil, fmt.Errorf("unknown transaction type: %v", contents)
	}
	account, err := p.account(contents)
	if err != nil {
		return nil, fmt.Errorf("cannot get record account: %v", err)
	}
	r := &record.Record{
		Broker: *account,
		Action: action,
		Ticker: contents[5],
		Name:   contents[6],
	}
	// fill up timestamp
	r.Timestamp, err = time.Parse(timeFmt, contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse timestamp: %v", err)
	}

	// fill up share count
	r.ShareCount, err = strconv.ParseFloat(contents[7], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to share count as float: %v", contents[7], err)
	}
	// fill up price
	r.PricePerShare, err = strconv.ParseFloat(contents[8], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to price per share as float: %v", contents[8], err)
	}
	// fillup currency
	r.Currency = record.NewCurrency(contents[9])
	// fill up exchange rate
	r.ExchangeRate, err = strconv.ParseFloat(contents[10], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to exchange rate as float: %v", contents[10], err)
	}
	// fill up exchange rate
	r.Commission, err = strconv.ParseFloat(contents[11], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to commission as float: %v", contents[11], err)
	}
	// fill up total price
	r.Total, err = strconv.ParseFloat(contents[12], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to total as float: %v", contents[12], err)
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
	r.Ticker = contents[5]
	r.Action = record.NewTransactionType(contents[4])
	r.Description = contents[13]
	return []*record.Record{r}, nil
}

func (p *defaultParser) account(contents []string) (*record.Account, error) {
	cgtExempt := false
	if cgt := contents[3]; cgt != "" {
		boolVal, err := strconv.ParseBool(cgt)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to bool: %v", cgt, err)
		}
		cgtExempt = boolVal
	}
	curr := record.NewCurrency(contents[2])
	if string(curr) == "" {
		return nil, fmt.Errorf("account.currency of record cannot be nil: %v", contents)
	}
	return &record.Account{
		Name:      strings.ToUpper(contents[1]),
		Currency:  curr,
		CGTExempt: cgtExempt,
	}, nil
}
func (p *defaultParser) cashRecord(contents []string) ([]*record.Record, error) {
	account, err := p.account(contents)
	if err != nil {
		return nil, fmt.Errorf("cannot get account for record: %v", err)
	}
	r := &record.Record{
		Broker: *account,
		Action: record.NewTransactionType(contents[4]),
		Ticker: contents[5],
	}
	// fill up timestamp
	r.Timestamp, err = time.Parse(timeFmt, contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse timestamp: %v", err)
	}
	r.ShareCount, err = strconv.ParseFloat(contents[7], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to share count as float: %v", contents[7], err)
	}
	// fillup currency
	r.Currency = record.NewCurrency(contents[9])
	return []*record.Record{r}, nil
}

func (p *defaultParser) divdendRecord(contents []string) ([]*record.Record, error) {
	account, err := p.account(contents)
	if err != nil {
		return nil, fmt.Errorf("cannot get account for record: %v", err)
	}
	r := &record.Record{
		Broker: *account,
		Action: record.NewTransactionType(contents[4]),
		Ticker: contents[5],
	}
	// fill up timestamp
	r.Timestamp, err = time.Parse(timeFmt, contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse timestamp: %v", err)
	}
	r.ShareCount, err = strconv.ParseFloat(contents[7], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to share count as float: %v", contents[7], err)
	}
	// fillup currency
	r.Currency = record.NewCurrency(contents[9])
	return []*record.Record{r}, nil
}
