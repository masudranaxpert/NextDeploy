package handlers

import (
	"context"
	"log"
	"time"

	"panel/internal/db"
)

func (p *Panel) StartBackupWorker() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		
		for range ticker.C {
			p.processScheduledBackups()
		}
	}()
	
	log.Println("Backup worker started")
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
				
				go p.runBackupJob(ctx, historyID, app.ID, dest, schedule.BackupType)
				
				_ = p.DB.UpdateBackupScheduleLastRun(ctx, schedule.ID)
				
				go p.cleanupOldBackups(ctx, app.ID, dest.ID, schedule.BackupType, schedule.RetentionCount)
			}
		}
	}
}

func shouldRunSchedule(schedule db.BackupSchedule) bool {
	if schedule.LastRun == "" {
		return true
	}
	
	lastRun, err := time.Parse("2006-01-02 15:04:05", schedule.LastRun)
	if err != nil {
		return true
	}
	
	return time.Since(lastRun) > 5*time.Minute
}

func (p *Panel) cleanupOldBackups(ctx context.Context, appID string, destID int64, backupType string, keepCount int) {
	_ = p.DB.DeleteOldBackups(ctx, appID, destID, backupType, keepCount)
}
