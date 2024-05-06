package record

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"aagr.xyz/trades/src/db"
)

const timeFmt = "2006-01-02 15:04:05"

var (
	// GlobalBroker are transactions that are broker independent
	GlobalBroker = Account{Name: "*", CGTExempt: false}
)

// TransactionType is the enum storing the type of each transaction
type TransactionType int

const (
	// Unknown transaction type
	Unknown TransactionType = iota
	// Buy of a ticker
	Buy
	// Sell of a ticker
	Sell
	// Split - stock splits
	Split
	// Rename of a ticker
	Rename
	// Transfer and Cash transactions are for house-keeping purposes and do not contribute to
	// CGT calculations.
	// Transfer from one account to another
	TransferOut
	TransferIn
	// CashIn - Pay in cash in an account
	CashIn
	// CashOut - withdraw cash
	CashOut
	// Dividend
	Dividend
)

// TransactionOrder - On a single day, this is the order the records need to be sorted by
var TransactionOrder = map[TransactionType]int{
	Rename:      0,
	Split:       1,
	TransferOut: 2,
	TransferIn:  3,
	CashIn:      4,
	Dividend:    5,
	Sell:        6,
	Buy:         7,
	CashOut:     8,
}

func (t TransactionType) String() string {
	switch t {
	case Buy:
		return "BUY"
	case Sell:
		return "SELL"
	case Split:
		return "SPLIT"
	case Rename:
		return "RENAME"
	case TransferIn:
		return "TRANSFERIN"
	case TransferOut:
		return "TRANSFEROut"
	case CashIn:
		return "CASHIN"
	case CashOut:
		return "CASHOUT"
	case Dividend:
		return "DIVIDEND"
	}
	return ""
}

// NewTransactionType returns a new transaction type enum
func NewTransactionType(s string) TransactionType {
	switch strings.ToUpper(s) {
	case "BUY":
		return Buy
	case "SELL":
		return Sell
	case "SPLIT":
		return Split
	case "RENAME":
		return Rename
	case "TRANSFERIN":
		return TransferIn
	case "TRANSFEROUT":
		return TransferOut
	case "CASHIN":
		return CashIn
	case "CASHOUT":
		return CashOut
	case "DIVIDEND":
		return Dividend
	}
	return Unknown
}

func (t TransactionType) IsMetadataEvent() bool {
	return t == Split || t == Rename || t == TransferIn || t == TransferOut
}

func (t TransactionType) IsCashEvent() bool {
	return t == CashIn || t == CashOut
}

func (t TransactionType) IsUnknown() bool {
	return t == Unknown
}

func (t TransactionType) IsDividend() bool {
	return t == Dividend
}

// InverseAction returns the inverse of buy and sell
func InverseAction(t TransactionType) TransactionType {
	switch t {
	case Buy:
		return Sell
	case Sell:
		return Buy
	}
	return Unknown
}

// Currency is the enum storing the currency
type Currency string

const (
	GBP      Currency = "GBP"
	GBX      Currency = "GBX" // GBX refers to 1 pence. So 100 GBX = 1 GBP
	USD      Currency = "USD"
	INR      Currency = "INR"
	EUR      Currency = "EUR"
	CHF      Currency = "CHF"
	MULTIPLE Currency = "*"
)

// NewCurrency returns a new currency type enum
func NewCurrency(s string) Currency {
	switch strings.ToUpper(s) {
	case "GBP":
		return GBP
	case "GBX":
		return GBX
	case "USD":
		return USD
	case "INR":
		return INR
	case "EUR":
		return EUR
	case "CHF":
		return CHF
	case "*":
		return MULTIPLE
	}
	return ""
}

// Account stores information about the account aka broker where something happened
type Account struct {
	// If Name is set to "*", it implies a global event like a stock split or rename of ticker.
	Name string
	// Currency of the account
	Currency Currency
	// If CGTExempt is true, then transactions in this account are exempt from CGT calculation,
	// but you can still see the profit/loss for it.
	CGTExempt bool
}

// Record stores each transaction
type Record struct {
	Timestamp     time.Time       `csv:"Timestamp"`
	Broker        Account         `csv:"Broker"`
	Action        TransactionType `csv:"Action"`
	Ticker        string          `csv:"Ticker"`
	Name          string          `csv:"Name"`
	ShareCount    float64         `csv:"Quantity"`
	PricePerShare float64         `csv:"Price"`
	Currency      Currency        `csv:"Currency"`
	ExchangeRate  float64         `csv:"ExchangeRate"` // this is multiplied to get the total in gbp
	Commission    float64         `csv:"Commission"`   // Commission is always in GBP
	Total         float64         `csv:"Total"`        // Total is always in GBP
	Description   string          `csv:"Description"`  // used for rename and split types
}

