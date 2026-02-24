package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAppName         = "Go Starter"
	defaultAppEnv          = "development"
	defaultAppURL          = "http://127.0.0.1:8080"
	defaultHTTPAddr        = ":8080"
	defaultShutdownTimeout = 5 * time.Second
	defaultDBMaxConns      = int32(4)
	defaultDBConnLifetime  = 30 * time.Minute
	defaultDBConnIdleTime  = 5 * time.Minute
)

type Config struct {
	AppName         string
	AppEnv          string
	AppURL          string
	HTTPAddr        string
	ShutdownTimeout time.Duration
	Database        DatabaseConfig
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		AppName:         defaultAppName,
		AppEnv:          defaultAppEnv,
		AppURL:          defaultAppURL,
		HTTPAddr:        defaultHTTPAddr,
		ShutdownTimeout: defaultShutdownTimeout,
		Database: DatabaseConfig{
			MaxConns:        defaultDBMaxConns,
			MaxConnLifetime: defaultDBConnLifetime,
			MaxConnIdleTime: defaultDBConnIdleTime,
		},
	}

	if v := strings.TrimSpace(os.Getenv("APP_NAME")); v != "" {
		cfg.AppName = v
	}
	if v := strings.TrimSpace(os.Getenv("APP_ENV")); v != "" {
		cfg.AppEnv = v
	}
	if v := strings.TrimSpace(os.Getenv("APP_URL")); v != "" {
		cfg.AppURL = v
	}
	if v := strings.TrimSpace(os.Getenv("HTTP_ADDR")); v != "" {
		cfg.HTTPAddr = v
	}
	if v := strings.TrimSpace(os.Getenv("SHUTDOWN_TIMEOUT")); v != "" {
		d, err := parseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse SHUTDOWN_TIMEOUT: %w", err)
		}
		cfg.ShutdownTimeout = d
	}

	appURL, err := url.Parse(strings.TrimSpace(cfg.AppURL))
	if err != nil || appURL.Scheme == "" || appURL.Host == "" {
		return Config{}, errors.New("APP_URL must be a valid absolute URL")
	}
	if strings.EqualFold(cfg.AppEnv, "production") && !strings.EqualFold(appURL.Scheme, "https") {
		return Config{}, errors.New("APP_URL must use https in production")
	}
	cfg.AppURL = appURL.String()

	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	cfg.Database.URL = dbURL

	if v := strings.TrimSpace(os.Getenv("DATABASE_MAX_CONNS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, errors.New("DATABASE_MAX_CONNS must be a positive integer")
		}
		cfg.Database.MaxConns = int32(n)
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_MAX_CONN_LIFETIME")); v != "" {
		d, err := parseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse DATABASE_MAX_CONN_LIFETIME: %w", err)
		}
		cfg.Database.MaxConnLifetime = d
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_MAX_CONN_IDLE_TIME")); v != "" {
		d, err := parseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse DATABASE_MAX_CONN_IDLE_TIME: %w", err)
		}
		cfg.Database.MaxConnIdleTime = d
	}

	return cfg, nil
}

func parseDuration(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, errors.New("empty duration")
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d, nil
	}
	seconds, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds) * time.Second, nil
}
