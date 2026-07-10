#!/bin/bash

# Make a directory for the profiles
mkdir -p profiles
echo "Starting pprof polling... (Press Ctrl+C to stop)"

while true; do
  TIMESTAMP=$(date +%s)
  
  # Grab a 10-second CPU profile
  echo "[$TIMESTAMP] Capturing 10s CPU profile..."
  curl -s "https://canvas.x1nx3r.dev/debug/pprof/profile?seconds=10" > "profiles/cpu_${TIMESTAMP}.prof"
  
  # Grab a heap profile
  echo "[$TIMESTAMP] Capturing heap profile..."
  curl -s "https://canvas.x1nx3r.dev/debug/pprof/heap" > "profiles/heap_${TIMESTAMP}.prof"
  
  # Grab goroutine count/state
  echo "[$TIMESTAMP] Capturing goroutine profile..."
  curl -s "https://canvas.x1nx3r.dev/debug/pprof/goroutine?debug=1" > "profiles/goroutines_${TIMESTAMP}.txt"
  
  # Sleep a bit before the next poll (CPU profile already blocked for 10s)
  sleep 5
done
