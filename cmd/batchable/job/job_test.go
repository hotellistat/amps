package job

import (
	"log"
	"testing"
	"time"
)

type FakeMessage struct {
	test string
}

func (wrapper FakeMessage) GetData() []byte {
	return []byte(wrapper.test)
}

func (n *FakeMessage) Ack() error {
	log.Println("ACK")
	return nil
}

func (n *FakeMessage) Reject() error {
	log.Println("REJECT")
	return nil
}

func TestSize(t *testing.T) {

	fakeMessage := &FakeMessage{
		test: "test",
	}

	var tmpSize int

	manifest := NewManifest(10)

	tmpSize = manifest.Size()

	if tmpSize != 0 {
		t.Error()
	}

	manifest.InsertJob("1111", fakeMessage)

	tmpSize = manifest.Size()

	if tmpSize != 1 {
		t.Error()
	}

	manifest.InsertJob("2222", fakeMessage)
	manifest.InsertJob("3333", fakeMessage)

	tmpSize = manifest.Size()

	if tmpSize != 3 {
		t.Error()
	}

	manifest.InsertJob("4444", fakeMessage)
	manifest.InsertJob("5555", fakeMessage)
	manifest.InsertJob("6666", fakeMessage)
	manifest.InsertJob("7777", fakeMessage)

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
	fakeMessage := &FakeMessage{
		test: "test",
	}

	manifest := NewManifest(10)

	isFalse := manifest.HasJob("1111")

	if isFalse != false {
		t.Error()
	}

	manifest.InsertJob("1111", fakeMessage)

	isTrue := manifest.HasJob("1111")

	if isTrue != true {
		t.Error()
	}
}

func TestInsertJob(t *testing.T) {
	fakeMessage := &FakeMessage{
		test: "test",
	}

	manifest := NewManifest(10)

	isFalse := manifest.HasJob("aaaa")

	if isFalse != false {
		t.Error()
	}

	manifest.InsertJob("aaaa", fakeMessage)

	isTrue := manifest.HasJob("aaaa")

	if isTrue != true {
		t.Error("Manifest does not have job")
	}

	err := manifest.InsertJob("aaaa", fakeMessage)

	if err == nil {
		t.Error("InsertJob should throw error on duplicate job")
	}
}

func TestDeleteJob(t *testing.T) {
	fakeMessage := &FakeMessage{
		test: "test",
	}

	manifest := NewManifest(10)

	manifest.InsertJob("bbbb", fakeMessage)

	isTrue := manifest.HasJob("bbbb")

	if isTrue != true {
		t.Error()
	}

	manifest.DeleteJob("bbbb")

	isFalse := manifest.HasJob("bbbb")

	if isFalse != false {
		t.Error()
	}

	err := manifest.DeleteJob("bbbb")

	if err == nil {
		t.Error("DeleteJob should throw error on not existing job")
	}

}

func TestDeleteDeceased(t *testing.T) {
	fakeMessage := &FakeMessage{
		test: "test",
	}

	manifest := NewManifest(10)

	manifest.InsertJob("aaaa", fakeMessage)

	time.Sleep(2 * time.Second)

	manifest.InsertJob("bbbb", fakeMessage)

	maxLifetime, _ := time.ParseDuration("1s")

	manifest.DeleteDeceased(maxLifetime)

	if manifest.Size() != 1 || manifest.HasJob("aaaa") {
		t.Error()
	}
}
