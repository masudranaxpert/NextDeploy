package phphosting

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"panel/internal/db"

	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

const (
	settingMinCronInterval = "php_min_cron_interval_minutes"
	cronRunTimeout         = 15 * time.Minute
	cronMaxConcurrent      = 8
)

// MinCronIntervalMinutes is the shortest allowed gap between two runs (admin setting).
func (m *Manager) MinCronIntervalMinutes(ctx context.Context) int {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(m.DB.GetSetting(ctx, settingMinCronInterval)), "%d", &n); err != nil || n < 1 {
		return 1
	}
	if n > 1440 {
		return 1440
	}
	return n
}

func (m *Manager) SetMinCronIntervalMinutes(ctx context.Context, n int) error {
	if n < 1 || n > 1440 {
		return fmt.Errorf("interval must be between 1 and 1440 minutes")
	}
	return m.DB.SetSetting(ctx, settingMinCronInterval, fmt.Sprint(n))
}

// ValidateSchedule parses a 5-field cron expression (or @hourly style descriptor) and
// enforces the minimum interval.
func ValidateSchedule(expr string, minMinutes int) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fmt.Errorf("schedule is required")
	}
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid schedule: %v", err)
	}
	if minMinutes <= 1 {
		return nil
	}
	t := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	minGap := time.Duration(1<<62 - 1)
	for i := 0; i < 400; i++ {
		next := sched.Next(t)
		if next.IsZero() {
			break
		}
		if i > 0 {
			if gap := next.Sub(t); gap < minGap {
				minGap = gap
			}
		}
		t = next
	}
	if minGap < time.Duration(minMinutes)*time.Minute {
		return fmt.Errorf("schedule runs more often than the allowed minimum of every %d minutes", minMinutes)
	}
	return nil
}

func ValidateCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return fmt.Errorf("command is required")
	}
	if len(cmd) > 2000 {
		return fmt.Errorf("command is too long")
	}
	if strings.ContainsRune(cmd, 0) {
		return fmt.Errorf("invalid command")
	}
	return nil
}

// StartCronScheduler runs due jobs once per minute until ctx is cancelled.
func (m *Manager) StartCronScheduler(ctx context.Context) {
	sem := make(chan struct{}, cronMaxConcurrent)
	go func() {
		for {
			now := time.Now()
			wait := now.Truncate(time.Minute).Add(time.Minute).Sub(now) + 500*time.Millisecond
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			tick := time.Now().Truncate(time.Minute)
			jobs, err := m.DB.ListAllEnabledPHPCronJobs(ctx)
			if err != nil {
				log.Printf("php cron: list jobs: %v", err)
				continue
			}
			for _, j := range jobs {
				sched, err := cronParser.Parse(j.Schedule)
				if err != nil {
					continue
				}
				if !sched.Next(tick.Add(-time.Minute)).Equal(tick) {
					continue
				}
				if !m.claimCron(j.ID) {
					continue // previous run still going
				}
				sem <- struct{}{}
				go func(j db.PHPCronJob) {
					defer func() { <-sem; m.releaseCron(j.ID) }()
					m.RunCronJob(ctx, j)
				}(j)
			}
		}
	}()
}

func (m *Manager) claimCron(id int64) bool {
	m.cronMu.Lock()
	defer m.cronMu.Unlock()
	if m.cronRunning[id] {
		return false
	}
	m.cronRunning[id] = true
	return true
}

func (m *Manager) releaseCron(id int64) {
	m.cronMu.Lock()
	delete(m.cronRunning, id)
	m.cronMu.Unlock()
}

// RunCronJob executes one job as the site user and records the outcome.
func (m *Manager) RunCronJob(ctx context.Context, j db.PHPCronJob) (status, output string) {
	site, err := m.DB.GetPHPSite(ctx, j.AppID)
	if err != nil {
		return "failed", "site not found"
	}
	cctx, cancel := context.WithTimeout(ctx, cronRunTimeout)
	defer cancel()
	res := m.RunInSite(cctx, site, j.Command)
	status = "ok"
	if !res.OK {
		status = fmt.Sprintf("failed (exit %d)", res.ExitCode)
		if cctx.Err() == context.DeadlineExceeded {
			status = "timed out"
		}
	}
	_ = m.DB.RecordPHPCronRun(context.Background(), j.ID, status, res.Output)
	return status, res.Output
}

// TryRunCronJobNow runs a job on demand unless it is already running.
func (m *Manager) TryRunCronJobNow(ctx context.Context, j db.PHPCronJob) (string, string, error) {
	if !m.claimCron(j.ID) {
		return "", "", fmt.Errorf("job is already running")
	}
	defer m.releaseCron(j.ID)
	st, out := m.RunCronJob(ctx, j)
	return st, out, nil
}
