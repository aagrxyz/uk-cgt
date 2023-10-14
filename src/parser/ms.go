package parser

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"aagr.xyz/trades/src/db"
	"aagr.xyz/trades/src/record"
	log "github.com/sirupsen/logrus"
)

var morganStanleyBroker = record.Account{Name: "MS", CGTExempt: false}

type msVestParser struct{}

func NewMSVest() *msVestParser {
	return &msVestParser{}
}

func (p *msVestParser) ValidateHeader(contents []string) error {
	want := map[int]string{
		0: "Date",
		2: "Plan",
		3: "Type",
		5: "Price",
		6: "Quantity",
	}
	return headerMatches(want, contents)
}

func (p *msVestParser) ToRecord(contents []string) ([]*record.Record, error) {
	r := &record.Record{
		Broker:     morganStanleyBroker,
		Action:     record.NewTransactionType("buy"),
		Currency:   record.USD,
		Ticker:     "GOOG",
		Commission: 0.0,
	}
	var err error
	r.Timestamp, err = time.Parse("02-Jan-2006", contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse date %s: %v", contents[0], err)
	}
	if contents[2] != "GSU Class C" {
		return nil, fmt.Errorf("invalid share class passed %v", contents[2])
	}
	r.ShareCount, err = strconv.ParseFloat(contents[6], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot get share count: %v", err)
	}
	price := strings.ReplaceAll(strings.ReplaceAll(contents[5], "$", ""), ",", "")
	r.PricePerShare, err = strconv.ParseFloat(price, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse price %s: %v", price, err)
	}
	r.ExchangeRate = db.GetForex(r.Timestamp, string(r.Currency))
	r.Total = r.PricePerShare * r.ShareCount * r.ExchangeRate
	return []*record.Record{r}, nil
}

type msWithdrawlParser struct{}

func NewMSWithdraw() *msWithdrawlParser {
	return &msWithdrawlParser{}
}

func (p *msWithdrawlParser) ValidateHeader(contents []string) error {
	want := map[int]string{
		0: "Date",
		2: "Plan",
		3: "Type",
		5: "Price",
		6: "Quantity",
		7: "Net Amount",
	}
	return headerMatches(want, contents)
}

func (p *msWithdrawlParser) ToRecord(contents []string) ([]*record.Record, error) {
	if contents[2] != "GSU Class C" || contents[3] != "Sale" {
		log.Warningf("invalid share class and/or type passed: %v, skipping", contents)
		return nil, nil
	}
	r := &record.Record{
		Broker:   morganStanleyBroker,
		Action:   record.NewTransactionType("sell"),
		Ticker:   "GOOG",
		Currency: record.USD,
	}
	cashR := &record.Record{
		Broker:       record.CashBroker,
		Action:       record.NewTransactionType("buy"),
		Ticker:       string(record.USD),
		Name:         string(record.USD),
		Currency:     record.GBP,
		Commission:   0.0,
		ExchangeRate: 1.0,
	}

	var err error
	r.Timestamp, err = time.Parse("02-Jan-2006", contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse date %s: %v", contents[0], err)
	}
	cashR.Timestamp = r.Timestamp
	r.ShareCount, err = strconv.ParseFloat(contents[6], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot get share count: %v", err)
	}
	r.ShareCount = math.Abs(r.ShareCount)
	price := strings.ReplaceAll(strings.ReplaceAll(contents[5], "$", ""), ",", "")
	r.PricePerShare, err = strconv.ParseFloat(price, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse price %s: %v", price, err)
	}
	total := strings.ReplaceAll(strings.ReplaceAll(contents[7], "$", ""), ",", "")
	r.Total, err = strconv.ParseFloat(total, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse total %s: %v", total, err)
	}
	// this is all in USD right now
	r.Total = math.Abs(r.Total)
	r.Commission = (r.ShareCount * r.PricePerShare) - r.Total
	cashR.ShareCount = r.Total

	r.ExchangeRate = db.GetForex(r.Timestamp, string(record.USD))
	cashR.PricePerShare = r.ExchangeRate
	cashR.Total = cashR.ShareCount * cashR.PricePerShare

	r.Total *= r.ExchangeRate
	r.Commission *= r.ExchangeRate

	return []*record.Record{r, cashR}, nil
}
