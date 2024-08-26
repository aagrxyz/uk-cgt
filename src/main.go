package main

import (
	"flag"
	"os"
	"path"

	"aagr.xyz/trades/config"
	"aagr.xyz/trades/db"
	"aagr.xyz/trades/marketdata"
	"aagr.xyz/trades/parser"
	"aagr.xyz/trades/proto/statementspb"
	"aagr.xyz/trades/server"
	"aagr.xyz/trades/statements"
	"aagr.xyz/trades/yahoo"
	"google.golang.org/protobuf/encoding/prototext"

	log "github.com/sirupsen/logrus"
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
	username = config.Env("AUTH_USERNAME", "")
	password = config.Env("AUTH_PASSWORD", "")
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
	if *port > 0 {
		config.SetMode(config.SERVER_MODE)
	}
	// make the output directory
	if err := os.MkdirAll(path.Join(*rootDir, "outputs"), 0755); err != nil {
		log.Fatalf("cannot create output directories: %v", err)
	}
	// Initialize db and yahoo clients
	yc, err := yahoo.NewBackend()
	if err != nil {
		log.Fatalf("cannot create a new yahoo backend: %v", err)
	}
	market := marketdata.NewService(map[marketdata.Source]marketdata.Backend{
		marketdata.YAHOO: yc,
	})
	db.InitDB(*rootDir)
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

	static, err := server.NewStaticLoader(*staticDir)
	if err != nil {
		log.Fatalf("cannot create a static loader: %v", err)
	}
	cfg := &server.Config{
		Statements: sts,
		RootDir:    *rootDir,
		Auth:       server.NewAuthorization(username, password),
		Market:     market,
		Static:     static,
	}
	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("cannot create a new server: %v", err)
	}
	if err := srv.Run(*port); err != nil {
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
