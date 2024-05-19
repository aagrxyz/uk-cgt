package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"aagr.xyz/trades/yahoo"
	"github.com/go-resty/resty/v2"
	"github.com/lithammer/fuzzysearch/fuzzy"
	log "github.com/sirupsen/logrus"
)

const symbolJSONFilename = "outputs/symbols_db.json"

const ForexType = "FOREX"

// Symbol stores metadata about a ticker
type Symbol struct {
	Names         []string `json:"names"`
	Exchange      string   `json:"exchange"`
	YahooTicker   string   `json:"yahoo_ticker"`
	YahooCurrency string   `json:"yahoo_currency"`
	AssetType     string   `json:"asset_type"`
	ETFType       string   `json:"etf_type"`
	Currency      string   `json:"currency"`
}

// symbols stores a map from ticker id to the various names it has.
var symbols map[string]*Symbol

// symbolsRenamed stored which symbols are renamed to which. It stores a disjoint set union.
var symbolsRenamed map[string]string

var yahooClient *resty.Client

// initSymbols is called to get the db initialized. If no file exists, it creates a new file.
func initSymbols(rootDir string) {
	symbols = make(map[string]*Symbol)
	symbolsRenamed = make(map[string]string)
	data, err := os.ReadFile(path.Join(rootDir, symbolJSONFilename))
	if err != nil {
		log.Errorf("Cannot read file for symbols: %v", err)
		return
	}
	err = json.Unmarshal(data, &symbols)
	if err != nil {
		log.Errorf("Cannot unmarshal to struct: %v", err)
		return
	}
}

func serializeSymbols(rootDir string) error {
	data, err := json.Marshal(symbols)
	if err != nil {
		return fmt.Errorf("cannot marshal json: %v", err)
	}
	err = os.WriteFile(path.Join(rootDir, symbolJSONFilename), data, 0666)
	if err != nil {
		return fmt.Errorf("cannot write json file to disk: %v", err)
	}
	return nil
}

func insertTickerName(ticker string, name string) {
	if _, ok := symbols[ticker]; !ok {
		symbols[ticker] = &Symbol{}
	}
	for _, x := range symbols[ticker].Names {
		if name == x {
			return
		}
	}
	symbols[ticker].Names = append(symbols[ticker].Names, name)
	sort.Sort(sort.StringSlice(symbols[ticker].Names))
}

func getNameOfTicker(ticker string) (string, error) {
	resp, err := yahoo.Search(yahooClient, ticker)
	if err == nil && resp.ShortName != "" {
		return resp.ShortName, nil
	}
	log.Warningf("Error in fetching name from yahoo finance: %v", err)
	manual, err := getInput(fmt.Sprintf("Cannot get name for ticker %s, Please enter manually", ticker))
	if err != nil {
		return "", err
	}
	return manual, nil
}

// AddTickerName adds a ticker to the DB, if the name is not set, then it is asked to be entered manually
func AddTickerName(ticker string, name string) error {
	if name == "" {
		manual, err := getNameOfTicker(ticker)
		if err != nil {
			return err
		}
		name = manual
	}
	insertTickerName(ticker, name)
	return nil
}

// TickerName returns the name of the ticker.
// If it is not present, a new entry is created
func TickerName(ticker string) (string, error) {
	if _, ok := symbols[ticker]; !ok {
		if err := AddTickerName(ticker, ""); err != nil {
			return "", fmt.Errorf("cannot add ticker to db: %v", err)
		}
	}
	return symbols[ticker].Names[0], nil
}

// TickerMeta returns the metadata about a ticker
func TickerMeta(ticker string) (*Symbol, error) {
	ticker = MostRecentTicker(ticker)
	meta, ok := symbols[ticker]
	if !ok {
		return nil, fmt.Errorf("ticker %s not found", ticker)
	}
	return meta, nil
}

// SetCurrency sets the currency from records to the db
func SetCurrency(ticker, currency string) error {
	currency = strings.ToUpper(currency)
	meta, ok := symbols[ticker]
	if !ok {
		return fmt.Errorf("ticker not added before")
	}
	if meta.Currency != "" && meta.Currency != currency {
		return fmt.Errorf("ticker %s cannot have two different currencies (old=%s, new=%s)", ticker, meta.Currency, currency)
	}
	meta.Currency = currency
	// This only happens in forex records
	if ticker == currency {
		meta.AssetType = ForexType
	}
	return nil
}

// GuessTickerFromName tries to identify the ticker for a given name
func GuessTickerFromName(name string) (string, error) {
	var data []string
	var inverseMap map[string]string = make(map[string]string)
	for ticker, meta := range symbols {
		for _, name := range meta.Names {
			data = append(data, name)
			inverseMap[name] = ticker
		}
	}
	matches := fuzzy.RankFindFold(name, data)
	sort.Sort(matches)
	vals := []fuzzy.Rank(matches)
	if len(matches) > 0 {
		return inverseMap[vals[0].Target], nil
	}
	// Now we couldn't guess the ticker name from here, so ask manually.
	manual, err := getInput(fmt.Sprintf("Cannot get ticker from name %s, Please enter manually", name))
	if err != nil {
		return "", err
	}
	AddTickerName(manual, name)
	return manual, nil
}

// RenameSymbol stores a rename from old to new
func RenameSymbol(old, new string) {
	symbolsRenamed[old] = new
}

// MostRecentTicker returns the most recent ticker name
func MostRecentTicker(ticker string) string {
	new, ok := symbolsRenamed[ticker]
	if !ok {
		return ticker
	}
	return MostRecentTicker(new)
}

// EnrichFromYahoo queries yahoo finance and updates the metadata.
func EnrichFromYahoo() error {
	for ticker, meta := range symbols {
		if MostRecentTicker(ticker) != ticker {
			// This ticker is renamed, so no point storing stuff for it
			continue
		}
		// Do not play around with forex
		if meta.AssetType == ForexType {
			continue
		}
		symbol := meta.YahooTicker
		if symbol == "" {
			symbol = ticker
			if strings.HasPrefix(meta.Currency, "GB") {
				symbol += ".L"
			}
			if meta.Currency == "CHF" {
				symbol += ".SW"
			}
		}
		if err := fillMetaFromYahoo(symbol, meta, yahooClient); err != nil {
			return err
		}
	}
	return nil
}

func fillMetaFromYahoo(symbol string, meta *Symbol, yahooClient *resty.Client) error {
	success := false
	var fetchErr error
	for i := 0; i < 2; i++ {
		quote, err := yahoo.GetQuote(yahooClient, symbol)
		if err == nil {
			meta.YahooTicker = symbol
			meta.Exchange = quote.ExchangeName
			meta.AssetType = quote.QuoteType
			meta.YahooCurrency = quote.Currency
			success = true
			break
		}
		manual, err := getInput(fmt.Sprintf("Cannot determine yahoo finance symbol for ticker %s. Enter Manually:", symbol))
		if err != nil {
			return err
		}
		symbol = manual
		fetchErr = err
	}
	if !success {
		return fmt.Errorf("cannot fill data from yahoo finance, latest fetch err: %v", fetchErr)
	}
	return nil
}
