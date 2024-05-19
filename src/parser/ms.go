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

type msVestParser struct {
	broker record.Account
}

func NewMSVest(act record.Account) (*msVestParser, error) {
	if act.Currency != record.USD {
		return nil, fmt.Errorf("MS Vest parser works with USD currency, got %s", act.Currency)
	}
	return &msVestParser{broker: act}, nil
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
		Broker:     p.broker,
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
	return []*record.Record{r, p.cashInRecord(r)}, nil
}

func (p *msVestParser) cashInRecord(vest *record.Record) *record.Record {
	cashIn := &record.Record{
		Timestamp:  vest.Timestamp,
		Broker:     p.broker,
		Action:     record.CashIn,
		Ticker:     string(record.USD),
		ShareCount: vest.Total / vest.ExchangeRate,
		Currency:   record.USD,
	}
	return cashIn
}

type msWithdrawlParser struct {
	broker          record.Account
	transferAccount record.Account
}

func NewMSWithdraw(act, transferAccount record.Account) (*msWithdrawlParser, error) {
	if act.Currency != record.USD {
		return nil, fmt.Errorf("MS Withdrawl parser assumes account currency USD, got %s", act.Currency)
	}
	return &msWithdrawlParser{
		broker:          act,
		transferAccount: transferAccount,
	}, nil
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

func (p *msWithdrawlParser) transferRecord(contents []string) ([]*record.Record, error) {
	out := &record.Record{
		Broker:   p.broker,
		Action:   record.TransferOut,
		Ticker:   "GOOG",
		Currency: record.USD,
	}
	var err error
	out.Timestamp, err = time.Parse("02-Jan-2006", contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse date %s: %v", contents[0], err)
	}
	out.ShareCount, err = strconv.ParseFloat(contents[6], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot get share count: %v", err)
	}
	out.ShareCount = math.Abs(out.ShareCount)
	out.Description = p.transferAccount.Name
	price := strings.ReplaceAll(strings.ReplaceAll(contents[5], "$", ""), ",", "")
	out.PricePerShare, err = strconv.ParseFloat(price, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse price %s: %v", price, err)
	}
	out.ExchangeRate = db.GetForex(out.Timestamp, string(record.USD))
	out.Total = out.PricePerShare * out.ShareCount * out.ExchangeRate
	in := *out
	in.Broker = p.transferAccount
	in.Action = record.TransferIn
	in.Description = "MS"
	return []*record.Record{out, &in}, nil
}

func (p *msWithdrawlParser) gsuRecord(contents []string) ([]*record.Record, error) {
	if contents[3] == "Transfer" {
		return p.transferRecord(contents)
	}
	if contents[3] != "Sale" {
		log.Warningf("invalid share class and/or type passed: %v, skipping", contents)
		return nil, nil
	}
	r := &record.Record{
		Broker:   p.broker,
		Action:   record.NewTransactionType("sell"),
		Ticker:   "GOOG",
		Currency: record.USD,
	}
	cashR := &record.Record{
		Broker:     p.broker,
		Action:     record.NewTransactionType("buy"),
		Ticker:     string(record.USD),
		Name:       string(record.USD),
		Currency:   record.USD,
		Commission: 0.0,
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
	cashR.PricePerShare = 1.0
	cashR.Total = cashR.ShareCount * cashR.PricePerShare * r.ExchangeRate
	cashR.ExchangeRate = r.ExchangeRate

	r.Total *= r.ExchangeRate
	r.Commission *= r.ExchangeRate

	return []*record.Record{r, cashR}, nil
}

func (p *msWithdrawlParser) cashRecord(contents []string) ([]*record.Record, error) {
	if contents[3] != "Sale" {
		return nil, fmt.Errorf("invalid type, expected Cash,Sale : %v", contents)
	}
	cashOut := &record.Record{
		Broker:   p.broker,
		Action:   record.CashOut,
		Ticker:   string(record.USD),
		Currency: record.USD,
	}
	var err error
	total := strings.ReplaceAll(strings.ReplaceAll(contents[7], "$", ""), ",", "")
	cashOut.ShareCount, err = strconv.ParseFloat(total, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse total %s: %v", total, err)
	}
	cashOut.Timestamp, err = time.Parse("02-Jan-2006", contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse date %s: %v", contents[0], err)
	}
	return []*record.Record{cashOut}, nil
}

func (p *msWithdrawlParser) ToRecord(contents []string) ([]*record.Record, error) {
	switch contents[2] {
	case "GSU Class C":
		return p.gsuRecord(contents)
	case "Cash":
		return p.cashRecord(contents)
	default:
		return nil, fmt.Errorf("invalid record passed: %v", contents)
	}
}
