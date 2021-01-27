package app

import (
	"testing"
	"time"
)

func TestSize(t *testing.T) {

	var tmpSize int

	jobManifest := NewJobManifest()

	tmpSize = jobManifest.Size()

	if tmpSize != 0 {
		t.Error()
	}

	jobManifest.InsertJob("1111")

	tmpSize = jobManifest.Size()

	if tmpSize != 1 {
		t.Error()
	}

	jobManifest.InsertJob("2222")
	jobManifest.InsertJob("3333")

	tmpSize = jobManifest.Size()

	if tmpSize != 3 {
		t.Error()
	}

	jobManifest.InsertJob("4444")
	jobManifest.InsertJob("5555")
	jobManifest.InsertJob("6666")
	jobManifest.InsertJob("7777")

	tmpSize = jobManifest.Size()

	println(tmpSize)
	if tmpSize != 7 {
		t.Error()
	}

	jobManifest.DeleteJob("1111")

	tmpSize = jobManifest.Size()

	if tmpSize != 6 {
		t.Error()
	}

}

func TestHasJob(t *testing.T) {
	jobManifest := NewJobManifest()

	isFalse := jobManifest.HasJob("1111")

	if isFalse != false {
		t.Error()
	}

	jobManifest.InsertJob("1111")

	isTrue := jobManifest.HasJob("1111")

	if isTrue != true {
		t.Error()
	}
}

func TestInsertJob(t *testing.T) {
	jobManifest := NewJobManifest()

	isFalse := jobManifest.HasJob("aaaa")

	if isFalse != false {
		t.Error()
	}

	jobManifest.InsertJob("aaaa")

	isTrue := jobManifest.HasJob("aaaa")

	if isTrue != true {
		t.Error()
	}
}

func TestDeleteJob(t *testing.T) {
	jobManifest := NewJobManifest()

	jobManifest.InsertJob("bbbb")

	isTrue := jobManifest.HasJob("bbbb")

	if isTrue != true {
		t.Error()
	}

	jobManifest.DeleteJob("bbbb")

	isFalse := jobManifest.HasJob("bbbb")

	if isFalse != false {
		t.Error()
	}
}

func TestDeleteDeceased(t *testing.T) {
	jobManifest := NewJobManifest()

	jobManifest.InsertJob("aaaa")

	time.Sleep(2 * time.Second)

	jobManifest.InsertJob("bbbb")

	maxLifetime, _ := time.ParseDuration("1s")

	jobManifest.DeleteDeceased(maxLifetime)

	if jobManifest.Size() != 1 || jobManifest.HasJob("aaaa") {
		t.Error()
	}
}
