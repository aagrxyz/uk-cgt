package main

import (
	"flag"
	"os"
	"path"

	"aagr.xyz/trades/src/db"
	"aagr.xyz/trades/src/parser"
	"aagr.xyz/trades/src/server"
	"aagr.xyz/trades/src/statements"
	"aagr.xyz/trades/src/yahoo"
	"github.com/go-resty/resty/v2"

	log "github.com/sirupsen/logrus"
)

const (
	outputTransactions = "outputs/merged_transactions.csv"
	reportFilename     = "outputs/report.txt"
	ghostfolioFile     = "outputs/ghostfolio.json"
)

var (
	rootDir          = flag.String("root_dir", "", "The root directory for outputs")
	transactionsFile = flag.String("transactions_file", "", "The file for merged transactions")
	port             = flag.Int("port", 0, "The port to run the web server on")
	sts              []*statements.Statement
)

func main() {
	var err error
	flag.Parse()
	// if rootDir is empty -> try to infer
	if *rootDir == "" {
		if *rootDir, err = os.Getwd(); err != nil {
			log.Fatalf("cannot get working directory")
		}
	}
	// Initialize db and yahoo clients
	yahooClient := yahoo.New(resty.New(), resty.New())
	yahoo.RefreshSession(yahooClient, resty.New())
	db.InitDB(*rootDir, yahooClient)

	if *transactionsFile != "" {
		sts = append(sts, statements.New(parser.NewDefault(), "", []string{*transactionsFile}))
	}

	records, err := statements.Records(sts, *rootDir)
	if err != nil {
		log.Fatalf("cannot read records: %v", err)
	}
	// Flush these transactions to disk
	if err := statements.FlushRecords(records, path.Join(*rootDir, outputTransactions)); err != nil {
		log.Fatalf("cannot flush merged transactions to disk: %v", err)
	}

	srv := server.New(records, yahooClient)
	if err := srv.Run(*port, path.Join(*rootDir, reportFilename)); err != nil {
		log.Fatalf("cannot run server: %v", err)
	}
}

// func ghostfolio(){
// 	activities, err := ghostfolio.ToActivities(records)
// 	if err != nil {
// 		log.Fatalf("cannot get activities for ghostfolio: %v", err)
// 	}
// 	marshalled, err := json.Marshal(activities)
// 	if err != nil {
// 		log.Fatalf("cannot marshal activities to json: %v", err)
// 	}
// 	if err := os.WriteFile(path.Join(rootDir, ghostfolioFile), marshalled, 0644); err != nil {
// 		log.Fatalf("cannot write to ghostfolio file: %v", err)
// 	}
// }
