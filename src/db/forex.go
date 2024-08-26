package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"time"

	"aagr.xyz/trades/config"
	"aagr.xyz/trades/record"

	log "github.com/sirupsen/logrus"
)

const forexJSONFilename = "outputs/fx_db.json"

var forex map[time.Time]map[record.Currency]float64

func initForex(rootDir string) {
	forex = make(map[time.Time]map[record.Currency]float64)
	data, err := os.ReadFile(path.Join(rootDir, forexJSONFilename))
	if err != nil {
		log.Errorf("Cannot read file for forex: %v", err)
		return
	}
	err = json.Unmarshal(data, &forex)
	if err != nil {
		log.Errorf("Cannot unmarshal to struct: %v", err)
		return
	}
}

func serializeForex(rootDir string) error {
	data, err := json.Marshal(forex)
	if err != nil {
		return fmt.Errorf("cannot marshal json: %v", err)
	}
	err = os.WriteFile(path.Join(rootDir, forexJSONFilename), data, 0666)
	if err != nil {
		return fmt.Errorf("cannot write json file to disk: %v", err)
	}
	return nil
}

// AddForex adds a mapping for a given date to GBP
// if currency is USD, then it stores X where, 1 USD = X GBP
func AddForex(ts time.Time, currency record.Currency, value float64) {
	date := ts.Truncate(24 * time.Hour)
	if _, ok := forex[date]; !ok {
		forex[date] = make(map[record.Currency]float64)
	}
	if _, ok := forex[date][currency]; ok {
		return
	}
	forex[date][currency] = value
}

// GetForex returns the conversion rate to GBP.
func GetForex(ts time.Time, currency record.Currency) (float64, error) {
	if currency == "GBP" {
		return 1.0, nil
	} else if currency == "GBX" {
		return 0.01, nil
	}
	date := ts.Truncate(24 * time.Hour)
	if _, ok := forex[date]; !ok {
		forex[date] = make(map[record.Currency]float64)
	}
	if val, ok := forex[date][currency]; ok {
		return val, nil
	}
	var inp float64
	s := fmt.Sprintf("Exchange rate not known for date %v, currency 1 %v to GBP, please enter:", date.Format("2006-01-02"), currency)
	if config.Mode() == config.SERVER_MODE {
		return 0.0, fmt.Errorf("%s", s)
	}
	fmt.Printf("%s\n", s)
	fmt.Scanf("%f", &inp)
	forex[date][currency] = inp
	return inp, nil
}
