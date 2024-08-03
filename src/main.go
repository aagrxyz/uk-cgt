package main

import (
	"flag"
	"os"
	"path"

	"aagr.xyz/trades/db"
	"aagr.xyz/trades/parser"
	"aagr.xyz/trades/proto/statementspb"
	"aagr.xyz/trades/server"
	"aagr.xyz/trades/statements"
	"aagr.xyz/trades/yahoo"
	"github.com/go-resty/resty/v2"
	"google.golang.org/protobuf/encoding/prototext"

	log "github.com/sirupsen/logrus"
)

const (
	outputTransactions = "outputs/merged_transactions.csv"
	reportFilename     = "outputs/report.txt"
	ghostfolioFile     = "outputs/ghostfolio.json"
)

var (
	rootDir            = flag.String("root_dir", "", "The root directory for outputs")
	staticDir          = flag.String("static_dir", "", "The root directory for static files")
	transactionsFile   = flag.String("transactions_file", "", "The file for merged transactions")
	configFile         = flag.String("config_file", "", "The file for parsing config textproto")
	port               = flag.Int("port", 0, "The port to run the web server on")
	username, password string
)

func init() {
	username = Env("AUTH_USERNAME", "")
	password = Env("AUTH_PASSWORD", "")
}

func main() {
	var err error
	flag.Parse()
	// if rootDir is empty -> try to infer
	if *rootDir == "" {
		if *rootDir, err = os.Getwd(); err != nil {
			log.Fatalf("cannot get working directory")
		}
	}
	// make the output directory
	if err := os.MkdirAll(path.Join(*rootDir, "outputs"), 0755); err != nil {
		log.Fatalf("cannot create output directories: %v", err)
	}
	// Initialize db and yahoo clients
	yahooClient := yahoo.New(resty.New(), resty.New())
	yahoo.RefreshSession(yahooClient, resty.New())
	db.InitDB(*rootDir, yahooClient)
	var sts []*statements.Statement

	if *configFile != "" {
		b, err := os.ReadFile(*configFile)
		if err != nil {
			log.Fatalf("cannot read the config file: %v", err)
		}
		var cfg = &statementspb.Statements{}
		if err := prototext.Unmarshal(b, cfg); err != nil {
			log.Fatalf("cannot unmarshal statements config file: %v", err)
		}
		parsed, err := statements.FromProtoConfig(cfg)
		if err != nil {
			log.Fatalf("cannot parse the statements config: %v", err)
		}
		sts = append(sts, parsed...)
	}

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
	static, err := server.NewStaticLoader(*staticDir)
	if err != nil {
		log.Fatalf("cannot create a static loader: %v", err)
	}
	srv := server.New(records, yahooClient, server.NewAuth(username, password), static)
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
