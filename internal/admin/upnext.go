package admin

import (
	"context"
	"encoding/json"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
)

// upNext returns the private "up next" list (the next tracks of the
// Spotify queue, written by queue prefetch), or nil when there is none or
// Redis fails: the overview never fails because of it.
func (a *API) upNext(ctx context.Context) *domain.UpNext {
	if a.Redis == nil {
		return nil
	}
	return readUpNext(ctx, func(ctx context.Context) ([]byte, error) {
		return a.Redis.Get(ctx, redisx.KeyUpNext).Bytes()
	})
}

func readUpNext(ctx context.Context, get func(context.Context) ([]byte, error)) *domain.UpNext {
	b, err := get(ctx)
	if err != nil || len(b) == 0 {
		return nil // redis.Nil (none) or an error
	}
	var up domain.UpNext
	if json.Unmarshal(b, &up) != nil || len(up.Items) == 0 {
		return nil
	}
	return &up
}
