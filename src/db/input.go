package db

import (
	"bufio"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

func getInput(info string) (string, error) {
	log.Warn(info)
	in := bufio.NewReader(os.Stdin)
	line, err := in.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
