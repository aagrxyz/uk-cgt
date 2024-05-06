// Package ghostfolio returns transactions in a format that is understood by Ghostfolio.
package ghostfolio

import (
	"fmt"
	"sort"
	"time"

	"aagr.xyz/trades/src/db"
	"aagr.xyz/trades/src/record"
	log "github.com/sirupsen/logrus"
)

// Activity stores a transaction for ghostfolio
type Activity struct {
	DataSource string  `json:"dataSource"`
	Date       string  `json:"date"`
	Type       string  `json:"type"`
	Quantity   float64 `json:"quantity"`
	// these have to be customized based on yahoo data
	UnitPrice float64 `json:"unitPrice"`
	Fee       float64 `json:"fee"`
	// These 2 need to be from yahoo
	Currency string `json:"currency"`
	Symbol   string `json:"symbol"`
	// This is filled in by the ToAccountID function
	AccountID string `json:"accountId"`
	Comment   string `json:"comment"`
}

type Activities struct {
	Act []*Activity `json:"activities"`
}

var ToAccountID func(r *record.Record) (string, error)

func ToActivities(recordsOrig []*record.Record) (*Activities, error) {
	var records []*record.Record
	for _, r := range recordsOrig {
		rCopy := *r
		records = append(records, &rCopy)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})
	var activities map[string][]*Activity = make(map[string][]*Activity)
	for _, r := range records {
		symbol, err := db.TickerMeta(r.Ticker)
		if err != nil {
			return nil, fmt.Errorf("Cannot get metadata of the ticker %s: %v", r.Ticker, err)
		}
		switch r.Action {
		case record.Unknown, record.Rename, record.Dividend, record.CashIn, record.CashOut:
			log.Warningf("Invalid type record: %v, skipping", r)
		case record.Buy, record.Sell:
			a, err := toActivity(r, symbol)
			if err != nil {
				return nil, fmt.Errorf("cannot convert to activity %v: %v", r, err)
			}
			if a == nil {
				continue
			}
			activities[a.Symbol] = append(activities[a.Symbol], a)
		case record.Split:
			if err := handleSplit(activities[symbol.YahooTicker], r.Description); err != nil {
				return nil, fmt.Errorf("cannot handle split: %v", r)
			}
		case record.TransferIn:
			log.Infof("transfer in record %v is handled by it's transfer out", r)
		case record.TransferOut:
			acts, err := handleTransfer(r, symbol)
			if err != nil {
				return nil, fmt.Errorf("cannot handle transfer transaction: %v", err)
			}
			activities[symbol.YahooTicker] = append(activities[symbol.YahooTicker], acts...)
		}
	}
	var res []*Activity
	for _, as := range activities {
		res = append(res, as...)
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Date < res[j].Date
	})
	return &Activities{Act: res}, nil
}

func handleSplit(activities []*Activity, description string) error {
	var newCt, oldCt int64
	_, err := fmt.Sscanf(description, "%d FOR %d", &newCt, &oldCt)
	if err != nil {
		return fmt.Errorf("error in parsing transaction %s", description)
	}
	factor := float64(newCt) / float64(oldCt)
	for _, a := range activities {
		a.Quantity *= factor
		a.UnitPrice /= factor
	}
	return nil
}

func handleTransfer(r *record.Record, meta *db.Symbol) ([]*Activity, error) {
	sellRecord := *r
	sellRecord.Action = record.Sell
	sell, err := toActivity(&sellRecord, meta)
	if err != nil {
		return nil, fmt.Errorf("cannot break transfer to sell: %v", err)
	}
	buyRecord := *r
	buyRecord.Action = record.Buy
	buyRecord.Broker = record.Account{Name: r.Description}
	buy, err := toActivity(&buyRecord, meta)
	if err != nil {
		return nil, fmt.Errorf("cannot break transfer to buy: %v", err)
	}
	return []*Activity{sell, buy}, nil

}

func toActivity(r *record.Record, meta *db.Symbol) (*Activity, error) {
	if meta.AssetType == db.ForexType {
		log.Warningf("Ignoring transaction for forex: %v", r)
		return nil, nil
	}
	a := &Activity{
		Date:       r.Timestamp.Format(time.RFC3339),
		DataSource: "YAHOO",
		Type:       r.Action.String(),
		Quantity:   r.ShareCount,
	}
	if ToAccountID != nil {
		id, err := ToAccountID(r)
		if err != nil {
			return nil, fmt.Errorf("cannot get accountID for record: %v", err)
		}
		a.AccountID = id
	}
	a.Symbol = r.Ticker
	a.Currency = string(r.Currency)

	if meta != nil && meta.YahooTicker != "" {
		a.Symbol = meta.YahooTicker
	}
	if meta != nil && meta.YahooCurrency != "" {
		a.Currency = meta.YahooCurrency
	}
	conversion, err := conversionFactor(meta.YahooCurrency, string(r.Currency))
	if err != nil {
		return nil, err
	}
	a.UnitPrice = r.PricePerShare * conversion
	// Commission is always in GBP, so need to convert it first
	// to record currency and then to yahoo currency
	a.Fee = (r.Commission / r.ExchangeRate) * conversion
	return a, nil
}

func conversionFactor(yahoo, r string) (float64, error) {
	if yahoo == r {
		return 1.0, nil
	}
	if r == "GBP" && yahoo == "GBp" {
		return 100.0, nil
	}
	if r == "GBp" && yahoo == "GBP" {
		return 0.01, nil
	}
	return 0.0, fmt.Errorf("invalid currecny pair yahoo = %s, record = %s", yahoo, r)
}
