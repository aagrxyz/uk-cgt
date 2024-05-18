package server

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"aagr.xyz/trades/src/holdings"
	"aagr.xyz/trades/src/record"
	"golang.org/x/exp/maps"

	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

const (
	timeFmt = "2006-01-02 15:04:05"
)

type Server struct {
	yahooClient *resty.Client
	records     []*record.Record
	byAccount   map[record.Account]*holdings.Account
	byTicker    map[string]*holdings.Holding
}

func New(records []*record.Record, yc *resty.Client) *Server {
	return &Server{
		yahooClient: yc,
		records:     records,
		byAccount:   make(map[record.Account]*holdings.Account),
		byTicker:    make(map[string]*holdings.Holding),
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "OK. Healthy!\n") // send healthy data
}

func (s *Server) Run(port int, filename string) error {
	if err := s.Update(); err != nil {
		return fmt.Errorf("cannot update server state: %v", err)
	}
	if filename != "" {
		if err := s.writeReport(filename); err != nil {
			return fmt.Errorf("cannot write report: %v", err)
		}
	}
	http.HandleFunc("/", s.healthz)
	http.HandleFunc("/healthz", s.healthz)
	http.HandleFunc("/portfolio", s.portfolioHandler)
	http.HandleFunc("/accounts", s.accountHandler)
	http.HandleFunc("/cgt", s.cgtHandler)
	if port <= 0 {
		return nil
	}
	log.Infof("Starting HTTP server on :%d", port)
	return http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}

func (s *Server) Update() error {
	var err error
	s.byAccount, err = holdings.ByAccount(s.records)
	if err != nil {
		return fmt.Errorf("cannot get holdings by accounts: %v", err)
	}
	s.byTicker, err = holdings.ByTicker(s.records)
	if err != nil {
		return fmt.Errorf("Cannot compute holdings by ticker: %v", err)
	}
	return nil
}

func (s *Server) portfolioHandler(w http.ResponseWriter, r *http.Request) {
	portfolio := holdings.Portfolio(s.byTicker, s.yahooClient)
	fmt.Fprintf(w, `
	<!DOCTYPE html>
	<html>
	<head>
		<title>DATE: %s</title>
	</head>
	<body>
	%s
	</body>
	</html>`, time.Now().Format(timeFmt), portfolio.RenderHTML())
}

func (s *Server) cgtHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
	<!DOCTYPE html>
	<html>
	<head>
		<title>CGT Calculation Report: %s</title>
	</head>
	<body>`, time.Now().Format(timeFmt))
	cgt, _ := holdings.CGT(s.byTicker)
	years := maps.Keys(cgt)
	sort.Strings(years)
	for _, y := range years {
		fmt.Fprint(w, cgt[y].RenderHTML())
		fmt.Fprint(w, "<br><br>")
	}
	fmt.Fprint(w, `</body></html>`)
}

func (s *Server) accountHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
	<!DOCTYPE html>
	<html>
	<head>
		<title>Account Stats: %s</title>
	</head>
	<body>`, time.Now().Format(timeFmt))
	accounts := holdings.AccountStats(s.byAccount, s.yahooClient)
	for _, act := range accounts {
		fmt.Fprint(w, act.RenderHTML())
		fmt.Fprint(w, "<br><br>")
	}
	fmt.Fprint(w, `</body></html>`)
}

func (s *Server) writeReport(filename string) error {
	portfolio := holdings.Portfolio(s.byTicker, s.yahooClient)
	cgt, debug := holdings.CGT(s.byTicker)
	accounts := holdings.AccountStats(s.byAccount, s.yahooClient)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("DATE: %s\n\n", time.Now().Format(timeFmt)))
	sb.WriteString("--------- PORTFOLIO --------\n\n")
	sb.WriteString(fmt.Sprintf("%s\n\n", portfolio.Render()))
	sb.WriteString(fmt.Sprintf("%s\n\n", portfolio.RenderCSV()))
	sb.WriteString("--------- Accounts --------\n\n")
	for _, act := range accounts {
		sb.WriteString(fmt.Sprintf("%s\n\n", act.Render()))
	}
	sb.WriteString("--------- CGT Calculation Report --------\n\n")
	years := maps.Keys(cgt)
	sort.Strings(years)
	for _, y := range years {
		sb.WriteString(fmt.Sprintf("%s\n\n", cgt[y].Render()))
	}
	sb.WriteString(fmt.Sprintf("-------------------------- DEBUG INFO ----------------\n\n%s", debug))
	if err := os.WriteFile(filename, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("cannot output report: %v", err)
	}
	return nil
}
