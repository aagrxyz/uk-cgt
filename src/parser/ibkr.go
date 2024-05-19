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

type ibkrParser struct {
	broker record.Account
}

func NewIBKR(act record.Account) (*ibkrParser, error) {
	if act.Currency != record.MULTIPLE {
		return nil, fmt.Errorf("IBKR Parser works with multiple currency support, got %s", act.Currency)
	}
	return &ibkrParser{broker: act}, nil
}

func (p *ibkrParser) ValidateHeader(contents []string) error {
	want := map[int]string{
		0:  "DateTime",
		1:  "Symbol",
		2:  "Buy/Sell",
		3:  "Quantity",
		4:  "TradePrice",
		5:  "CurrencyPrimary",
		6:  "FXRateToBase",
		7:  "Taxes",
		8:  "IBCommission",
		9:  "IBCommissionCurrency",
		10: "NetCash",
		11: "AssetClass",
	}
	return headerMatches(want, contents)
}

func (p *ibkrParser) ToRecord(contents []string) ([]*record.Record, error) {
	if contents[11] == "CASH" {
		return p.cashCurrencyRecord(contents)
	}
	if contents[11] != "STK" {
		log.Warningf("invalid asset class passed %v, ignored", contents)
		return nil, nil
	}

	r := &record.Record{Broker: p.broker}
	var err error
	// fill up timestamp
	r.Timestamp, err = time.Parse(timeFmt, contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse timestamp: %v", err)
	}
	// fill up action field
	if strings.Contains(contents[2], "BUY") {
		r.Action = record.NewTransactionType("buy")
	} else if strings.Contains(contents[2], "SELL") {
		r.Action = record.NewTransactionType("sell")
	} else {
		return nil, fmt.Errorf("invalid action type %s", contents[2])
	}
	r.Ticker = contents[1]

	// fill up share count
	r.ShareCount, err = strconv.ParseFloat(contents[3], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to share count as float: %v", contents[3], err)
	}
	r.ShareCount = math.Abs(r.ShareCount)
	// fill up price
	r.PricePerShare, err = strconv.ParseFloat(contents[4], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to price per share as float: %v", contents[4], err)
	}
	// fillup currency
	r.Currency = record.NewCurrency(contents[5])
	// fill up exchange rate
	r.ExchangeRate, err = strconv.ParseFloat(contents[6], 64)
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
	db.AddForex(r.Timestamp, string(r.Currency), r.ExchangeRate)

	r.Commission, err = strconv.ParseFloat(contents[8], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot calcuate commission: %v", err)
	}
	commissionCurr := record.NewCurrency(contents[9])
	if commissionCurr == r.Currency {
		r.Commission = r.ExchangeRate * math.Abs(r.Commission)
	} else if commissionCurr == record.GBP {
		r.Commission = math.Abs(r.Commission)
	} else {
		return nil, fmt.Errorf("commission currency %v is different from default currency %v", commissionCurr, r.Currency)
	}
	taxes, err := strconv.ParseFloat(contents[7], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot calclate taxes: %v", err)
	}
	r.Commission += math.Abs(taxes)

	r.Total, err = strconv.ParseFloat(contents[10], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to total as GBP as float: %v", contents[10], err)
	}
	r.Total = r.ExchangeRate * math.Abs(r.Total)
	var res []*record.Record
	res = append(res, r)
	if r.Currency != record.GBP {
		res = append(res, p.forexRecord(r))
	}

	return res, nil
}

func (p *ibkrParser) forexRecord(trade *record.Record) *record.Record {
	r := &record.Record{
		Timestamp: trade.Timestamp,
		Broker:    p.broker,
		Ticker:    string(trade.Currency),
		Name:      string(trade.Currency),
		// amount of currency converted, total in GBP divided by exchange rate
		ShareCount:    trade.Total / trade.ExchangeRate,
		PricePerShare: 1.0,
		Currency:      trade.Currency,
		ExchangeRate:  trade.ExchangeRate,
		Commission:    0.0,
		Total:         trade.Total,
	}
	// if it is buy a stock then sell currency
	r.Action = record.InverseAction(trade.Action)
	return r
}

func (p *ibkrParser) cashCurrencyRecord(contents []string) ([]*record.Record, error) {
	r := &record.Record{Broker: p.broker}
	var err error
	// fill up timestamp
	r.Timestamp, err = time.Parse(timeFmt, contents[0])
	if err != nil {
		return nil, fmt.Errorf("cannot parse timestamp: %v", err)
	}
	// fill up action field
	if !strings.Contains(contents[2], "SELL") {
		return nil, fmt.Errorf("invalid action type %s", contents[2])
	}
	currencyPair := strings.Split(contents[1], ".")
	sellCurrency := record.NewCurrency(currencyPair[0])
	buyCurrency := record.NewCurrency(currencyPair[1])
	if sellCurrency != record.GBP && buyCurrency != record.GBP {
		return nil, fmt.Errorf("case where base currency or got currency is not GBP is not handled")
	}

	r.Commission, err = strconv.ParseFloat(contents[8], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot calcuate commission: %v", err)
	}
	commissionCurr := record.NewCurrency(contents[9])
	if commissionCurr == record.GBP {
		r.Commission = math.Abs(r.Commission)
	} else {
		return nil, fmt.Errorf("commission currency %v is different from default currency %v", commissionCurr, record.GBP)
	}

	qty, err := strconv.ParseFloat(contents[3], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to quantity as float: %v", contents[3], err)
	}
	qty = math.Abs(qty)
	price, err := strconv.ParseFloat(contents[4], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %v to price as float: %v", contents[4], err)
	}
	price = math.Abs(price)

	if sellCurrency == record.GBP {
		// Since sell currency is GBP, this is buying another currency i.e. selling GBP to get something else
		r.Action = record.NewTransactionType("buy")
		r.Ticker = string(buyCurrency)
		r.Name = string(buyCurrency)
		// Record is like SELL 3000 GBP at 1.2 to get 4200 USD. We are storing we buying USD, so need to invert stuff
		r.ShareCount = qty * price
		r.PricePerShare = 1.0
		r.Total = qty + r.Commission
		r.Currency = buyCurrency
		r.ExchangeRate = 1.0 / price
		r.Description = "SELL GBP"
	} else if buyCurrency == record.GBP {
		// This time we got GBP back, so sold something
		r.Action = record.NewTransactionType("sell")
		r.Ticker = string(sellCurrency)
		r.Name = string(sellCurrency)
		// Record is like SELL 3000 EUR at 0.8 GBP to get 2400 GBP. So no inversion required
		r.ShareCount = qty
		r.PricePerShare = 1.0
		r.Total = qty*price - r.Commission
		r.Currency = sellCurrency
		r.ExchangeRate = price
		r.Description = "BUY GBP"
	}
	return []*record.Record{r}, nil
}
