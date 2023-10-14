package parser

import (
	"encoding/csv"
	"fmt"
	"io"

	"aagr.xyz/trades/src/record"
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
			if err := r.ValidateAndEnrich(); err != nil {
				return nil, fmt.Errorf("cannot validate and enrich record: %v", err)
			}
			res = append(res, r)
		}
	}
	return res, nil
}
