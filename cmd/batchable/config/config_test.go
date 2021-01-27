package config

import (
	"os"
	"testing"
	"time"
)

func TestGetEnv(t *testing.T) {
	os.Setenv("GO_TEST_GETENV", "getEnv")
	value := GetEnv("GO_TEST_GETENV", "default")

	if value != "getEnv" {
		t.Error("Could not get environment variable")
	}

	value2 := GetEnv("GO_TEST_GETENV_NA", "default")

	if value2 != "default" {
		t.Error("Could not get default environment variable")
	}
}

func TestGetEnvRequired(t *testing.T) {
	os.Environ()
	os.Setenv("GO_TEST_GETENVREQUIRED", "getEnv")
	exitCalled := false
	value := GetEnvRequired("GO_TEST_GETENVREQUIRED", func(code int) { exitCalled = true })

	if value != "getEnv" || exitCalled != false {
		t.Error("Could not get environment variable")
	}

	exitCalled = false

	GetEnvRequired("GO_TEST_GETENVREQUIRED_NA", func(code int) {
		exitCalled = true
	})

	if exitCalled != true {
		t.Error("Exit was not called")
	}
}

func TestGetEnvAsInt(t *testing.T) {
	os.Setenv("GO_TEST_GETENV", "5")
	value := GetEnvAsInt("GO_TEST_GETENV", 1)

	if value != 5 {
		t.Error("Could not get environment int variable")
	}

	value2 := GetEnvAsInt("GO_TEST_GETENV_NA", 3)

	if value2 != 3 {
		t.Error("Could not get default int environment variable")
	}
}

func TestGetEnvAsDuration(t *testing.T) {
	os.Setenv("GO_TEST_GETENV", "4m")
	value := GetEnvAsDuration("GO_TEST_GETENV", "1m")

	testDur, _ := time.ParseDuration("4m")

	if value != testDur {
		t.Error("Could not get environment time variable")
	}

	value2 := GetEnvAsDuration("GO_TEST_GETENV_NA", "3m")

	testDefaultDur, _ := time.ParseDuration("3m")

	if value2 != testDefaultDur {
		t.Error("Could not get default time environment variable")
	}

	os.Setenv("GO_TEST_GETENV_FALLBACK", "4horses")

	value3 := GetEnvAsDuration("GO_TEST_GETENV_FALLBACK", "8m")

	testDefaultFallbackDur, _ := time.ParseDuration("8m")

	if value3 != testDefaultFallbackDur {
		t.Error("Could not get default time environment variable")
	}
}

func TestGetEnvAsBool(t *testing.T) {
	os.Setenv("GO_TEST_GETENV", "false")

	value := GetEnvAsBool("GO_TEST_GETENV", true)

	if value != false {
		t.Error("Could not get environment bool variable")
	}

	value2 := GetEnvAsBool("GO_TEST_GETENV_NA", true)

	if value2 != true {
		t.Error("Could not get default environment variable")
	}
}

func TestGetEnvAsSlice(t *testing.T) {
	// os.Setenv("GO_TEST_GETENV", "a,b,c")

	// asd := []string{"d", "e", "f"}
	// value := GetEnvAsSlice("GO_TEST_GETENV", asd, ",")

	// if value != []string{"a", "b", "c"} {
	// 	t.Error("Could not get environment bool variable")
	// }

	// value2 := GetEnvAsSlice("GO_TEST_GETENV_NA", true)

	// if value2 != true {
	// 	t.Error("Could not get default environment variable")
	// }
}
