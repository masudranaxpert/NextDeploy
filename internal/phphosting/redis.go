package phphosting

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	settingRedisEnabled = "php_redis_enabled"
	settingRedisImage   = "php_redis_image"
)

// RedisStatus describes the state of the shared Redis container for PHP hosting.
type RedisStatus struct {
	Enabled          bool   `json:"enabled"`
	Exists           bool   `json:"exists"`
	Running          bool   `json:"running"`
	Image            string `json:"image"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Version          string `json:"version"`
	UsedMemoryHuman  string `json:"used_memory_human"`
	ConnectedClients int    `json:"connected_clients"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
}

func (m *Manager) redisImage(ctx context.Context) string {
	if v := strings.TrimSpace(m.DB.GetSetting(ctx, settingRedisImage)); v != "" {
		return v
	}
	return DefaultRedis
}

// RedisStatus inspects the shared Redis container and queries stats via redis-cli.
func (m *Manager) RedisStatus(ctx context.Context) RedisStatus {
	image := m.redisImage(ctx)
	st := RedisStatus{
		Enabled: m.DB.GetSetting(ctx, settingRedisEnabled) == "1",
		Image:   image,
		Host:    RedisContainer,
		Port:    6379,
	}

	ci := inspectContainer(ctx, RedisContainer)
	st.Exists = ci.Exists
	st.Running = ci.Running
	if !st.Running {
		return st
	}

	// Fetch runtime metrics using redis-cli inside container with a strict timeout
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	res := docker(cmdCtx, "exec", RedisContainer, "redis-cli", "info")
	if !res.OK {
		return st
	}

	for _, line := range strings.Split(res.Output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "redis_version:") {
			st.Version = strings.TrimPrefix(line, "redis_version:")
		} else if strings.HasPrefix(line, "used_memory_human:") {
			st.UsedMemoryHuman = strings.TrimPrefix(line, "used_memory_human:")
		} else if strings.HasPrefix(line, "connected_clients:") {
			st.ConnectedClients, _ = strconv.Atoi(strings.TrimPrefix(line, "connected_clients:"))
		} else if strings.HasPrefix(line, "uptime_in_seconds:") {
			st.UptimeSeconds, _ = strconv.ParseInt(strings.TrimPrefix(line, "uptime_in_seconds:"), 10, 64)
		}
	}

	return st
}

// StartRedis launches or starts the shared Redis container on m.DBNetwork.
func (m *Manager) StartRedis(ctx context.Context) error {
	if err := m.ensureNetwork(ctx, m.DBNetwork); err != nil {
		return err
	}

	ci := inspectContainer(ctx, RedisContainer)
	if ci.Exists {
		if !ci.Running {
			if res := docker(ctx, "start", RedisContainer); !res.OK {
				return fmt.Errorf("start redis: %s", res.Output)
			}
		}
		_ = m.DB.SetSetting(ctx, settingRedisEnabled, "1")
		return nil
	}

	image := m.redisImage(ctx)
	args := []string{
		"run", "-d",
		"--name", RedisContainer,
		"--restart", "unless-stopped",
		"--network", m.DBNetwork,
		"--label", "nd.php=redis",
		"-v", RedisVolume + ":/data",
		"-p", "127.0.0.1:6379:6379",
		image,
		"redis-server", "--appendonly", "yes",
	}

	res := docker(ctx, args...)
	if !res.OK {
		// Fallback: If port 6379 is occupied on host, run on internal network without -p
		fallbackArgs := []string{
			"run", "-d",
			"--name", RedisContainer,
			"--restart", "unless-stopped",
			"--network", m.DBNetwork,
			"--label", "nd.php=redis",
			"-v", RedisVolume + ":/data",
			image,
			"redis-server", "--appendonly", "yes",
		}
		res = docker(ctx, fallbackArgs...)
		if !res.OK {
			return fmt.Errorf("run redis: %s", res.Output)
		}
	}

	_ = m.DB.SetSetting(ctx, settingRedisEnabled, "1")
	return nil
}

// StopRedis stops the shared Redis container.
func (m *Manager) StopRedis(ctx context.Context) error {
	ci := inspectContainer(ctx, RedisContainer)
	if !ci.Exists || !ci.Running {
		_ = m.DB.SetSetting(ctx, settingRedisEnabled, "0")
		return nil
	}

	if res := docker(ctx, "stop", RedisContainer); !res.OK {
		return fmt.Errorf("stop redis: %s", res.Output)
	}

	_ = m.DB.SetSetting(ctx, settingRedisEnabled, "0")
	return nil
}

// RestartRedis restarts the shared Redis container.
func (m *Manager) RestartRedis(ctx context.Context) error {
	ci := inspectContainer(ctx, RedisContainer)
	if !ci.Exists {
		return m.StartRedis(ctx)
	}

	if res := docker(ctx, "restart", RedisContainer); !res.OK {
		return fmt.Errorf("restart redis: %s", res.Output)
	}

	_ = m.DB.SetSetting(ctx, settingRedisEnabled, "1")
	return nil
}
