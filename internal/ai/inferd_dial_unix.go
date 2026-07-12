//go:build unix

package ai

import (
	"context"
	"net"

	inferd "github.com/3rg0n/inferd/clients/go"
)

// dialInferdOverride dials the inferd daemon at an explicit unix socket path.
func dialInferdOverride(ctx context.Context, path string) (*inferd.Client, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	return inferd.New(conn), nil
}
