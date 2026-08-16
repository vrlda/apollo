---
description: Debugging Go services — race conditions, goroutine leaks, profiling, pprof, structured logging
---

# Go Debugging Skill

## Race Conditions
Always run tests with: `go test -race ./...`
Common causes: shared maps without mutex, goroutines reading/writing same variable.

```go
// Safe map access pattern
var mu sync.RWMutex
mu.RLock()
val := myMap[key]
mu.RUnlock()
```

## Goroutine Leak Detection
```go
// Import in test file
import "github.com/uber-go/goleak"

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

## pprof Profiling
Add to main.go for CPU + mem profiling endpoints:
```go
import _ "net/http/pprof"
// Then: go tool pprof http://localhost:6060/debug/pprof/heap
```

Common pprof commands:
```
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/goroutine
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/cpu?seconds=30
```

## Structured Logging (slog, Go 1.21+)
```go
import "log/slog"
slog.Info("request received", "method", r.Method, "path", r.URL.Path, "latency_ms", ms)
slog.Error("database error", "err", err, "query", q)
```

## JSON Decode Gotchas
- `json.Decoder` reads lazily — always close body with `defer r.Body.Close()`
- Unknown fields are silently ignored unless you use `decoder.DisallowUnknownFields()`
- `time.Time` marshals as RFC3339 by default

## HTTP Timeout Pattern
```go
client := &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
        TLSHandshakeTimeout: 5 * time.Second,
        ResponseHeaderTimeout: 10 * time.Second,
    },
}
```

## Error Wrapping
```go
if err != nil {
    return fmt.Errorf("getting user %d: %w", id, err)
}
// Unwrap: errors.Is(err, ErrNotFound), errors.As(err, &myErr)
```

## go vet Checks
`go vet ./...` catches: printf format mismatches, unreachable code, suspicious composite literals, incorrect mutex copies.

## Benchmark Pattern
```go
func BenchmarkMyFunc(b *testing.B) {
    for b.Loop() { // Go 1.24+ — or: for i := 0; i < b.N; i++
        MyFunc()
    }
}
// Run: go test -bench=. -benchmem ./...
```
