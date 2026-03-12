package task

import (
	"log/slog"
	"sync"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/robfig/cron/v3"
)

// RunHandler is called by the scheduler when a task is due.
type RunHandler func(task Task)

// Scheduler manages cron-based task scheduling.
type Scheduler struct {
	store    *Store
	cron     *cron.Cron
	handler  RunHandler
	eventBus *bus.Bus
	logger   *slog.Logger
	mu       sync.Mutex
	entries  map[int64]cron.EntryID
	stopCh   chan struct{}
}

func NewScheduler(store *Store, handler RunHandler, eventBus *bus.Bus, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		store:    store,
		cron:     cron.New(),
		handler:  handler,
		eventBus: eventBus,
		logger:   logger,
		entries:  make(map[int64]cron.EntryID),
	}
}

// Start loads all scheduled tasks, begins the cron runner, and subscribes
// to TaskChanged events so the schedule stays in sync with the database.
// It also starts a wake detector that catches up on tasks missed during sleep.
func (s *Scheduler) Start() error {
	s.mu.Lock()

	tasks, err := s.store.ListScheduledTasks()
	if err != nil {
		s.mu.Unlock()
		return err
	}

	for _, t := range tasks {
		s.addTaskLocked(t)
	}

	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	s.cron.Start()
	s.logger.Info("task scheduler started", "scheduled_tasks", len(tasks))

	// Auto-reload when tasks are created, updated, or deleted.
	if s.eventBus != nil {
		go s.watchChanges()
	}

	go s.watchWake()

	return nil
}

// watchChanges subscribes to TaskChanged events and reloads the schedule.
func (s *Scheduler) watchChanges() {
	s.mu.Lock()
	stop := s.stopCh
	s.mu.Unlock()

	ch := s.eventBus.Subscribe(bus.TaskChanged)
	defer s.eventBus.Unsubscribe(ch)

	for {
		select {
		case <-stop:
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			if err := s.Reload(); err != nil {
				s.logger.Error("scheduler reload on task change failed", "error", err)
			}
		}
	}
}

const (
	wakeCheckInterval = 30 * time.Second
	// If wall clock advanced more than this beyond the tick interval, we slept.
	wakeThreshold = 60 * time.Second
)

// watchWake detects OS sleep/wake by comparing elapsed wall-clock time against
// the expected tick interval. On wake, it reloads the cron schedule (so
// next-fire times are recalculated) and fires one catch-up run per task that
// missed a firing during the sleep window.
func (s *Scheduler) watchWake() {
	s.mu.Lock()
	stop := s.stopCh
	s.mu.Unlock()

	ticker := time.NewTicker(wakeCheckInterval)
	defer ticker.Stop()

	lastTick := time.Now()

	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			elapsed := now.Sub(lastTick)
			lastTick = now

			if elapsed <= wakeThreshold {
				continue
			}

			sleepStart := now.Add(-elapsed)
			s.logger.Info("wake detected", "slept_for", elapsed.String())

			// Reload so cron recalculates next-fire times from the current clock.
			if err := s.Reload(); err != nil {
				s.logger.Error("scheduler reload after wake failed", "error", err)
			}

			s.catchUpMissed(sleepStart, now)
		}
	}
}

// catchUpMissed fires one catch-up run for each scheduled task that should
// have fired during the window [sleepStart, wakeTime) but did not.
func (s *Scheduler) catchUpMissed(sleepStart, wakeTime time.Time) {
	tasks, err := s.store.ListScheduledTasks()
	if err != nil {
		s.logger.Error("catchup: failed to list tasks", "error", err)
		return
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

	for _, t := range tasks {
		sched, err := parser.Parse(t.CronExpr)
		if err != nil {
			s.logger.Warn("catchup: bad cron expr", "task", t.Name, "cron", t.CronExpr, "error", err)
			continue
		}

		// Determine whether this task had a scheduled firing inside the sleep window.
		// Walk forward from sleepStart; if the next firing is before wakeTime, it was missed.
		nextDue := sched.Next(sleepStart)
		if nextDue.IsZero() || !nextDue.Before(wakeTime) {
			continue
		}

		// Check whether the task actually ran during or after the sleep window.
		lastRun, err := s.store.LastRunTime(t.ID)
		if err != nil {
			s.logger.Warn("catchup: failed to get last run time", "task", t.Name, "error", err)
			continue
		}
		if !lastRun.Before(sleepStart) {
			// Already ran during the window (or after). No catch-up needed.
			continue
		}

		s.logger.Info("catchup: firing missed task", "task", t.Name, "task_id", t.ID,
			"missed_at", nextDue.Format(time.RFC3339))

		if s.handler != nil {
			go s.handler(t)
		}
	}
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.stopCh != nil {
		close(s.stopCh)
		s.stopCh = nil
	}
	s.mu.Unlock()

	ctx := s.cron.Stop()
	<-ctx.Done()
}

// Reload refreshes the scheduled tasks from the store.
func (s *Scheduler) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for taskID, entryID := range s.entries {
		s.cron.Remove(entryID)
		delete(s.entries, taskID)
	}

	tasks, err := s.store.ListScheduledTasks()
	if err != nil {
		return err
	}

	for _, t := range tasks {
		s.addTaskLocked(t)
	}

	s.logger.Info("scheduler reloaded", "scheduled_tasks", len(tasks))
	return nil
}

// addTaskLocked adds a task to the cron. Caller must hold s.mu.
func (s *Scheduler) addTaskLocked(t Task) {
	entryID, err := s.cron.AddFunc(t.CronExpr, func() {
		s.logger.Info("cron trigger", "task", t.Name, "task_id", t.ID)

		if s.eventBus != nil {
			s.eventBus.Publish(bus.Event{
				Type: bus.TaskStarted,
				Payload: map[string]any{
					"task_id": t.ID,
					"trigger": string(TriggerCron),
				},
			})
		}

		if s.handler != nil {
			s.handler(t)
		}
	})
	if err != nil {
		s.logger.Error("failed to schedule task", "task", t.Name, "cron", t.CronExpr, "error", err)
		return
	}

	s.entries[t.ID] = entryID
}

// NextScheduledTime returns the earliest next-fire time across all
// scheduled cron entries. If no tasks are scheduled it returns the zero
// time and false.
func (s *Scheduler) NextScheduledTime() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := s.cron.Entries()
	if len(entries) == 0 {
		return time.Time{}, false
	}
	earliest := entries[0].Next
	for _, e := range entries[1:] {
		if e.Next.Before(earliest) {
			earliest = e.Next
		}
	}
	return earliest, true
}

// ScheduledCount returns the number of currently scheduled tasks.
func (s *Scheduler) ScheduledCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// NextRunTimes returns a map from task ID to the next scheduled run time.
// Tasks that are not scheduled or whose next time is zero are omitted.
func (s *Scheduler) NextRunTimes() map[int64]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[int64]time.Time, len(s.entries))
	for taskID, entryID := range s.entries {
		entry := s.cron.Entry(entryID)
		if !entry.Next.IsZero() {
			out[taskID] = entry.Next
		}
	}
	return out
}
