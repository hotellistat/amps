package job

import (
	"testing"

	"github.com/hotellistat/amps/cmd/amps/job"
)

type FakeDelivery struct {
	test string
}

func (n *FakeDelivery) Ack(multiple bool) error {
	println("ACK")
	return nil
}

func (n *FakeDelivery) Nack(multiple, requeue bool) error {
	println("NACK")
	return nil
}

func TestSize(t *testing.T) {

	fakeDelivery := &FakeDelivery{
		test: "test",
	}

	var tmpSize int

	manifest := job.NewManifest(10)

	tmpSize = manifest.Size()

	if tmpSize != 0 {
		t.Error()
	}

	manifest.InsertJobWithDelivery("1111", fakeDelivery)

	tmpSize = manifest.Size()

	if tmpSize != 1 {
		t.Error()
	}

	manifest.InsertJobWithDelivery("2222", fakeDelivery)
	manifest.InsertJobWithDelivery("3333", fakeDelivery)

	tmpSize = manifest.Size()

	if tmpSize != 3 {
		t.Error()
	}

	manifest.InsertJobWithDelivery("4444", fakeDelivery)
	manifest.InsertJobWithDelivery("5555", fakeDelivery)
	manifest.InsertJobWithDelivery("6666", fakeDelivery)
	manifest.InsertJobWithDelivery("7777", fakeDelivery)

	tmpSize = manifest.Size()

	println(tmpSize)
	if tmpSize != 7 {
		t.Error()
	}

	manifest.DeleteJob("1111")

	tmpSize = manifest.Size()

	if tmpSize != 6 {
		t.Error()
	}

}

func TestHasJob(t *testing.T) {
	fakeDelivery := &FakeDelivery{
		test: "test",
	}

	manifest := job.NewManifest(10)

	isFalse := manifest.HasJob("1111")

	if isFalse != false {
		t.Error()
	}

	manifest.InsertJobWithDelivery("1111", fakeDelivery)

	isTrue := manifest.HasJob("1111")

	if isTrue != true {
		t.Error()
	}
}

func TestInsertJob(t *testing.T) {
	fakeDelivery := &FakeDelivery{
		test: "test",
	}

	manifest := job.NewManifest(10)

	isFalse := manifest.HasJob("aaaa")

	if isFalse != false {
		t.Error()
	}

	manifest.InsertJobWithDelivery("aaaa", fakeDelivery)

	isTrue := manifest.HasJob("aaaa")

	if isTrue != true {
		t.Error("Manifest does not have job")
	}

	// err := manifest.InsertJob("aaaa", fakeMessage)

	// if err == nil {
	// 	t.Error("InsertJob should throw error on duplicate job")
	// }
}

func TestDeleteJob(t *testing.T) {
	fakeDelivery := &FakeDelivery{
		test: "test",
	}

	manifest := job.NewManifest(10)

	manifest.InsertJobWithDelivery("bbbb", fakeDelivery)

	isTrue := manifest.HasJob("bbbb")

	if isTrue != true {
		t.Error()
	}

	manifest.DeleteJob("bbbb")

	isFalse := manifest.HasJob("bbbb")

	if isFalse != false {
		t.Error()
	}

	// err := manifest.DeleteJob("bbbb")

	// if err == nil {
	// 	t.Error("DeleteJob should throw error on not existing job")
	// }

}

// func TestDeleteDeceased(t *testing.T) {
// 	fakeMessage := &FakeMessage{
// 		test: "test",
// 	}

// 	manifest := job.NewManifest(10)

// 	manifest.InsertJob("aaaa", fakeMessage)

// 	time.Sleep(2 * time.Second)

// 	manifest.InsertJob("bbbb", fakeMessage)

// 	maxLifetime, _ := time.ParseDuration("1s")

// 	manifest.DeleteDeceased(maxLifetime)

// 	if manifest.Size() != 1 || manifest.HasJob("aaaa") {
// 		t.Error()
// 	}
// }
