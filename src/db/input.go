package db

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"aagr.xyz/trades/config"
	log "github.com/sirupsen/logrus"
)

func getInput(info string) (string, error) {
	if config.Mode() == config.SERVER_MODE {
		return "", fmt.Errorf(info)
	}
	log.Warn(info)
	in := bufio.NewReader(os.Stdin)
	line, err := in.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
