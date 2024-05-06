package parser

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"aagr.xyz/trades/src/record"
)

type igDividendParser struct {
	act record.Account
}

func NewIGDividend(account record.Account) *igDividendParser {
	return &igDividendParser{act: account}
}

func (p *igDividendParser) ValidateHeader(contents []string) error {
	want := map[int]string{
		13: "DateUtc",
		1:  "Summary",
		2:  "MarketName",
		5:  "Transaction type",
		11: "PL Amount",
	}
	return headerMatches(want, contents)
}

func (p *igDividendParser) ToRecord(contents []string) ([]*record.Record, error) {
	if contents[1] != "Dividend" || contents[5] != "DEPO" {
		return nil, fmt.Errorf("invalid activty type %v", contents)
	}

	r := &record.Record{
		Broker: p.act,
		Action: record.Dividend,
	}
	var err error
	// fill up timestamp
	r.Timestamp, err = time.Parse(time.RFC3339, fmt.Sprintf("%sZ", contents[13]))
	if err != nil {
		return nil, fmt.Errorf("cannot parse timestamp: %v", err)
	}
	marketName := strings.Split(contents[2], "DIVIDEND")
	r.Name = strings.TrimSpace(marketName[0])
	r.Description = strings.TrimSpace(marketName[1])
	r.Currency = record.GBP
	r.ShareCount, err = strconv.ParseFloat(contents[11], 64)
	if err != nil {
		return nil, fmt.Errorf("cannot calcuate commission: %v", err)
	}
	r.PricePerShare = 1.0
	return []*record.Record{r}, nil
}
