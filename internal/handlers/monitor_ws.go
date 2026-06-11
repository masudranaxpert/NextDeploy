package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"panel/internal/db"
	"panel/internal/dockerapi"
	"panel/internal/sysinfo"

	"github.com/fasthttp/websocket"
	fws "github.com/gofiber/contrib/websocket"
)

type monitorPayload struct {
	Sys                    sysinfo.Snapshot              `json:"sys"`
	UsageRows              []dockerapi.ContainerUsageRow `json:"usageRows"`
	DockerError            string                        `json:"dockerError"`
	UpdatedAt              string                        `json:"updatedAt"`
	TotalAllocatedMemoryGB float64                       `json:"totalAllocatedMemoryGB"`
	TotalAllocatedCPUs     float64                       `json:"totalAllocatedCPUs"`
	MemoryAllocatedPct     float64                       `json:"memoryAllocatedPct"`
	CPUAllocatedPct        float64                       `json:"cpuAllocatedPct"`
	LimitedUserCount       int                           `json:"limitedUserCount"`
	UsedMemoryGB           float64                       `json:"usedMemoryGB"`
	UsedMemoryPct          float64                       `json:"usedMemoryPct"`
	UsedCPUs               float64                       `json:"usedCPUs"`
	UsedCPUPct             float64                       `json:"usedCPUPct"`
}

func (p *Panel) MonitorWebSocket(c *fws.Conn) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	prev := map[string]dockerapi.ContainerUsageRow{}
	prevAt := time.Now()

	sendSnapshot := func() error {
		u, uOk := c.Locals(contextUserKey).(db.User)
		if uOk {
			dbUser, err := p.DB.GetUserByID(context.Background(), u.ID)
			if err != nil || dbUser.Status == db.UserStatusSuspended || dbUser.Role != db.RoleAdmin {
				_ = c.Close()
				return errors.New("forbidden or suspended")
			}
		}
		now := time.Now()
		ctx := context.Background()
		sys := sysinfo.Collect(ctx)
		rows, errMsg := dockerapi.ListContainerUsage(ctx)
		elapsed := now.Sub(prevAt).Seconds()
		if elapsed <= 0 {
			elapsed = 2
		}
		for i := range rows {
			if old, ok := prev[rows[i].ID]; ok {
				if rows[i].NetInput >= old.NetInput {
					rows[i].NetDLRateHuman = dockerapi.HumanBytes(uint64(float64(rows[i].NetInput-old.NetInput) / elapsed)) + "/s"
				} else {
					rows[i].NetDLRateHuman = "0 B/s"
				}
				if rows[i].NetOutput >= old.NetOutput {
					rows[i].NetULRateHuman = dockerapi.HumanBytes(uint64(float64(rows[i].NetOutput-old.NetOutput) / elapsed)) + "/s"
				} else {
					rows[i].NetULRateHuman = "0 B/s"
				}
			} else {
				rows[i].NetDLRateHuman = "—"
				rows[i].NetULRateHuman = "—"
			}
		}
		nextPrev := make(map[string]dockerapi.ContainerUsageRow, len(rows))
		for _, row := range rows {
			nextPrev[row.ID] = row
		}
		prev = nextPrev
		prevAt = now

		users, _ := p.DB.ListUsers(ctx)
		var totalAllocatedMemoryMB int
		var totalAllocatedCPUs float64
		var limitedUsers int
		for _, u := range users {
			if u.Role != db.RoleAdmin {
				limitedUsers++
				totalAllocatedMemoryMB += u.MaxMemoryMB
				totalAllocatedCPUs += u.MaxCPUs
			}
		}

		memTotal := sys.MemTotalGB
		var memPct float64
		if memTotal > 0 {
			memPct = ((float64(totalAllocatedMemoryMB) / 1024.0) / memTotal) * 100.0
		}
		cpuTotal := float64(sys.NumCPU)
		var cpuPct float64
		if cpuTotal > 0 {
			cpuPct = (totalAllocatedCPUs / cpuTotal) * 100.0
		}

		// Live consumption across all containers: limits are caps, not reservations,
		// so the panel reports what is actually being used right now.
		var usedMemBytes uint64
		var usedCPUs float64
		for _, row := range rows {
			usedMemBytes += row.MemUsage
			usedCPUs += row.CPUPercent / 100.0
		}
		usedMemGB := float64(usedMemBytes) / (1024.0 * 1024.0 * 1024.0)
		var usedMemPct float64
		if memTotal > 0 {
			usedMemPct = (usedMemGB / memTotal) * 100.0
		}
		var usedCPUPct float64
		if cpuTotal > 0 {
			usedCPUPct = (usedCPUs / cpuTotal) * 100.0
		}

		payload := monitorPayload{
			Sys:                    sys,
			UsageRows:              rows,
			DockerError:            errMsg,
			UpdatedAt:              now.Format(time.RFC3339),
			TotalAllocatedMemoryGB: float64(totalAllocatedMemoryMB) / 1024.0,
			TotalAllocatedCPUs:     totalAllocatedCPUs,
			MemoryAllocatedPct:     memPct,
			CPUAllocatedPct:        cpuPct,
			LimitedUserCount:       limitedUsers,
			UsedMemoryGB:           usedMemGB,
			UsedMemoryPct:          usedMemPct,
			UsedCPUs:               usedCPUs,
			UsedCPUPct:             usedCPUPct,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return c.WriteMessage(websocket.TextMessage, body)
	}

	if err := sendSnapshot(); err != nil {
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := sendSnapshot(); err != nil {
				return
			}
		}
	}
}
