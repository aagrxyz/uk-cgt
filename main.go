package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"aagr.xyz/trades/src/db"
	"aagr.xyz/trades/src/ghostfolio"
	"aagr.xyz/trades/src/holdings"
	"aagr.xyz/trades/src/parser"
	"aagr.xyz/trades/src/record"
	"aagr.xyz/trades/src/yahoo"
	"github.com/go-resty/resty/v2"
	"github.com/jedib0t/go-pretty/v6/table"
	log "github.com/sirupsen/logrus"
	"golang.org/x/exp/maps"
)

const (
	transactionFilename = "outputs/merged_transactions.csv"
	reportFilename      = "outputs/report.txt"
	ghostfolioFile      = "outputs/ghostfolio.json"
	timeFmt             = "2006-01-02 15:04:05"
)

var rootDir string

func init() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("cannot get working directory")
	}
	rootDir = root
}

type statement struct {
	parser        parser.Parser
	directoryName string
	filenames     []string
}

var (
	fxIsAsset        = flag.Bool("fx_is_asset", true, "Whether forex transactions are an asset")
	transactionsFile = flag.String("transactions_file", "", "The file for merged transactions")

	yahooClient *resty.Client
	statements  []*statement
)

func init() {
	yahooClient = yahoo.New(resty.New(), resty.New())
	yahoo.RefreshSession(yahooClient, resty.New())
	db.InitDB(rootDir, yahooClient)
}

func getFiles(st *statement) ([]string, error) {
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

func readRecords() ([]*record.Record, error) {
	// Read files and get records
	var records []*record.Record
	for _, st := range statements {
		files, err := getFiles(st)
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

func main() {
	flag.Parse()
	defer db.SerializeDB(rootDir)
	if *transactionsFile != "" {
		statements = append(statements, &statement{
			parser:    parser.NewDefault(),
			filenames: []string{*transactionsFile},
		})
	}

	records, err := readRecords()
	if err != nil {
		log.Fatalf("Cannot read records: %v", err)
	}
	db.SerializeDB(rootDir)
	if err := db.EnrichFromYahoo(); err != nil {
		db.SerializeDB(rootDir)
		log.Errorf("Cannot enrich db from yahoo finance, present data may be inaccurate: %v", err)
	}
	db.SerializeDB(rootDir)

	// Write transactions in a common format to CSV file
	if err := writeTransactions(records); err != nil {
		log.Fatalf("cannot write transactions")
	}

	// calculate holdings
	state, err := holdings.Calculate(records, *fxIsAsset)
	if err != nil {
		log.Fatalf("Cannot compute holdings: %v", err)
	}

	portfolio := holdings.Portfolio(state, yahooClient)
	cgt, debug := holdings.CGT(state)

	fmt.Println(portfolio.Render())
	fmt.Println(cgt)

	if err := writeReport(portfolio, cgt, debug); err != nil {
		log.Fatalf("cannot write report: %v", err)
	}

	activities, err := ghostfolio.ToActivities(records)
	if err != nil {
		log.Fatalf("cannot get activities for ghostfolio: %v", err)
	}
	marshalled, err := json.Marshal(activities)
	if err != nil {
		log.Fatalf("cannot marshal activities to json: %v", err)
	}
	if err := os.WriteFile(path.Join(rootDir, ghostfolioFile), marshalled, 0644); err != nil {
		log.Fatalf("cannot write to ghostfolio file: %v", err)
	}

}

func writeReport(portfolio table.Writer, cgt, debug string) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("DATE: %s\n\n", time.Now().Format(timeFmt)))
	sb.WriteString(fmt.Sprintf("%s\n\n", portfolio.Render()))
	sb.WriteString(fmt.Sprintf("%s\n\n", portfolio.RenderCSV()))
	sb.WriteString(fmt.Sprintf("%s\n\n", cgt))
	sb.WriteString(fmt.Sprintf("%s\n\n", debug))
	if err := os.WriteFile(path.Join(rootDir, reportFilename), []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("cannot output report: %v", err)
	}
	return nil
}

func writeTransactions(records []*record.Record) error {
	transactions, err := recordsToCSV(records)
	if err != nil {
		return fmt.Errorf("cannot convert records to csv: %v", err)
	}
	if err := os.WriteFile(path.Join(rootDir, transactionFilename), []byte(transactions), 0644); err != nil {
		return fmt.Errorf("cannot write csv file: %v", err)
	}
	return nil
}

func recordsToCSV(records []*record.Record) (string, error) {
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
