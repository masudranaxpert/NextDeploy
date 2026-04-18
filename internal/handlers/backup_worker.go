package handlers

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"panel/internal/db"
)

var backupCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func (p *Panel) StartBackupWorker() {
	if n, err := p.DB.ResetInFlightBackups(context.Background(), "panel restarted while this backup was running"); err != nil {
		log.Printf("[backup] reset in-flight rows: %v", err)
	} else if n > 0 {
		log.Printf("[backup] reset %d in-flight backup row(s) to failed", n)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[backup-worker] recovered from panic: %v — restarting", r)
				go p.StartBackupWorker()
			}
		}()

		p.safeProcessScheduledBackups()
		firstWait := time.Until(time.Now().Truncate(time.Minute).Add(time.Minute))
		if firstWait > 0 {
			timer := time.NewTimer(firstWait)
			<-timer.C
		}
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			p.safeProcessScheduledBackups()
		}
	}()

	log.Println("Backup worker started")
}

func (p *Panel) safeProcessScheduledBackups() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[backup-worker] tick panic: %v", r)
		}
	}()
	p.processScheduledBackups()
}

func (p *Panel) processScheduledBackups() {
	ctx := context.Background()
	
	apps, err := p.DB.ListApps(ctx)
	if err != nil {
		return
	}
	
	for _, app := range apps {
		schedules, err := p.DB.ListBackupSchedules(ctx, app.ID)
		if err != nil {
			continue
		}
		
		for _, schedule := range schedules {
			if !schedule.Enabled {
				continue
			}
			
			if shouldRunSchedule(schedule) {
				dest, err := p.DB.GetBackupDestination(ctx, schedule.DestinationID)
				if err != nil {
					continue
				}
				
				historyID, err := p.DB.CreateBackupHistory(ctx, app.ID, dest.ID, schedule.BackupType, "", "running", "", 0)
				if err != nil {
					continue
				}
				
				retention := schedule.RetentionCount
				if retention < 1 {
					retention = 5
				}
				go p.runBackupJob(ctx, historyID, app.ID, dest, schedule.BackupType, schedule.VolumeNames, retention)

				_ = p.DB.UpdateBackupScheduleLastRun(ctx, schedule.ID)
			}
		}
	}
}

func shouldRunSchedule(schedule db.BackupSchedule) bool {
	expr := strings.TrimSpace(schedule.CronExpression)
	if expr == "" {
		return false
	}
	sched, err := backupCronParser.Parse(expr)
	if err != nil {
		return false
	}
	now := time.Now()
	if schedule.LastRun == "" {
		base, ok := backupScheduleBaseTime(schedule, now)
		if !ok {
			return false
		}
		next := sched.Next(base)
		return !now.Before(next)
	}
	lastRun, err := parseScheduleDBTime(schedule.LastRun)
	if err != nil {
		base, ok := backupScheduleBaseTime(schedule, now)
		if !ok {
			return false
		}
		next := sched.Next(base)
		return !now.Before(next)
	}
	next := sched.Next(lastRun)
	return !now.Before(next)
}

func parseScheduleDBTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty schedule timestamp")
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err != nil {
		return time.Time{}, err
	}
	return t.In(time.Local), nil
}

func backupScheduleBaseTime(schedule db.BackupSchedule, now time.Time) (time.Time, bool) {
	if createdAt := strings.TrimSpace(schedule.CreatedAt); createdAt != "" {
		t, err := parseScheduleDBTime(createdAt)
		if err == nil {
			return t, true
		}
	}
	return now, true
}

// nextBackupScheduleRun returns the next calendar run time after now for UI (enabled schedules with valid cron).
func nextBackupScheduleRun(schedule db.BackupSchedule) (time.Time, bool) {
	if !schedule.Enabled {
		return time.Time{}, false
	}
	expr := strings.TrimSpace(schedule.CronExpression)
	if expr == "" {
		return time.Time{}, false
	}
	sched, err := backupCronParser.Parse(expr)
	if err != nil {
		return time.Time{}, false
	}
	now := time.Now()
	var next time.Time
	if schedule.LastRun != "" {
		lastRun, err := parseScheduleDBTime(schedule.LastRun)
		if err != nil {
			next = sched.Next(now.Add(-time.Second))
		} else {
			next = sched.Next(lastRun)
		}
	} else {
		next = sched.Next(now.Add(-time.Second))
	}
	for !next.After(now) {
		n := sched.Next(next)
		if !n.After(next) {
			break
		}
		next = n
	}
	return next, true
}

