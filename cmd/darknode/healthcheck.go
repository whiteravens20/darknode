// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/whiteravens20/darknode/internal/config"
)

// healthcheck asks the server running in this container whether it is well.
// The image has no shell and no curl, so the binary does it itself.
func healthcheck(ctx context.Context, getenv func(string) string) error {
	port := config.Config{Listen: value(getenv("DARKNODE_LISTEN"), ":8080")}.ListenPort()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	url := "http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the server answered %s", resp.Status)
	}
	return nil
}

func value(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
