package main

import (
	"math/rand"
	"os"
	"time"

	"github.com/soda-cdm/kahu/provider/meta_service/server"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	command := server.NewMetaServiceCommand()
	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
