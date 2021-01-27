package main

import (
	"batchable/cmd/batchable/app"

	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	app.Run()
}
