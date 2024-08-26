package yahoo

import (
	"fmt"

	"github.com/go-resty/resty/v2"
)

//nolint:gochecknoglobals
var (
	postMarketStatuses = map[string]bool{"POST": true, "POSTPOST": true}
)

// ResponseQuote represents a quote of a single security from the API response
type ResponseQuote struct {
	ShortName                  string              `json:"shortName"`
	Symbol                     string              `json:"symbol"`
	MarketState                string              `json:"marketState"`
	Currency                   string              `json:"currency"`
	ExchangeName               string              `json:"fullExchangeName"`
	ExchangeDelay              float64             `json:"exchangeDataDelayedBy"`
	RegularMarketChange        ResponseFieldFloat  `json:"regularMarketChange"`
	RegularMarketChangePercent ResponseFieldFloat  `json:"regularMarketChangePercent"`
	RegularMarketPrice         ResponseFieldFloat  `json:"regularMarketPrice"`
	RegularMarketPreviousClose ResponseFieldFloat  `json:"regularMarketPreviousClose"`
	RegularMarketOpen          ResponseFieldFloat  `json:"regularMarketOpen"`
	RegularMarketDayRange      ResponseFieldString `json:"regularMarketDayRange"`
	RegularMarketDayHigh       ResponseFieldFloat  `json:"regularMarketDayHigh"`
	RegularMarketDayLow        ResponseFieldFloat  `json:"regularMarketDayLow"`
	RegularMarketVolume        ResponseFieldFloat  `json:"regularMarketVolume"`
	PostMarketChange           ResponseFieldFloat  `json:"postMarketChange"`
	PostMarketChangePercent    ResponseFieldFloat  `json:"postMarketChangePercent"`
	PostMarketPrice            ResponseFieldFloat  `json:"postMarketPrice"`
	PreMarketChange            ResponseFieldFloat  `json:"preMarketChange"`
	PreMarketChangePercent     ResponseFieldFloat  `json:"preMarketChangePercent"`
	PreMarketPrice             ResponseFieldFloat  `json:"preMarketPrice"`
	FiftyTwoWeekHigh           ResponseFieldFloat  `json:"fiftyTwoWeekHigh"`
	FiftyTwoWeekLow            ResponseFieldFloat  `json:"fiftyTwoWeekLow"`
	QuoteType                  string              `json:"quoteType"`
	MarketCap                  ResponseFieldFloat  `json:"marketCap"`
}

func (r *ResponseQuote) price() float64 {
	if r == nil {
		return 0.0
	}
	return r.RegularMarketPrice.Raw
}

func (r *ResponseQuote) todayPercentChange() float64 {
	if r == nil {
		return 0.0
	}
	return r.RegularMarketChangePercent.Raw
}

type ResponseFieldFloat struct {
	Raw float64 `json:"raw"`
	Fmt string  `json:"fmt"`
}

type ResponseFieldString struct {
	Raw string `json:"raw"`
	Fmt string `json:"fmt"`
}

// Response represents the container object from the API response
type Response struct {
	QuoteResponse struct {
		Quotes []ResponseQuote `json:"result"`
		Error  interface{}     `json:"error"`
	} `json:"quoteResponse"`
}

type SearchQuote struct {
	ShortName string `json:"shortname"`
	Symbol    string `json:"symbol"`
}

type SearchResponse struct {
	Quotes []SearchQuote `json:"quotes"`
	Error  interface{}   `json:"error"`
}

// getQuote issues a HTTP request to retrieve quote from the API and process the response
func getQuote(client *resty.Client, symbol string) (*ResponseQuote, error) {
	res, _ := client.R().
		SetResult(Response{}).
		SetQueryParam("fields", "shortName,regularMarketChange,regularMarketChangePercent,regularMarketPrice,regularMarketPreviousClose,regularMarketOpen,regularMarketDayRange,regularMarketDayHigh,regularMarketDayLow,regularMarketVolume,postMarketChange,postMarketChangePercent,postMarketPrice,preMarketChange,preMarketChangePercent,preMarketPrice,fiftyTwoWeekHigh,fiftyTwoWeekLow,marketCap").
		SetQueryParam("symbols", symbol).
		Get("/v7/finance/quote")
	return firstQuote(res)
}

func firstQuote(res *resty.Response) (*ResponseQuote, error) {
	resp := res.Result().(*Response)
	if resp.QuoteResponse.Error != nil {
		err := resp.QuoteResponse.Error.(error)
		return nil, fmt.Errorf("error in getting response from yahoo: %v", err)
	}
	if len(resp.QuoteResponse.Quotes) == 0 {
		return nil, fmt.Errorf("no response from yahoo finance")
	}
	return &resp.QuoteResponse.Quotes[0], nil
}
func search(client *resty.Client, query string) (*SearchQuote, error) {
	res, _ := client.R().
		SetResult(SearchResponse{}).
		SetQueryParam("q", query).
		SetQueryParam("lang", "en-US").
		SetQueryParam("quotesCount", "1").
		Get("/v1/finance/search")

	resp := res.Result().(*SearchResponse)
	if resp.Error != nil {
		err := resp.Error.(error)
		return nil, fmt.Errorf("error in getting response from yahoo: %v", err)
	}
	if len(resp.Quotes) == 0 {
		return nil, fmt.Errorf("no response from yahoo finance")
	}
	return &resp.Quotes[0], nil
}
