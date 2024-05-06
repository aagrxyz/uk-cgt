package holdings

import (
	"fmt"
	"strings"
	"time"

	"aagr.xyz/trades/src/record"
)

// Account is information about that account
type Account struct {
	record.Account
	positions map[string]*position
}

func (a *Account) Render() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Account = %v\n\n", a.Account))
	for t, p := range a.positions {
		if p.quantity <= 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\tTicker = %s, Quantity = %f, AverageCost = %f\n", t, p.quantity, p.averageCost()))
	}
	sb.WriteString("\n\n")
	return sb.String()
}

type AccountStats struct {
	accounts []*Account
}

func (a *AccountStats) Render() string {
	var sb strings.Builder
	sb.WriteString("\n\nAccount Holdings info\n\n")
	for _, act := range a.accounts {
		sb.WriteString(act.Render())
	}
	return sb.String()
}

func GetAccountStats(records []*record.Record) (*AccountStats, error) {
	var byAccount = make(map[record.Account][]*record.Record)
	var globals []*record.Record
	for _, r := range records {
		rCopy := *r
		if r.Broker == record.GlobalBroker {
			globals = append(globals, &rCopy)
			continue
		}
		byAccount[rCopy.Broker] = append(byAccount[rCopy.Broker], &rCopy)
	}
	for k := range byAccount {
		for _, g := range globals {
			gCopy := *g
			byAccount[k] = append(byAccount[k], &gCopy)
		}
	}
	var res []*Account
	for k, vals := range byAccount {
		a, err := accountInternal(k, vals)
		if err != nil {
			return nil, fmt.Errorf("cannot get account stats for %v: %v", k, err)
		}
		res = append(res, a)
	}
	return &AccountStats{accounts: res}, nil
}

// this runs on only one account
func accountInternal(act record.Account, records []*record.Record) (*Account, error) {
	if len(records) == 0 {
		return nil, nil
	}
	a := &Account{
		Account:   act,
		positions: make(map[string]*position),
	}
	sortRecords(records, time.Second)
	oldDate := records[0].Timestamp.Truncate(24 * time.Hour)
	for _, r := range records {
		// this is a new date transaction, so check if nothing is -ve
		if !r.Timestamp.Truncate(24 * time.Hour).Equal(oldDate) {
			for k, p := range a.positions {
				if p.quantity < 0.0 {
					return nil, fmt.Errorf("position %s became -ve on previous day: %f", k, p.quantity)
				}
			}
		}
		if _, ok := a.positions[r.Ticker]; !ok {
			a.positions[r.Ticker] = &position{}
		}
		p := a.positions[r.Ticker]
		switch r.Action {
		case record.CashIn:
			if act.Currency == record.GBP && r.Currency != record.GBP {
				return nil, fmt.Errorf("cannot deposit %s to account %v", r.Currency, act)
			}
			p.buy(r.ShareCount, r.ShareCount)
		case record.Dividend:
			if act.Currency == record.GBP && r.Currency != record.GBP {
				return nil, fmt.Errorf("cannot deposit dividend %s to account %v", r.Currency, act)
			}
			a.positions[string(r.Currency)].buy(r.ShareCount, r.ShareCount)
		case record.CashOut:
			if act.Currency == record.GBP && r.Currency != record.GBP {
				return nil, fmt.Errorf("cannot deposit %s to account %v", r.Currency, act)
			}
			if p.quantity < r.ShareCount {
				return nil, fmt.Errorf("trying to cashout %v, insufficient available quantity %f", r, p.quantity)
			}
			p.sell(r.ShareCount)
		case record.TransferOut:
			if p.quantity < r.ShareCount {
				return nil, fmt.Errorf("trying to cashout %v, insufficient available quantity %f", r, p.quantity)
			}
			p.sell(r.ShareCount)
		case record.TransferIn:
			p.buy(r.ShareCount, r.Total/r.ExchangeRate)
		case record.Buy:
			p.buy(r.ShareCount, r.Total/r.ExchangeRate)
			if err := sellOtherSide(r, a); err != nil {
				return nil, fmt.Errorf("cannot sell otherside of buy: %v", err)
			}
		case record.Sell:
			p.sell(r.ShareCount)
			buyOtherSide(r, a)
		case record.Split:
			var newCt, oldCt int64
			_, err := fmt.Sscanf(r.Description, "%d FOR %d", &newCt, &oldCt)
			if err != nil {
				return nil, fmt.Errorf("error in parsing transaction %s", r.String())
			}
			p.split(newCt, oldCt)
		}
		oldDate = r.Timestamp.Truncate(24 * time.Hour)
	}
	// check again
	for k, p := range a.positions {
		if p.quantity < 0.0 {
			return nil, fmt.Errorf("position %s became -ve on previous day: %f", k, p.quantity)
		}
	}
	return a, nil
}

func sellOtherSide(r *record.Record, act *Account) error {
	// this is a normal buy of an asset in a multi-currency account, which will have an explicit sell
	// so skip
	if act.Currency == record.MULTIPLE && r.Description == "" && r.Currency != record.GBP {
		return nil
	}
	want := r.Total / r.ExchangeRate
	curr := string(r.Currency)
	// if it is a a GBP account, or transaction is in GBP or currency conversion, make sure we have that much GBP available
	if act.Currency == record.GBP || r.Description == "SELL GBP" {
		want = r.Total
		curr = string(record.GBP)
	}
	available := act.positions[curr]
	// you need this much funds to buy this ticker
	if available == nil || available.quantity < want {
		return fmt.Errorf("trying to buy %v, don't have enough funds %v", r, available)
	}
	available.sell(want)
	return nil
}

func buyOtherSide(r *record.Record, act *Account) {
	// this is a normal sell of an asset in a multi-currency account, which will have an explicit buy
	// so skip
	if act.Currency == record.MULTIPLE && r.Description == "" && r.Currency != record.GBP {
		return
	}
	got := r.Total / r.ExchangeRate
	curr := string(r.Currency)
	if act.Currency == record.GBP || r.Description == "BUY GBP" {
		got = r.Total
		curr = string(record.GBP)
	}
	if _, ok := act.positions[curr]; !ok {
		act.positions[curr] = &position{}
	}
	act.positions[curr].buy(got, got)
}
