package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/danilrybalkin/apollo-dash/db"
	"github.com/go-ping/ping"
)

type VpsHost struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	IP   string `json:"ip"`
}

type PingResult struct {
	Host    string `json:"host"`
	Name    string `json:"name"`
	Online  bool   `json:"online"`
	Latency int64  `json:"latencyMs"`
}

func getHosts() ([]VpsHost, error) {
	rows, err := db.DB.Query("SELECT id, name, ip FROM vps_hosts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []VpsHost
	for rows.Next() {
		var h VpsHost
		if err := rows.Scan(&h.ID, &h.Name, &h.IP); err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func PingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	hosts, err := getHosts()
	if err != nil {
		http.Error(w, "Failed to load hosts", http.StatusInternalServerError)
		return
	}

	if len(hosts) == 0 {
		json.NewEncoder(w).Encode([]PingResult{})
		return
	}

	results := make([]PingResult, len(hosts))
	var wg sync.WaitGroup

	for i, host := range hosts {
		wg.Add(1)
		go func(index int, h VpsHost) {
			defer wg.Done()
			pinger, err := ping.NewPinger(h.IP)
			if err != nil {
				results[index] = PingResult{Host: h.IP, Name: h.Name, Online: false}
				return
			}
			pinger.Count = 1
			pinger.Timeout = 2 * time.Second
			pinger.SetPrivileged(true)

			err = pinger.Run()
			if err != nil || pinger.Statistics().PacketsRecv == 0 {
				results[index] = PingResult{Host: h.IP, Name: h.Name, Online: false}
			} else {
				results[index] = PingResult{
					Host:    h.IP,
					Name:    h.Name,
					Online:  true,
					Latency: pinger.Statistics().AvgRtt.Milliseconds(),
				}
			}
		}(i, host)
	}

	wg.Wait()
	json.NewEncoder(w).Encode(results)
}

func VpsManagerHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		var newHost VpsHost
		if err := json.Unmarshal(body, &newHost); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if newHost.IP == "" || newHost.Name == "" {
			http.Error(w, "Name and IP are required", http.StatusBadRequest)
			return
		}

		_, err = db.DB.Exec("INSERT INTO vps_hosts (name, ip) VALUES (?, ?)", newHost.Name, newHost.IP)
		if err != nil {
			http.Error(w, "Failed to save host (maybe IP already exists?)", http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		return
	}

	if r.Method == http.MethodDelete {
		ip := r.URL.Query().Get("ip")
		if ip == "" {
			http.Error(w, "IP query parameter required", http.StatusBadRequest)
			return
		}

		_, err := db.DB.Exec("DELETE FROM vps_hosts WHERE ip = ?", ip)
		if err != nil {
			http.Error(w, "Internal error deleting host", http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
