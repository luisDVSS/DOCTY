package main

import (
	"os"

	"github.com/luisdvss/docty/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
