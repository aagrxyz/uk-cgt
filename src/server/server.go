package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"aagr.xyz/trades/holdings"
	"aagr.xyz/trades/record"
	"aagr.xyz/trades/statements"
	"golang.org/x/exp/maps"

	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

const (
	timeFmt = "2006-01-02 15:04:05"
)

type Auth struct {
	username, password string
}

func NewAuth(u, p string) *Auth {
	return &Auth{
		username: u,
		password: p,
	}
}

type Server struct {
	static      *StaticLoader
	auth        *Auth
	yahooClient *resty.Client
	records     []*record.Record
	byAccount   map[record.Account]*holdings.Account
	byTicker    map[string]*holdings.Holding
}

func New(records []*record.Record, yc *resty.Client, auth *Auth, static *StaticLoader) *Server {
	return &Server{
		static:      static,
		yahooClient: yc,
		auth:        auth,
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
	http.HandleFunc("/portfolio", s.basicAuth(s.portfolioHandler))
	http.HandleFunc("/accounts", s.basicAuth(s.accountHandler))
	http.HandleFunc("/cgt", s.basicAuth(s.cgtHandler))
	http.HandleFunc("/csv/portfolio", s.basicAuth(s.portfolioCSVHandler))
	http.HandleFunc("/csv/accounts", s.basicAuth(s.accountsCSVHandler))
	http.HandleFunc("/csv/transactions", s.basicAuth(s.transactionsHandler))
	http.HandleFunc("/quit/quit/quit", s.basicAuth(s.quit))
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

func (s *Server) quit(w http.ResponseWriter, r *http.Request) {
	os.Exit(-1)
}

func (s *Server) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.auth.username == "" || s.auth.password == "" {
			next.ServeHTTP(w, r)
			return
		}
		username, password, ok := r.BasicAuth()
		if ok {
			usernameHash := sha256.Sum256([]byte(username))
			passwordHash := sha256.Sum256([]byte(password))
			expectedUsernameHash := sha256.Sum256([]byte(s.auth.username))
			expectedPasswordHash := sha256.Sum256([]byte(s.auth.password))

			usernameMatch := (subtle.ConstantTimeCompare(usernameHash[:], expectedUsernameHash[:]) == 1)
			passwordMatch := (subtle.ConstantTimeCompare(passwordHash[:], expectedPasswordHash[:]) == 1)

			if usernameMatch && passwordMatch {
				next.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func (s *Server) portfolioHandler(w http.ResponseWriter, r *http.Request) {
	rows := holdings.PortfolioRows(s.byTicker, s.yahooClient)
	tmpl := s.static.Portfolio()
	if tmpl == nil {
		http.Error(w, "no content", http.StatusInternalServerError)
		return
	}
	type Data struct {
		Timestamp                        string
		Rows                             []*holdings.TickerRow
		TotalCost, TotalValue, TotalGain float64
		TotalGainPercentage              float64
	}
	// sort the place where you lost most money
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].GBPPriceMetrics.TotalGain < rows[j].GBPPriceMetrics.TotalGain
	})
	d := Data{Timestamp: time.Now().Format(timeFmt), Rows: rows}
	for _, r := range rows {
		d.TotalCost += r.GBPPriceMetrics.TotalCost
		d.TotalValue += r.GBPPriceMetrics.TotalValue
		d.TotalGain += r.GBPPriceMetrics.TotalGain
	}
	d.TotalGainPercentage = d.TotalGain / d.TotalCost * 100.0
	err := tmpl.Execute(w, d)
	if err != nil {
		log.Errorf("cannot execute template: %v", err)
		http.Error(w, "cannot generate portfolio", http.StatusInternalServerError)
	}
}
func (s *Server) portfolioCSVHandler(w http.ResponseWriter, r *http.Request) {
	portfolio := holdings.PortfolioTable(s.byTicker, s.yahooClient)
	fmt.Fprint(w, portfolio.RenderCSV())
}

func (s *Server) transactionsHandler(w http.ResponseWriter, r *http.Request) {
	res, err := statements.RecordsToCSV(s.records)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, res)
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
	byAct := holdings.AccountRows(s.byAccount, s.yahooClient)
	tmpl := s.static.Accounts()
	if tmpl == nil {
		http.Error(w, "no content", http.StatusInternalServerError)
		return
	}
	type AccountData struct {
		Name                             string
		Rows                             []*holdings.TickerRow
		TotalCost, TotalValue, TotalGain float64
		TotalGainPercentage              float64
	}
	type Data struct {
		Timestamp string
		Accounts  []*AccountData
	}

	d := Data{Timestamp: time.Now().Format(timeFmt)}
	for act, rows := range byAct {
		ad := &AccountData{Name: act.Name}
		// sort the place where you lost most money
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].GBPPriceMetrics.TotalGain < rows[j].GBPPriceMetrics.TotalGain
		})
		ad.Rows = rows
		for _, r := range rows {
			ad.TotalCost += r.GBPPriceMetrics.TotalCost
			ad.TotalValue += r.GBPPriceMetrics.TotalValue
			ad.TotalGain += r.GBPPriceMetrics.TotalGain
		}
		ad.TotalGainPercentage = ad.TotalGain / ad.TotalCost * 100.0
		d.Accounts = append(d.Accounts, ad)
	}
	err := tmpl.Execute(w, d)
	if err != nil {
		log.Errorf("cannot execute template: %v", err)
		http.Error(w, "cannot generate accounts", http.StatusInternalServerError)
	}
}

func (s *Server) accountsCSVHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Account Stats: %s", time.Now().Format(timeFmt))
	accounts := holdings.AccountTable(s.byAccount, s.yahooClient)
	i := 0
	for _, act := range accounts {
		act.SetTitle("")
		act.ResetFooters()
		if i > 0 {
			act.ResetHeaders()
		}
		fmt.Fprintf(w, "\n%s", act.RenderCSV())
		i++
	}
}

func (s *Server) writeReport(filename string) error {
	if err := os.MkdirAll(path.Dir(filename), 0755); err != nil {
		return fmt.Errorf("cannot create directories: %v", err)
	}
	portfolio := holdings.PortfolioTable(s.byTicker, s.yahooClient)
	cgt, debug := holdings.CGT(s.byTicker)
	accounts := holdings.AccountTable(s.byAccount, s.yahooClient)

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
