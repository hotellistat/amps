package app

import (
	"testing"

	"github.com/joho/godotenv"
)

func TestMain(t *testing.T) {
	godotenv.Load()
	Run()
}
