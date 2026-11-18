// Package secrets reads secrets from AWS Secrets Manager.
package secrets

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// LoadBytes reads a base64 secret, retrying with backoff until it works or ctx
// ends (R6: the service starts even if MiniStack is not ready yet).
func LoadBytes(ctx context.Context, sm *secretsmanager.Client, id string, log *slog.Logger) ([]byte, error) {
	for wait := 500 * time.Millisecond; ; wait = min(wait*2, 10*time.Second) {
		out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &id})
		if err == nil && out.SecretString != nil {
			b, err := base64.StdEncoding.DecodeString(*out.SecretString)
			if err != nil {
				return nil, fmt.Errorf("secret %s is not base64: %w", id, err)
			}
			return b, nil
		}
		log.Warn("secret read failed, retrying", "secretId", id, "err", err, "wait", wait.String())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}
