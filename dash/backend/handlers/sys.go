package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"

	"github.com/danilrybalkin/apollo-dash/tools"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type SysMetrics struct {
	CPUPercent  []float64              `json:"cpuPercent"`
	MemTotal    uint64                 `json:"memTotal"`
	MemUsed     uint64                 `json:"memUsed"`
	MemPercent  float64                `json:"memPercent"`
	DiskTotal   uint64                 `json:"diskTotal"`
	DiskUsed    uint64                 `json:"diskUsed"`
	DiskPercent float64                `json:"diskPercent"`
	NetSent     uint64                 `json:"netSent"`
	NetRecv     uint64                 `json:"netRecv"`
	HostOS      string                 `json:"hostOs"`
	Uptime      uint64                 `json:"uptime"`
	Temps       []host.TemperatureStat `json:"temps"`
}

func SysStatsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	metrics := SysMetrics{}

	// CPU
	cpuPercents, err := cpu.Percent(0, false)
	if err == nil {
		metrics.CPUPercent = cpuPercents
	}

	// Memory
	vMem, err := mem.VirtualMemory()
	if err == nil {
		metrics.MemTotal = vMem.Total
		metrics.MemUsed = vMem.Used
		metrics.MemPercent = vMem.UsedPercent
	}

	// Disk (root)
	d, err := disk.Usage("/")
	if err == nil {
		metrics.DiskTotal = d.Total
		metrics.DiskUsed = d.Used
		metrics.DiskPercent = d.UsedPercent
	}

	// Network
	nv, err := net.IOCounters(false)
	if err == nil && len(nv) > 0 {
		metrics.NetSent = nv[0].BytesSent
		metrics.NetRecv = nv[0].BytesRecv
	}

	// Host
	hInfo, err := host.Info()
	if err == nil {
		metrics.HostOS = hInfo.OS
		metrics.Uptime = hInfo.Uptime
	}

	// Temps
	// Note: gopsutil temps might return empty depending on OS (macOS is notorious for missing SMC sensors without extra privileges)
	tStats, err := host.SensorsTemperatures()
	if err == nil {
		metrics.Temps = tStats
	}

	json.NewEncoder(w).Encode(metrics)
}

func SandboxRestoreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Hash == "" {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	cmd := exec.Command("git", "reset", "--hard", payload.Hash)
	cmd.Dir = tools.WorkspaceDir
	out, err := cmd.CombinedOutput()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Error: %v, Output: %s", err, string(out))})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"success": "true"})
}
