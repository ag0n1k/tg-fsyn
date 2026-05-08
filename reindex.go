package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// Reindexer triggers a media reindex by invoking synoindex on the host.
type Reindexer interface {
	Reindex() (string, error)
}

type synoindexRunner struct {
	bin   string
	paths []string
}

func NewSynoindexRunner(bin string, paths []string) *synoindexRunner {
	return &synoindexRunner{bin: bin, paths: paths}
}

func (r *synoindexRunner) Reindex() (string, error) {
	indexed := make([]string, 0, len(r.paths))
	var firstErr error
	for _, p := range r.paths {
		start := time.Now()
		log.Printf("synoindex -A %s starting", p)
		cmd := exec.Command(r.bin, "-A", p)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("synoindex -A %s failed in %v: %v (%s)", p, time.Since(start), err, strings.TrimSpace(string(out)))
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w (%s)", p, err, strings.TrimSpace(string(out)))
			}
			continue
		}
		log.Printf("synoindex -A %s ok in %v", p, time.Since(start))
		indexed = append(indexed, p)
	}

	if firstErr != nil && len(indexed) == 0 {
		return "", firstErr
	}
	result := strings.Join(indexed, ", ")
	if firstErr != nil {
		return result, fmt.Errorf("partial failure: %w", firstErr)
	}
	return result, nil
}
