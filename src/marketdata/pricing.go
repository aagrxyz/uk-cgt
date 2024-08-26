package marketdata

import (
	"fmt"
	"time"

	"aagr.xyz/trades/record"
	"github.com/ReneKroon/ttlcache"
	log "github.com/sirupsen/logrus"
)

var (
	defaultTTL = 10 * time.Minute
	openHr     = 6
	closeHr    = 22
)

type Backend interface {
	GuessTicker(symbol string, currency record.Currency) (string, error)
	QueryMetadata(ticker string) (*SourceMetadata, error)
	GetQuote(ticker string, currency record.Currency) (*Quote, error)
	GetForex(currency record.Currency) (float64, error)
	// Search(ticker string)
}

type Service struct {
	cache      *ttlcache.Cache
	forexCache *ttlcache.Cache
	backends   map[Source]Backend
}

func NewService(backends map[Source]Backend) *Service {
	cache := ttlcache.NewCache()
	cache.SkipTtlExtensionOnHit(true)
	forexCache := ttlcache.NewCache()
	forexCache.SetTTL(defaultTTL)
	forexCache.SkipTtlExtensionOnHit(true)
	return &Service{
		cache:      cache,
		forexCache: forexCache,
		backends:   backends,
	}
}

type Source string

const (
	YAHOO          Source = "YAHOO"
	GOOGLE_FINANCE Source = "GOOGLE"
)

type SourceMetadata struct {
	Ticker       string          `json:"ticker"`
	Currency     record.Currency `json:"currency"`
	ExchangeName string          `json:"exchange"`
}

func (s *SourceMetadata) Merge(other *SourceMetadata) (*SourceMetadata, error) {
	if other.Ticker == "" {
		return s, nil
	}
	if other.Ticker != s.Ticker {
		return nil, fmt.Errorf("cannot merge with different tickers (%s and %s)", s.Ticker, other.Ticker)
	}
	if other.Currency != "" {
		s.Currency = other.Currency
	}
	if other.ExchangeName != "" {
		s.ExchangeName = other.ExchangeName
	}
	return s, nil
}

type Quote struct {
	RegularMarketPrice float64
	TodayPercentChange float64
}

func nextWeeklyEvent(t time.Time, weekday time.Weekday, hour, minute int) time.Time {
	days := int((7 + (weekday - t.Weekday())) % 7)
	y, m, d := t.AddDate(0, 0, days).Date()
	return time.Date(y, m, d, hour, minute, 0, 0, t.Location())
}

func expirationTTL(t time.Time) time.Duration {
	utc := t.UTC()
	expire := utc.Add(defaultTTL)

	// if in night, expire at the next market open time
	if hr := expire.Hour(); hr < openHr || hr >= closeHr {
		hrs := int64((24 + openHr - hr) % 24)
		expire = expire.Add(time.Duration(hrs) * time.Hour).Truncate(time.Hour)
	}
	// If weekend - return the next weekly monday market opening time
	if wd := expire.Weekday(); wd == time.Saturday || wd == time.Sunday {
		expire = nextWeeklyEvent(utc, time.Monday, openHr, 0)
	}
	return expire.Sub(utc)
}

// GetQuote returns any source which returns a non-nil and non-error result
func (s *Service) GetQuote(ticker string, currency record.Currency, metadata map[Source]*SourceMetadata) (*Quote, error) {
	val, ok := s.cache.Get(ticker)
	if ok {
		return val.(*Quote), nil
	}
	for src, md := range metadata {
		resp, err := s.backends[src].GetQuote(md.Ticker, currency)
		if err != nil {
			log.Errorf("source %s returned error for ticker %s: %v", src, md.Ticker, err)
			continue
		}
		if resp == nil {
			continue
		}
		s.cache.SetWithTTL(ticker, resp, expirationTTL(time.Now().UTC()))
		return resp, nil
	}
	return nil, fmt.Errorf("cannot get quote from any source")
}

func (s *Service) GetForex(currency record.Currency) (float64, error) {
	switch currency {
	case record.GBP:
		return 1.0, nil
	case record.GBX:
		return 0.01, nil
	}
	val, ok := s.forexCache.Get(string(currency))
	if ok {
		return val.(float64), nil
	}
	for src, b := range s.backends {
		forex, err := b.GetForex(currency)
		if err != nil {
			log.Errorf("error in fetching forex from %s: %v", src, err)
			continue
		}
		s.forexCache.Set(string(currency), forex)
		return forex, nil
	}
	return 0.0, fmt.Errorf("cannot get forex %s from any source", currency)

}

// func Search(symbol string) (*Quote, error) {}

func (s *Service) Metadata(symbol string, currency record.Currency, old map[Source]*SourceMetadata) (map[Source]*SourceMetadata, error) {
	var res = make(map[Source]*SourceMetadata)
	for src, b := range s.backends {
		md := old[src]
		if md == nil {
			md = &SourceMetadata{}
		}
		if md.Ticker == "" {
			t, err := b.GuessTicker(symbol, currency)
			if err != nil {
				return nil, fmt.Errorf("cannot get ticker: %v", err)
			}
			md.Ticker = t
		}
		resp, err := b.QueryMetadata(md.Ticker)
		if err != nil {
			log.Errorf("cannot get metadata from source %s, merging old: %v", src, err)
		}
		new, err := md.Merge(resp)
		if err != nil {
			return nil, fmt.Errorf("cannot merge: %v", err)
		}
		res[src] = new
	}
	return res, nil
}
