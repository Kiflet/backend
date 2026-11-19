package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/kiflet/backend/pkg/storage/garage"
)

// setupStorage makes Garage ready for the app: the layout, the app's key
// and bucket, its CORS and lifecycle rules (pkg/storage/garage). The deploy
// runs it before the app starts, every time; it changes only what is
// missing. It reads its settings from the environment alone, without the
// app's (config.Load would make the app's secret):
//
//	GARAGE_ADMIN_URL      the admin API, http://garage:3903 by default
//	GARAGE_ADMIN_TOKEN    or GARAGE_ADMIN_TOKEN_FILE
//	GARAGE_NODES          how many nodes the layout waits for, 1 by default
//	S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY   the app's, as the app has them
//	PUBLIC_URL            the origin allowed to use presigned URLs (CORS)
//	GARAGE_EXPIRE_DAYS    delete objects this old; unset keeps them
func setupStorage(ctx context.Context, log zerolog.Logger) error {
	token := os.Getenv("GARAGE_ADMIN_TOKEN")
	if f := os.Getenv("GARAGE_ADMIN_TOKEN_FILE"); f != "" {
		raw, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("GARAGE_ADMIN_TOKEN_FILE: %w", err)
		}
		token = strings.TrimSpace(string(raw))
	}
	if token == "" {
		return errors.New("set GARAGE_ADMIN_TOKEN (or GARAGE_ADMIN_TOKEN_FILE), the admin token Garage was started with")
	}
	nodes, err := intEnv("GARAGE_NODES", 1)
	if err != nil {
		return err
	}
	expire, err := intEnv("GARAGE_EXPIRE_DAYS", 0)
	if err != nil {
		return err
	}
	adminURL := os.Getenv("GARAGE_ADMIN_URL")
	if adminURL == "" {
		adminURL = "http://garage:3903"
	}
	key := garage.Key{ID: os.Getenv("S3_ACCESS_KEY"), Secret: os.Getenv("S3_SECRET_KEY"), Name: "kiflet-app"}
	bucket := garage.Bucket{
		Name:   os.Getenv("S3_BUCKET"),
		Grants: []garage.Grant{{KeyID: key.ID, Read: true, Write: true, Owner: true}},
		// A phone that gave up halfway through a large upload leaves parts.
		AbortUploadsDays: 1,
		ExpireDays:       expire,
	}
	if origin := strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"); origin != "" {
		bucket.CORSOrigins = []string{origin}
	}
	c := &garage.Client{URL: adminURL, Token: token, Log: func(format string, args ...any) {
		log.Info().Msgf(format, args...)
	}}
	plan := garage.Plan{Nodes: nodes, Keys: []garage.Key{key}, Buckets: []garage.Bucket{bucket}}
	if err := c.Apply(ctx, plan, 5*time.Minute); err != nil {
		return fmt.Errorf("garage: %w", err)
	}
	log.Info().Str("bucket", bucket.Name).Msg("storage ready")
	return nil
}

func intEnv(name string, def int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: %q isn't a whole number", name, v)
	}
	return n, nil
}
