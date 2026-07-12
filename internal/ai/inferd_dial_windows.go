//go:build windows

package ai

import (
	"context"

	inferd "github.com/3rg0n/inferd/clients/go"
)

// dialInferdOverride dials the inferd daemon at an explicit named pipe path (Windows).
func dialInferdOverride(ctx context.Context, path string) (*inferd.Client, error) {
	return inferd.DialPipe(ctx, path)
}
