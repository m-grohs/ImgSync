package main

import (
	"os"

	"imgsync/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
