package job

import (
	"errors"
	"sync"
	"time"
)

type Message interface {
}

// Job represents a job item
type Job struct {
	created time.Time
	message Message
}

// Manifest represents the collection of current jobs
type Manifest struct {
	Mutex *sync.Mutex
	jobs  map[string]Job
}

// NewManifest creates a new Manifest with a predefined maxSize
func NewManifest(size int) Manifest {
	return Manifest{
		&sync.Mutex{},
		make(map[string]Job, size),
	}
}

// Size returns the current job manifest size
func (jm *Manifest) Size() int {
	return len(jm.jobs)
}

// HasJob checks if a job with a given ID already exists
func (jm *Manifest) HasJob(ID string) bool {
	_, exists := jm.jobs[ID]
	return exists
}

// InsertJob inserts a new job and checks that there are no duplicates
func (jm *Manifest) InsertJob(ID string, message Message) error {
	if jm.HasJob(ID) {
		return errors.New("A Job with the ID: " + ID + " already exists")
	}

	jm.jobs[ID] = Job{
		created: time.Now(),
		message: message,
	}

	return nil
}

// DeleteJob removes a job if it exists, otherwise throws an error
func (jm *Manifest) DeleteJob(ID string) error {

	if !jm.HasJob(ID) {
		return errors.New("A Job with the ID: " + ID + " does not exist")
	}

	println("[batchable] Deleting Job ID:", ID)

	delete(jm.jobs, ID)

	return nil
}

// DeleteDeceased removes all jobs that outlived the max duration relatvie to the current time
func (jm *Manifest) DeleteDeceased(maxLifetime time.Duration) error {
	for ID, jobItem := range jm.jobs {
		if time.Since(jobItem.created) > maxLifetime {
			println("[batchable] Job ID:", ID, "timed out")
			jm.DeleteJob(ID)
		}
	}

	return nil
}
