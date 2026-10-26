package sysinfo

import (
	"context"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type Snapshot struct {
	GoVersion   string
	NumCPU      int
	MemTotalGB  float64
	MemUsedGB   float64
	MemUsedPct  float64
	DiskTotalGB float64
	DiskUsedGB  float64
	DiskUsedPct float64
	CPUPercent  float64
	HostNote    string
	PublicIP    string `json:"public_ip"`
}

const diskCacheTTL = 45 * time.Second

var (
	staticOnce sync.Once
	staticBase Snapshot

	diskMu   sync.RWMutex
	diskData Snapshot
	diskAt   time.Time

	cpuMu       sync.Mutex
	lastCPUIdle uint64
	lastCPUTot  uint64
	lastCPUPct  float64
	lastCPUAt   time.Time
)

// readProcStatCPU reads total CPU ticks from /proc/stat
func readProcStatCPU() (idle uint64, total uint64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "cpu" {
			var tot uint64
			for i := 1; i < len(fields); i++ {
				val, _ := strconv.ParseUint(fields[i], 10, 64)
				tot += val
			}
			idleVal, _ := strconv.ParseUint(fields[4], 10, 64)   // idle
			iowaitVal, _ := strconv.ParseUint(fields[5], 10, 64) // iowait
			return idleVal + iowaitVal, tot, true
		}
	}
	return 0, 0, false
}

func calculateCPUPercent() float64 {
	cpuMu.Lock()
	defer cpuMu.Unlock()

	idle, total, ok := readProcStatCPU()
	if !ok || total == 0 {
		return lastCPUPct
	}

	now := time.Now()
	if lastCPUTot == 0 {
		lastCPUIdle = idle
		lastCPUTot = total
		lastCPUAt = now
		return lastCPUPct
	}

	if total > lastCPUTot && now.Sub(lastCPUAt) >= 200*time.Millisecond {
		diffTot := float64(total - lastCPUTot)
		diffIdle := float64(idle - lastCPUIdle)
		if diffTot > 0 && diffIdle <= diffTot {
			pct := (1.0 - (diffIdle / diffTot)) * 100.0
			lastCPUPct = math.Round(pct*10) / 10
		}
		lastCPUIdle = idle
		lastCPUTot = total
		lastCPUAt = now
	}
	return lastCPUPct
}

// DetectHostIP discovers the public IP of the host machine.
func DetectHostIP() string {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	for _, endpoint := range []string{"https://api.ipify.org", "https://icanhazip.com"} {
		resp, err := client.Get(endpoint)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		_ = resp.Body.Close()
		ipStr := strings.TrimSpace(string(b))
		if ip := net.ParseIP(ipStr); ip != nil && ip.To4() != nil {
			return ip.String()
		}
	}
	if conn, err := net.DialTimeout("udp", "1.1.1.1:80", 500*time.Millisecond); err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP.To4() != nil {
			return addr.IP.String()
		}
	}
	return "127.0.0.1"
}

func detectHostIP() string {
	return DetectHostIP()
}

func initStatic() {
	staticBase = Snapshot{
		GoVersion: runtime.Version(),
		NumCPU:    runtime.NumCPU(),
		HostNote:  "Docker Container (" + runtime.GOOS + "/" + runtime.GOARCH + ")",
		PublicIP:  detectHostIP(),
	}
}

func refreshDisk(ctx context.Context) {
	diskMu.RLock()
	if time.Since(diskAt) < diskCacheTTL {
		diskMu.RUnlock()
		return
	}
	diskMu.RUnlock()

	var d Snapshot
	if parts, err := disk.UsageWithContext(ctx, "/"); err == nil && parts != nil {
		d.DiskTotalGB = math.Round(float64(parts.Total)/(1024*1024*1024)*10) / 10
		d.DiskUsedGB = math.Round(float64(parts.Used)/(1024*1024*1024)*10) / 10
		d.DiskUsedPct = math.Round(parts.UsedPercent*10) / 10
	}

	diskMu.Lock()
	diskData = d
	diskAt = time.Now()
	diskMu.Unlock()
}

func Collect(ctx context.Context) Snapshot {
	staticOnce.Do(initStatic)
	refreshDisk(ctx)

	s := staticBase
	diskMu.RLock()
	s.DiskTotalGB = diskData.DiskTotalGB
	s.DiskUsedGB = diskData.DiskUsedGB
	s.DiskUsedPct = diskData.DiskUsedPct
	diskMu.RUnlock()

	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil && v != nil {
		s.MemTotalGB = math.Round(float64(v.Total)/(1024*1024*1024)*10) / 10
		s.MemUsedGB = math.Round(float64(v.Used)/(1024*1024*1024)*10) / 10
		if v.Total > 0 {
			s.MemUsedPct = math.Round((float64(v.Used)*100/float64(v.Total))*10) / 10
		}
	}
	s.CPUPercent = calculateCPUPercent()
	return s
}

