package cli

import (
	"os"
	"strings"
)

var cfgFormat = "auto"

type outputFormat int

const (
	formatTTY outputFormat = iota
	formatMarkdown
	formatJSON
)

func outputMode() outputFormat {
	if flagJSON || strings.EqualFold(cfgFormat, "json") {
		return formatJSON
	}
	switch strings.ToLower(strings.TrimSpace(cfgFormat)) {
	case "md", "markdown":
		return formatMarkdown
	case "tty":
		return formatTTY
	}
	if isTerminalWriter(os.Stdout) {
		return formatTTY
	}
	return formatMarkdown
}
