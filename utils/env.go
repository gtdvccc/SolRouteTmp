package utils

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var loadOnce sync.Once

// LoadEnv loads environment variables from a .env file in project root if present.
// Existing environment variables are not overwritten.
func LoadEnv() {
    loadOnce.Do(func() {
        // Try to locate .env starting from current working directory
        cwd, err := os.Getwd()
        if err != nil {
            return
        }

        // Walk up to project root at most 3 levels
        candidate := ""
        dir := cwd
        for i := 0; i < 3; i++ {
            path := filepath.Join(dir, ".env")
            if st, err := os.Stat(path); err == nil && !st.IsDir() {
                candidate = path
                break
            }
            parent := filepath.Dir(dir)
            if parent == dir {
                break
            }
            dir = parent
        }

        if candidate == "" {
            return
        }

        f, err := os.Open(candidate)
        if err != nil {
            log.Printf("warning: cannot open .env: %v", err)
            return
        }
        defer f.Close()

        scanner := bufio.NewScanner(f)
        for scanner.Scan() {
            line := strings.TrimSpace(scanner.Text())
            if line == "" || strings.HasPrefix(line, "#") {
                continue
            }
            // Support export prefix
            if strings.HasPrefix(line, "export ") {
                line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
            }
            kv := strings.SplitN(line, "=", 2)
            if len(kv) != 2 {
                continue
            }
            key := strings.TrimSpace(kv[0])
            val := strings.TrimSpace(kv[1])
            // Remove surrounding quotes if any
            if len(val) >= 2 {
                if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
                    (strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
                    val = val[1 : len(val)-1]
                }
            }
            if _, exists := os.LookupEnv(key); !exists {
                _ = os.Setenv(key, val)
            }
        }
    })
}


