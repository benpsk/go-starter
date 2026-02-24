package server

import (
	"strconv"
	"strings"
)

func formatUserID(userID int64) string {
	return strconv.FormatInt(userID, 10)
}

func parseUserID(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}
