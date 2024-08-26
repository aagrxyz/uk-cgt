// Package db returns an in-memory database to store metadata needed for the program
// Right now it stores the forex exchange rates and the names <-> ticker mapping for symbols.
// It is the caller's responsibility to call the Serialize function to persist changes to disk.
package db

import (
	"fmt"
)

// InitDB is called by caller to initialize a database for symbols and forex.
func InitDB(rootDir string) {
	initSymbols(rootDir)
	initForex(rootDir)
}

// SerializeDB is called by caller to persist changes to disk
func SerializeDB(rootDir string) error {
	if err := serializeSymbols(rootDir); err != nil {
		return fmt.Errorf("cannot serialize symbols: %v", err)
	}
	if err := serializeForex(rootDir); err != nil {
		return fmt.Errorf("cannot serialize forex: %v", err)
	}
	return nil
}