func (r *Record) String() string {
	var buf bytes.Buffer
	io.WriteString(&buf, fmt.Sprintf("[%s,%s] %s ", r.Timestamp.Format(timeFmt), r.Broker.Name, r.Action))
	io.WriteString(&buf, fmt.Sprintf("%f %s (%s) @ %f %s ", r.ShareCount, r.Name, r.Ticker, r.PricePerShare, r.Currency))
	if r.Currency != GBP {
		io.WriteString(&buf, fmt.Sprintf(" Converted @ 1%s = %fGBP ", r.Currency, r.ExchangeRate))
	}
	io.WriteString(&buf, fmt.Sprintf(" ; commission = %f GBP ; total = %f", r.Commission, r.Total))
	return buf.String()
}

// Header returns the header for a CSV of Records
func (r *Record) Header() []string {
	return []string{
		"Timestamp",
		"Account.Name",
		"Account.Currency",
		"Account.CGTExempt",
		"Action",
		"Ticker",
		"Name",
		"Quantity",
		"Price",
		"Currency",
		"ExchangeRate",
		"Commission",
		"Total",
		"Description",
	}
}

// MarshalCSV converts a record to a slice of string, which can be marshalled to CSV
func (r *Record) MarshalCSV() []string {
	return []string{
		r.Timestamp.Format(timeFmt),
		string(r.Broker.Name),
		string(r.Broker.Currency),
		fmt.Sprintf("%t", r.Broker.CGTExempt),
		r.Action.String(),
		r.Ticker,
		r.Name,
		fmt.Sprintf("%f", r.ShareCount),
		fmt.Sprintf("%f", r.PricePerShare),
		string(r.Currency),
		fmt.Sprintf("%f", r.ExchangeRate),
		fmt.Sprintf("%f", r.Commission),
		fmt.Sprintf("%f", r.Total),
		r.Description,
	}
}

func (r *Record) ValidateAndEnrich() error {
	// Let's store everything in GBP
	if r.Currency == GBX {
		r.PricePerShare /= 100.0
		r.Currency = GBP
		r.ExchangeRate = 1.0
	}
	currency := r.Currency
	if err := r.assertMaths(); err != nil {
		return fmt.Errorf("cannot assert the maths for the record (record = %s): %v", r.String(), err)
	}
	if err := r.fillTickerOrName(); err != nil {
		return fmt.Errorf("cannot fill ticker or name (record= %s): %v", r.String(), err)
	}
	// If it is not a buy or sell transaction, then don't fiddle around with currency
	if !(r.Action == Sell || r.Action == Buy) {
		return nil
	}
	if err := db.SetCurrency(r.Ticker, string(currency)); err != nil {
		return fmt.Errorf("cannot set currency of the ticker (record = %s): %v", r.String(), err)
	}
	return nil
}

func (r *Record) assertMaths() error {
	want := (r.ShareCount * r.PricePerShare * r.ExchangeRate)
	switch r.Action {
	case Buy:
		want += r.Commission
	case Sell:
		want -= r.Commission
	}
	if math.Abs(want-r.Total) > 0.1 {
		return fmt.Errorf("record's total price differs %s, want: %f got: %f", r, want, r.Total)
	}
	return nil
}

func (r *Record) fillTickerOrName() error {
	if r.Ticker == "" && r.Name == "" {
		return fmt.Errorf("both name and ticker are empty")
	}
	var err error
	if r.Ticker != "" && r.Name == "" {
		err = r.fillName()
	}
	if r.Name != "" && r.Ticker == "" {
		err = r.fillTicker()
	}
	if err != nil {
		return err
	}
	db.AddTickerName(r.Ticker, r.Name)
	return nil
}

func (r *Record) fillName() error {
	name, err := db.TickerName(r.Ticker)
	if err != nil {
		return err
	}
	r.Name = name
	return nil
}

func (r *Record) fillTicker() error {
	ticker, err := db.GuessTickerFromName(r.Name)
	if err != nil {
		return err
	}
	r.Ticker = ticker
	return nil
}
