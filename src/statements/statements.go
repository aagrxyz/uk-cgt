package statements

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"aagr.xyz/trades/db"
	"aagr.xyz/trades/parser"
	"aagr.xyz/trades/record"
	"golang.org/x/exp/maps"

	log "github.com/sirupsen/logrus"
)

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
	if err := db.EnrichFromYahoo(); err != nil {
		log.Errorf("Cannot enrich db from yahoo finance, present data may be inaccurate: %v", err)
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
