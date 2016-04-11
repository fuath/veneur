package veneuer

import (
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
)

// Worker is the doodad that does work.
type Worker struct {
	id          int
	percentiles []float64
	WorkChan    chan Metric
	QuitChan    chan bool
	counters    map[uint32]*Counter
	gauges      map[uint32]*Gauge
	histograms  map[uint32]*Histo
	mutex       *sync.Mutex
}

// NewWorker creates, and returns a new Worker object. Its only argument
// is a channel that the worker will receive work from.
func NewWorker(id int, percentiles []float64) *Worker {
	// Create, and return the worker.
	return &Worker{
		id:          id,
		percentiles: percentiles,
		WorkChan:    make(chan Metric, 100),
		QuitChan:    make(chan bool),
		mutex:       &sync.Mutex{},
		counters:    make(map[uint32]*Counter),
		gauges:      make(map[uint32]*Gauge),
		histograms:  make(map[uint32]*Histo),
	}
}

// Start "starts" the worker by starting a goroutine, that is
// an infinite "for-select" loop.
func (w *Worker) Start() {

	go func() {
		for {
			select {
			case m := <-w.WorkChan:
				w.ProcessMetric(&m)
			case <-w.QuitChan:
				// We have been asked to stop.
				log.WithFields(log.Fields{
					"worker": w.id,
				}).Error("Stopping")
				return
			}
		}
	}()
}

// ProcessMetric takes a Metric and samples it
//
// This is standalone to facilitate testing
func (w *Worker) ProcessMetric(m *Metric) {
	// start := time.Now()
	w.mutex.Lock()
	switch m.Type {
	case "counter":
		_, present := w.counters[m.Digest]
		if !present {
			log.WithFields(log.Fields{
				"name": m.Name,
			}).Debug("New counter")
			w.counters[m.Digest] = NewCounter(m.Name, m.Tags)
		}
		w.counters[m.Digest].Sample(m.Value)
	case "gauge":
		_, present := w.gauges[m.Digest]
		if !present {
			log.WithFields(log.Fields{
				"name": m.Name,
			}).Debug("New gauge")
			w.gauges[m.Digest] = NewGauge(m.Name, m.Tags)
		}
		w.gauges[m.Digest].Sample(m.Value)
	case "histogram", "timer":
		_, present := w.histograms[m.Digest]
		if !present {
			log.WithFields(log.Fields{
				"name": m.Name,
			}).Debug("New histogram")
			w.histograms[m.Digest] = NewHist(m.Name, m.Tags, w.percentiles)
		}
		w.histograms[m.Digest].Sample(m.Value)
	default:
		log.WithFields(log.Fields{
			"type": m.Type,
		}).Error("Unknown metric type")
	}
	// Keep track of how many packets we've processed
	// w.counters[Metric{Name: "veneur.stats.packets", Tags: fmt.Sprintf("worker_id:%d", w.id)}]++
	// Keep track of how long it took us to process a packet
	// hist := w.histograms[Metric{Name: "veneur.stats.process_duration_ns"}]
	// if hist == nil {
	// 	hist = metrics.NewHistogram(metrics.NewExpDecaySample(1028, 0.015))
	// 	ph := Metric{Name: "veneur.stats.process_duration_ns", Tags: fmt.Sprintf("worker_id:%d", w.id)}
	// 	w.histograms[ph] = hist
	// }
	// hist.Update(time.Now().Sub(start).Nanoseconds())

	w.mutex.Unlock()
}

// Flush generates DDMetrics to emit. Uses the supplied time
// to judge expiry of metrics for removal.
func (w *Worker) Flush(flushInterval time.Duration, expirySeconds time.Duration, currTime time.Time) []DDMetric {
	var postMetrics []DDMetric
	w.mutex.Lock()
	// TODO This should probably be a single function that passes in the metrics and the
	// map
	for k, v := range w.counters {
		if currTime.Sub(v.lastSampleTime) > expirySeconds {
			log.WithFields(log.Fields{
				"name": v.name,
			}).Debug("Expiring counter")
			delete(w.counters, k)
		} else {
			postMetrics = append(postMetrics, v.Flush(flushInterval)...)
		}
	}
	for k, v := range w.gauges {
		if currTime.Sub(v.lastSampleTime) > expirySeconds {
			log.WithFields(log.Fields{
				"name": v.name,
			}).Debug("Expiring gauge")
			delete(w.gauges, k)
		} else {
			postMetrics = append(postMetrics, v.Flush(flushInterval)...)
		}
	}
	for k, v := range w.histograms {
		if currTime.Sub(v.lastSampleTime) > expirySeconds {
			log.WithFields(log.Fields{
				"name": v.name,
			}).Debug("Expiring histogram")
			delete(w.histograms, k)
		} else {
			postMetrics = append(postMetrics, v.Flush(flushInterval)...)
		}
	}
	w.mutex.Unlock()
	return postMetrics
}

// Stop tells the worker to stop listening for work requests.
//
// Note that the worker will only stop *after* it has finished its work.
func (w *Worker) Stop() {
	go func() {
		w.QuitChan <- true
	}()
}
