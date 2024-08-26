package yahoo

import (
	"fmt"
	"strings"

	"aagr.xyz/trades/marketdata"
	"aagr.xyz/trades/record"
	"github.com/go-resty/resty/v2"
)

type Backend struct {
	client *resty.Client
}

func NewBackend() (*Backend, error) {
	main, session := resty.New(), resty.New()
	main = newClient(main, session)
	if err := refreshSession(main, session); err != nil {
		return nil, fmt.Errorf("cannot create new yahoo client: %v", err)
	}
	return &Backend{client: main}, nil
}

func (b *Backend) GuessTicker(symbol string, currency record.Currency) (string, error) {
	ticker := symbol
	if strings.HasPrefix(string(currency), "GB") {
		ticker += ".L"
	}
	if currency == record.CHF {
		ticker += ".SW"
	}
	_, err := getQuote(b.client, ticker)
	if err != nil {
		return "", fmt.Errorf("cannot guess yahoo finance symbol for %s:%s :%v", symbol, currency, err)
	}
	return ticker, nil
}

func (b *Backend) QueryMetadata(ticker string) (*marketdata.SourceMetadata, error) {
	quote, err := getQuote(b.client, ticker)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch from yahoo finance: %v", err)
	}
	return &marketdata.SourceMetadata{
		Ticker:       ticker,
		ExchangeName: quote.ExchangeName,
		Currency:     record.NewCurrency(quote.Currency),
	}, nil
}

func (b *Backend) GetQuote(ticker string, currency record.Currency) (*marketdata.Quote, error) {
	quote, err := getQuote(b.client, ticker)
	if err != nil {
		return nil, fmt.Errorf("cannot get price from yahoo for ticker %s: %v", ticker, err)
	}
	res := &marketdata.Quote{
		RegularMarketPrice: quote.price(),
		TodayPercentChange: quote.todayPercentChange(),
	}
	// Handle different types of currencies i.e. GBP and GBX bull-shit
	if quote.Currency == "GBp" && currency == record.GBP {
		res.RegularMarketPrice /= 100.0
	} else if quote.Currency == "GBP" && currency == record.GBX {
		res.RegularMarketPrice *= 100.0
	} else if quote.Currency != string(currency) {
		// If it is any other currency typically USD, EUR, INR - this will be correct and match the ticker currency
		return nil, fmt.Errorf("The currencies should match for ticker %s, but did not (got=%s, want=%s)", ticker, quote.Currency, currency)
	}
	return res, nil
}

func (b *Backend) GetForex(currency record.Currency) (float64, error) {
	symbol := fmt.Sprintf("%sGBP=X", currency)
	quote, err := getQuote(b.client, symbol)
	if err != nil {
		return 0.0, fmt.Errorf("cannot get forex for today for symbol %s from yahoo finance: %v", symbol, err)
	}
	return quote.price(), nil
}
