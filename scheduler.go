package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron        *cron.Cron
	jobs        map[string]*CronJob
	persistFile string
	executor    *Executor
	notifyFn    func(string) // callback to send messages via Telegram
	mu          sync.RWMutex
}

type CronJob struct {
	ID       string    `json:"id"`
	Spec     string    `json:"spec"`     // cron expression
	Command  string    `json:"command"`  // bash command
	Label    string    `json:"label"`    // human-readable name
	Created  time.Time `json:"created"`
	LastRun  time.Time `json:"last_run,omitempty"`
	EntryID  cron.EntryID `json:"-"`
}

func NewScheduler(cfg SchedulerConfig, executor *Executor, notifyFn func(string)) *Scheduler {
	// Ensure persist directory exists
	os.MkdirAll(filepath.Dir(cfg.PersistFile), 0755)

	s := &Scheduler{
		cron:        cron.New(cron.WithSeconds()),
		jobs:        make(map[string]*CronJob),
		persistFile: cfg.PersistFile,
		executor:    executor,
		notifyFn:    notifyFn,
	}

	// Load persisted jobs
	s.load()

	return s
}

// Start begins the cron scheduler.
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// Add creates a new cron job.
// spec uses standard cron format: "0 */5 * * * *" (with seconds) or "@every 5m"
func (s *Scheduler) Add(id, spec, command, label string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[id]; exists {
		return fmt.Errorf("job %q already exists", id)
	}

	job := &CronJob{
		ID:      id,
		Spec:    spec,
		Command: command,
		Label:   label,
		Created: time.Now(),
	}

	entryID, err := s.cron.AddFunc(spec, func() {
		s.runJob(job)
	})
	if err != nil {
		return fmt.Errorf("invalid cron spec %q: %w", spec, err)
	}

	job.EntryID = entryID
	s.jobs[id] = job
	s.persist()

	return nil
}

// Remove deletes a cron job.
func (s *Scheduler) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[id]
	if !exists {
		return fmt.Errorf("job %q not found", id)
	}

	s.cron.Remove(job.EntryID)
	delete(s.jobs, id)
	s.persist()

	return nil
}

// List returns all registered jobs.
func (s *Scheduler) List() []*CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var jobs []*CronJob
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	return jobs
}

func (s *Scheduler) runJob(job *CronJob) {
	result, err := s.executor.Run(job.Command)

	s.mu.Lock()
	job.LastRun = time.Now()
	s.persist()
	s.mu.Unlock()

	// Notify via Telegram
	var msg string
	if err != nil {
		msg = fmt.Sprintf("⏰ Cron [%s] %s\n❌ Error: %s", job.ID, job.Label, err)
	} else {
		msg = fmt.Sprintf("⏰ Cron [%s] %s\n%s", job.ID, job.Label, FormatResult(result))
	}

	if s.notifyFn != nil {
		s.notifyFn(msg)
	}
}

func (s *Scheduler) persist() {
	data, err := json.MarshalIndent(s.jobs, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(s.persistFile, data, 0644)
}

func (s *Scheduler) load() {
	data, err := os.ReadFile(s.persistFile)
	if err != nil {
		return
	}

	var jobs map[string]*CronJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return
	}

	for _, job := range jobs {
		j := job // capture for closure
		entryID, err := s.cron.AddFunc(j.Spec, func() {
			s.runJob(j)
		})
		if err != nil {
			continue
		}
		j.EntryID = entryID
		s.jobs[j.ID] = j
	}
}

// FormatJobList formats the job list for display.
func FormatJobList(jobs []*CronJob) string {
	if len(jobs) == 0 {
		return "📋 No cron jobs configured."
	}

	msg := "📋 *Cron Jobs:*\n\n"
	for _, j := range jobs {
		lastRun := "never"
		if !j.LastRun.IsZero() {
			lastRun = j.LastRun.Format("Jan 02 15:04")
		}
		msg += fmt.Sprintf("• `%s` — %s\n  Schedule: `%s`\n  Command: `%s`\n  Last run: %s\n\n",
			j.ID, j.Label, j.Spec, j.Command, lastRun)
	}
	return msg
}
