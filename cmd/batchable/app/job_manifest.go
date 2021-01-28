package app

import (
	"errors"
	"sync"
	"time"

	"github.com/nats-io/stan.go"
)

// Job represents a job item
type Job struct {
	created time.Time
	message *stan.Msg
}

// JobManifest represents the collection of current jobs
type JobManifest struct {
	mutex sync.Mutex
	jobs  map[string]Job
}

// NewJobManifest creates a new jobManifest with a predefined maxSize
func NewJobManifest(size int) JobManifest {
	return JobManifest{
		sync.Mutex{},
		make(map[string]Job, size),
	}
}

// Size returns the current job manifest size
func (jm *JobManifest) Size() int {
	return len(jm.jobs)
}

// HasJob checks if a job with a given ID already exists
func (jm *JobManifest) HasJob(ID string) bool {
	_, exists := jm.jobs[ID]
	if exists {
		return true
	}

	return false
}

// InsertJob inserts a new job and checks that there are no duplicates
func (jm *JobManifest) InsertJob(ID string) error {

	if jm.HasJob(ID) {
		return errors.New("A Job with the ID: " + ID + " already exists")
	}

	jm.jobs[ID] = Job{
		created: time.Now(),
	}

	return nil
}

// DeleteJob removes a job if it exists, otherwise throws an error
func (jm *JobManifest) DeleteJob(ID string) error {

	if !jm.HasJob(ID) {
		return errors.New("A Job with the ID: " + ID + " already exists")
	}

	delete(jm.jobs, ID)

	return nil
}

// DeleteDeceased removes all jobs that outlived the max duration relatvie to the current time
func (jm *JobManifest) DeleteDeceased(maxLifetime time.Duration) error {

	for ID, jobItem := range jm.jobs {
		if time.Now().Sub(jobItem.created) > maxLifetime {
			println("Job ID:", ID, "timed out")
			jm.DeleteJob(ID)
		}
	}

	return nil
}

// Lock locks the job manifest mutex
func (jm *JobManifest) Lock() {
	jm.mutex.Lock()
}

// Unlock unlocks the job manifest mutex
func (jm *JobManifest) Unlock() {
	jm.mutex.Unlock()
}
