package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"

	"aagr.xyz/trades/marketdata"
	"aagr.xyz/trades/record"
	"github.com/lithammer/fuzzysearch/fuzzy"
	log "github.com/sirupsen/logrus"
)

const symbolJSONFilename = "outputs/symbols_db.json"

type SymbolState string

const (
	Active   SymbolState = "Active"
	Inactive SymbolState = "Inactive"
)

// Symbol stores metadata about a ticker
type Symbol struct {
	Names     []string         `json:"names"`
	Currency  record.Currency  `json:"currency"`
	AssetType record.AssetType `json:"asset_type"`
	ETFType   string           `json:"etf_type"`
	State     SymbolState      `json:"state"`

	Metadata map[marketdata.Source]*marketdata.SourceMetadata `json:"source_metadata"`
}

var (
	// symbols stores a map from ticker id to the various names it has.
	symbols map[string]*Symbol
	// symbolsRenamed stored which symbols are renamed to which. It stores a disjoint set union.
	symbolsRenamed map[string]string
)

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
func SetCurrency(ticker string, currency record.Currency) error {
	meta, ok := symbols[ticker]
	if !ok {
		return fmt.Errorf("ticker not added before")
	}
	if meta.Currency != "" && meta.Currency != currency {
		return fmt.Errorf("ticker %s cannot have two different currencies (old=%s, new=%s)", ticker, meta.Currency, currency)
	}
	meta.Currency = currency
	// This only happens in forex records
	if ticker == string(currency) {
		meta.AssetType = record.FOREX_ASSET
	}
	return nil
}

func insertTickerName(ticker string, name string) error {
	if _, ok := symbols[ticker]; !ok {
		symbols[ticker] = &Symbol{}
	}
	if name == "" {
		manual, err := getNameOfTicker(ticker)
		if err != nil {
			return err
		}
		name = manual
	}
	for _, x := range symbols[ticker].Names {
		if name == x {
			return nil
		}
	}
	symbols[ticker].Names = append(symbols[ticker].Names, name)
	sort.Sort(sort.StringSlice(symbols[ticker].Names))
	return nil
}

// TickerName returns the name of the ticker.
// If it is not present, a new entry is created
func TickerName(ticker string) (string, error) {
	if _, ok := symbols[ticker]; !ok {
		if err := insertTickerName(ticker, ""); err != nil {
			return "", fmt.Errorf("cannot add ticker to db: %v", err)
		}
	}
	return symbols[ticker].Names[0], nil
}

// guessTickerFromName tries to identify the ticker for a given name
func guessTickerFromName(name string) (string, error) {
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
	insertTickerName(manual, name)
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

// Function that modifies the record
func FillTickerOrName(r *record.Record) error {
	if r.Ticker == "" && r.Name == "" {
		return fmt.Errorf("both name and ticker are empty")
	}
	var err error
	if r.Ticker != "" && r.Name == "" {
		err = fillName(r)
	}
	if r.Name != "" && r.Ticker == "" {
		err = fillTicker(r)
	}
	if err != nil {
		return err
	}
	insertTickerName(r.Ticker, r.Name)
	return nil
}

func fillName(r *record.Record) error {
	name, err := TickerName(r.Ticker)
	if err != nil {
		return err
	}
	r.Name = name
	return nil
}

func fillTicker(r *record.Record) error {
	ticker, err := guessTickerFromName(r.Name)
	if err != nil {
		return err
	}
	r.Ticker = ticker
	return nil
}

// Market data functions
func getNameOfTicker(ticker string) (string, error) {
	// resp, err := marketdata.Search(ticker)
	// if err == nil && resp.ShortName != "" {
	// 	return resp.ShortName, nil
	// }
	// log.Warningf("Error in fetching name from yahoo finance: %v", err)
	manual, err := getInput(fmt.Sprintf("Cannot get name for ticker %s, Please enter manually", ticker))
	if err != nil {
		return "", err
	}
	return manual, nil
}

// EnrichFromMarket queries yahoo finance and updates the metadata.
func EnrichFromMarket(md *marketdata.Service) error {
	for ticker, meta := range symbols {
		// This ticker is renamed, so no point storing stuff for it
		if MostRecentTicker(ticker) != ticker {
			continue
		}
		// Do not play around with forex
		if meta.AssetType == record.FOREX_ASSET {
			continue
		}
		// If symbol is inactive, then ignore it.
		if meta.State == Inactive {
			continue
		}
		mds, err := md.Metadata(ticker, meta.Currency, meta.Metadata)
		if err != nil {
			return fmt.Errorf("cannot enrich from market: %v", err)
		}
		meta.Metadata = mds
	}
	return nil
}