func InvalidateDiskCache() {
	diskMu.Lock()
	diskAt = time.Time{}
	diskMu.Unlock()
}

type TelemetryPoint struct {
	Time       string  `json:"time"`
	CPU        float64 `json:"cpu"`
	Memory     float64 `json:"memory"`
	DiskIO     float64 `json:"diskIo"`
	NetInMbps  float64 `json:"netInMbps"`
	NetOutMbps float64 `json:"netOutMbps"`
	Requests   int     `json:"requests"`
}

var (
	telemetryMu      sync.RWMutex
	telemetryHistory []TelemetryPoint
	maxTelemetryLen  = 120
	collectorOnce    sync.Once

	// Live real telemetry counters
	reqCounter uint64
	lastNetIn  uint64
	lastNetOut uint64
	lastNetAt  time.Time
)

// RecordRequest increments the live request counter for throughput calculations.
func RecordRequest() {
	atomic.AddUint64(&reqCounter, 1)
}

func readProcNetDev() (rxBytes uint64, txBytes uint64) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		// Ignore local loopback
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) >= 9 {
			rx, _ := strconv.ParseUint(fields[0], 10, 64)
			tx, _ := strconv.ParseUint(fields[8], 10, 64)
			rxBytes += rx
			txBytes += tx
		}
	}
	return rxBytes, txBytes
}

func GetTelemetryHistory() []TelemetryPoint {
	StartTelemetryCollector()
	telemetryMu.RLock()
	defer telemetryMu.RUnlock()
	res := make([]TelemetryPoint, len(telemetryHistory))
	copy(res, telemetryHistory)
	return res
}

func StartTelemetryCollector() {
	collectorOnce.Do(func() {
		snap := Collect(context.Background())
		now := time.Now()
		initialRx, initialTx := readProcNetDev()
		lastNetIn = initialRx
		lastNetOut = initialTx
		lastNetAt = now

		telemetryMu.Lock()
		if len(telemetryHistory) == 0 {
			for i := 40; i >= 0; i-- {
				t := now.Add(-time.Duration(i*30) * time.Second)
				telemetryHistory = append(telemetryHistory, TelemetryPoint{
					Time:       t.Format("15:04"),
					CPU:        snap.CPUPercent,
					Memory:     snap.MemUsedPct,
					DiskIO:     snap.DiskUsedPct,
					NetInMbps:  0,
					NetOutMbps: 0,
					Requests:   0,
				})
			}
		}
		telemetryMu.Unlock()

		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				s := Collect(ctx)
				cancel()

				now := time.Now()
				// Calculate real Requests per minute
				reqs := atomic.SwapUint64(&reqCounter, 0)
				// Since ticker is 15s, rate per minute is reqs * 4
				reqRate := int(reqs * 4)

				// Calculate real network throughput in Mbps
				rxBytes, txBytes := readProcNetDev()
				var netInMbps, netOutMbps float64
				if !lastNetAt.IsZero() && now.After(lastNetAt) {
					elapsedSec := now.Sub(lastNetAt).Seconds()
					if elapsedSec > 0 && rxBytes >= lastNetIn && txBytes >= lastNetOut {
						// (bytes * 8) / (1024 * 1024) / elapsedSec
						netInMbps = float64(rxBytes-lastNetIn) * 8 / (1000 * 1000) / elapsedSec
						netOutMbps = float64(txBytes-lastNetOut) * 8 / (1000 * 1000) / elapsedSec
						// round to 2 decimals
						netInMbps = math.Round(netInMbps*100) / 100
						netOutMbps = math.Round(netOutMbps*100) / 100
					}
				}
				lastNetIn = rxBytes
				lastNetOut = txBytes
				lastNetAt = now

				pt := TelemetryPoint{
					Time:       now.Format("15:04"),
					CPU:        s.CPUPercent,
					Memory:     s.MemUsedPct,
					DiskIO:     s.DiskUsedPct,
					NetInMbps:  netInMbps,
					NetOutMbps: netOutMbps,
					Requests:   reqRate,
				}

				telemetryMu.Lock()
				if len(telemetryHistory) >= maxTelemetryLen {
					telemetryHistory = telemetryHistory[1:]
				}
				telemetryHistory = append(telemetryHistory, pt)
				telemetryMu.Unlock()
			}
		}()
	})
}
