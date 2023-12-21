package holdings

import (
	"fmt"
	"time"
)

var taxYears map[string]*TaxYear

func init() {
	taxYears = make(map[string]*TaxYear)
}

// TaxYear stores info about a UK tax year
type TaxYear struct {
	name       string
	start, end time.Time
}

func newTaxYear(yearStart, yearEnd int) *TaxYear {
	return &TaxYear{
		name:  fmt.Sprintf("%d-%d", yearStart, yearEnd%100),
		start: time.Date(yearStart, time.April, 06, 0, 0, 0, 0, time.UTC),
		end:   time.Date(yearEnd, time.April, 05, 0, 0, 0, 0, time.UTC),
	}
}

func (ty *TaxYear) contains(ts time.Time) bool {
	day := ts.Truncate(24 * time.Hour)
	// tax year is inclusive
	if day.Equal(ty.start) || day.Equal(ty.end) {
		return true
	}
	if day.After(ty.start) && day.Before(ty.end) {
		return true
	}
	return false
}

func getTaxYear(ts time.Time) string {
	// Check if it exists in the map
	for year, ty := range taxYears {
		if ty.contains(ts) {
			return year
		}
	}
	// If not, then generate one.
	// A given date can only be in [year-1,year] or [year,year+1]
	year := ts.Year()
	for end := year + 1; end >= year; end-- {
		x := newTaxYear(end-1, end)
		if x.contains(ts) {
			taxYears[x.name] = x
			return x.name
		}
	}
	return ""
}
