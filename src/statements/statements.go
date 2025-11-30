package statements

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"aagr.xyz/trades/db"
	"aagr.xyz/trades/parser"
	"aagr.xyz/trades/record"
	"golang.org/x/exp/maps"

	log "github.com/sirupsen/logrus"
)

const epsilon = 1e-5

type Statement struct {
	parser        parser.Parser
	directoryName string
	filenames     []string
}

func New(p parser.Parser, directory string, fs []string) *Statement {
	return &Statement{parser: p, directoryName: directory, filenames: fs}
}

func (st *Statement) files(rootDir string) ([]string, error) {
	res := make(map[string]bool)
	for _, f := range st.filenames {
		res[path.Join(rootDir, f)] = true
	}
	if st.directoryName == "" {
		return maps.Keys(res), nil
	}
	// walk the directory and get the names of files
	files, err := os.ReadDir(path.Join(rootDir, st.directoryName))
	if err != nil {
		return nil, fmt.Errorf("cannot read files in directory: %v", err)
	}
	for _, f := range files {
		name := path.Join(rootDir, st.directoryName, f.Name())
		if !strings.HasSuffix(f.Name(), ".csv") {
			log.Warningf("Directory has file not of CSV format, so skipping: %v", name)
			continue
		}
		res[name] = true
	}
	return maps.Keys(res), nil
}

func readRecords(statements []*Statement, rootDir string) ([]*record.Record, error) {
	// Read files and get records
	var records []*record.Record
	for _, st := range statements {
		files, err := st.files(rootDir)
		if err != nil {
			return nil, fmt.Errorf("cannot get files for statement %v: %v", st, err)
		}
		// filenames are prefixed by rootdir
		for _, filename := range files {
			log.Infof("Reading file %s for parsing", filename)
			f, err := os.Open(filename)
			if err != nil {
				return nil, fmt.Errorf("unable to read input file: %v ", err)
			}
			defer f.Close()
			recs, err := parser.Parse(f, st.parser)
			if err != nil {
				return nil, fmt.Errorf("cannot parse records: %v", err)
			}
			records = append(records, recs...)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})
	renamed, err := handleRename(records)
	if err != nil {
		return nil, fmt.Errorf("cannot rename: %v", err)
	}
	dividendTaxAccounted, err := handleDividends(renamed)
	if err != nil {
		return nil, fmt.Errorf("cannot handle dividend tax: %v", err)
	}
	return dividendTaxAccounted, nil
}

func handleDividends(records []*record.Record) ([]*record.Record, error) {
	type key struct {
		ticker  string
		account record.Account
		date    time.Time
	}
	makeKey := func(r *record.Record) key {
		return key{
			ticker:  r.Ticker,
			account: r.Broker,
			date:    r.Timestamp.Truncate(24 * time.Hour),
		}
	}
	byKey := make(map[key][]*record.Record)
	var res []*record.Record
	for _, r := range records {
		switch r.Action {
		case record.Dividend:
			k := makeKey(r)
			rCpy := *r
			byKey[k] = append(byKey[k], &rCpy)
		case record.WitholdingTax:
			// handled in next loop
			continue
		default:
			res = append(res, r)
		}
	}
	for _, r := range records {
		if r.Action != record.WitholdingTax {
			continue
		}
		k := makeKey(r)
		left := r.ShareCount
		for i := 0; i < len(byKey[k]) && left > epsilon; i++ {
			got := math.Min(byKey[k][i].ShareCount, left)
			byKey[k][i].ShareCount -= got
			byKey[k][i].Total -= (got * byKey[k][i].ExchangeRate)
			left -= got
			byKey[k][i].Description += fmt.Sprintf(" tax of %f ;", got)
		}
		if left > epsilon {
			return nil, fmt.Errorf("cannot subtract witholding tax for record %v, left = %v", r, left)
		}
	}
	for _, rs := range byKey {
		for _, r := range rs {
			if r.ShareCount < epsilon {
				continue
			}
			res = append(res, r)
		}
	}
	return res, nil
}

func handleRename(records []*record.Record) ([]*record.Record, error) {
	// rename the tickers to most recent value
	for _, r := range records {
		if r.Action == record.Rename {
			db.RenameSymbol(r.Ticker, r.Description)
		}
	}
	var res []*record.Record
	// now fix the old records
	for _, r := range records {
		if r.Action == record.Rename {
			continue
		}
		rCpy := *r
		rCpy.Ticker = db.MostRecentTicker(r.Ticker)
		name, err := db.TickerName(r.Ticker)
		if err != nil {
			return nil, fmt.Errorf("cannot get name of ticker %s: %v", r.Ticker, err)
		}
		rCpy.Name = name
		res = append(res, &rCpy)
	}
	return res, nil
}

func Records(statements []*Statement, rootDir string) ([]*record.Record, error) {
	defer db.SerializeDB(rootDir)
	records, err := readRecords(statements, rootDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read records: %v", err)
	}
	return records, err
}

func FlushRecords(records []*record.Record, outputFile string) error {
	transactions, err := RecordsToCSV(records)
	if err != nil {
		return fmt.Errorf("cannot convert records to csv: %v", err)
	}
	if err := os.MkdirAll(path.Dir(outputFile), 0755); err != nil {
		return fmt.Errorf("cannot create directories: %v", err)
	}
	if err := os.WriteFile(outputFile, []byte(transactions), 0644); err != nil {
		return fmt.Errorf("cannot write csv file: %v", err)
	}
	return nil
}

func RecordsToCSV(records []*record.Record) (string, error) {
	sb := new(bytes.Buffer)
	w := csv.NewWriter(sb)
	if err := w.Write(records[0].Header()); err != nil {
		return "", fmt.Errorf("cannot write header: %v", err)
	}
	for _, r := range records {
		if err := w.Write(r.MarshalCSV()); err != nil {
			return "", fmt.Errorf("cannot write record: %v", err)
		}
	}
	w.Flush()
	return sb.String(), w.Error()
}
