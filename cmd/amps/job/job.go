package job

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	messagesInserted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "amps_messages_inserted_total",
		Help: "The total number of inserted messages from the broker",
	})
	messagesDeleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "amps_messages_deleted_total",
		Help: "The total number of inserted messages from the broker",
	})
	currentJobCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "amps_messages_count",
		Help: "The total number of inserted messages from the broker",
	})
	messageLifetime = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "amps_message_lifetime_seconds",
		Help:    "The total number of inserted messages from the broker",
		Buckets: prometheus.ExponentialBuckets(1, 2, 16),
	})
)

type Message interface {
	GetData() []byte
}

// Job represents a job item
type Job struct {
	Created time.Time
	Message Message
}

// Manifest represents the collection of current jobs
type Manifest struct {
	Mutex *sync.RWMutex
	Jobs  map[string]Job
}

// NewManifest creates a new Manifest with a predefined maxSize
func NewManifest(size int) Manifest {
	return Manifest{
		&sync.RWMutex{},
		make(map[string]Job, size),
	}
}

// Size returns the current job manifest size
func (jm *Manifest) Size() int {
	return len(jm.Jobs)
}

// HasJob checks if a job with a given ID already exists
func (jm *Manifest) HasJob(ID string) bool {
	_, exists := jm.Jobs[ID]
	return exists
}

// GetJob fetches a job by its ID
func (jm *Manifest) GetJob(ID string) Job {
	job := jm.Jobs[ID]
	return job
}

// InsertJob inserts a new job and checks that there are no duplicates
func (jm *Manifest) InsertJob(ID string, message Message) {
	messagesInserted.Inc()

	jm.Jobs[ID] = Job{
		Created: time.Now(),
		Message: message,
	}

	currentJobCount.Set(float64(jm.Size()))
}

// DeleteJob removes a job if it exists, otherwise throws an error
func (jm *Manifest) DeleteJob(ID string) {
	messagesDeleted.Inc()

	println("[AMPS] Deleting Job ID:", ID)

	job := jm.Jobs[ID]

	delete(jm.Jobs, ID)
	messageLifetime.Observe(float64(time.Since(job.Created).Seconds()))
	currentJobCount.Set(float64(jm.Size()))
}
