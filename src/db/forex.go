package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"time"

	log "github.com/sirupsen/logrus"
)

const forexJSONFilename = "outputs/fx_db.json"

var forex map[time.Time]map[string]float64

func initForex(rootDir string) {
	forex = make(map[time.Time]map[string]float64)
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
func AddForex(ts time.Time, currency string, value float64) {
	date := ts.Truncate(24 * time.Hour)
	if _, ok := forex[date]; !ok {
		forex[date] = make(map[string]float64)
	}
	if _, ok := forex[date][string(currency)]; ok {
		return
	}
	forex[date][string(currency)] = value
}

// GetForex returns the conversion rate to GBP.
func GetForex(ts time.Time, currency string) float64 {
	date := ts.Truncate(24 * time.Hour)
	if _, ok := forex[date]; !ok {
		forex[date] = make(map[string]float64)
	}
	if val, ok := forex[date][string(currency)]; ok {
		return val
	}
	var inp float64
	fmt.Printf("Exchange rate not known for date %v, currency 1 %v to GBP, please enter:\n", date.Format("2006-01-02"), currency)
	fmt.Scanf("%f", &inp)
	forex[date][string(currency)] = inp
	return inp
}
