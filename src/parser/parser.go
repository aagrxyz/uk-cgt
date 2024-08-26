package parser

import (
	"encoding/csv"
	"fmt"
	"io"

	"aagr.xyz/trades/db"
	"aagr.xyz/trades/record"
)

const timeFmt = "2006-01-02 15:04:05"

// Parser is the interface to parse records of 1 type to another.
// Different brokers have their different CSV files, so have different go codes.
type Parser interface {
	ValidateHeader(contents []string) error
	ToRecord(contents []string) ([]*record.Record, error)
}

func headerMatches(want map[int]string, contents []string) error {
	for idx, name := range want {
		if idx >= len(contents) {
			return fmt.Errorf("not many fields in header, want %q at index %d, got %v", name, idx, contents)
		}
		if name != contents[idx] {
			return fmt.Errorf("mismatch field name in header, want %q at index %d, got %q", name, idx, contents[idx])
		}
	}
	return nil
}

// Parse parses a file into records
func Parse(in io.Reader, parser Parser) ([]*record.Record, error) {
	f := csv.NewReader(in)
	f.TrimLeadingSpace = true
	header, err := f.Read()
	if err != nil {
		return nil, fmt.Errorf("cannot read header: %v", err)
	}
	if err := parser.ValidateHeader(header); err != nil {
		return nil, fmt.Errorf("cannot validate header: %v", err)
	}
	var res []*record.Record
	for {
		contents, err := f.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("cannot read record: %v", err)
		}
		rr, err := parser.ToRecord(contents)
		if err != nil {
			return nil, fmt.Errorf("cannot convert to a record: %v", err)
		}
		if len(rr) == 0 {
			continue
		}
		for _, r := range rr {
			if err := validateAndEnrich(r); err != nil {
				return nil, fmt.Errorf("cannot validate and enrich record: %v", err)
			}
			res = append(res, r)
		}
	}
	return res, nil
}

func validateAndEnrich(r *record.Record) error {
	// Let's store everything in GBP
	if r.Currency == record.GBX {
		r.PricePerShare /= 100.0
		r.Currency = record.GBP
		r.ExchangeRate = 1.0
	}
	if err := r.AssertMaths(); err != nil {
		return fmt.Errorf("cannot assert the maths for the record (record = %s): %v", r.String(), err)
	}
	if err := db.FillTickerOrName(r); err != nil {
		return fmt.Errorf("cannot fill ticker or name from db: %v", err)
	}
	// If it is not a buy or sell transaction, then don't fiddle around with currency
	if !(r.Action == record.Sell || r.Action == record.Buy) {
		return nil
	}
	if err := db.SetCurrency(r.Ticker, r.Currency); err != nil {
		return fmt.Errorf("cannot set currency of the ticker (record = %s): %v", r.String(), err)
	}
	return nil
}
