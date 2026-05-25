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
	defaultAppName          = "Go Starter"
	defaultAppEnv           = "development"
	defaultAppURL           = "http://127.0.0.1:8080"
	defaultHTTPAddr         = ":8080"
	defaultShutdownTimeout  = 5 * time.Second
	defaultSessionCookie    = "go_starter_session"
	defaultSessionTTL       = 30 * 24 * time.Hour
	defaultAPIAccessTTL     = 10 * time.Minute
	defaultAPIRefreshTTL    = 30 * 24 * time.Hour
	defaultAPIRefreshCookie = "go_starter_api_refresh"
	defaultDBMaxConns       = int32(4)
	defaultDBConnLifetime   = 30 * time.Minute
	defaultDBConnIdleTime   = 5 * time.Minute
	defaultStorageDriver    = "local"
	defaultLocalStorageDir  = "media"
	defaultLocalPublicPath  = "/media"
	defaultR2Region         = "auto"
)

type Config struct {
	AppName         string
	AppEnv          string
	AppURL          string
	GoogleTagID     string
	Auth            AuthConfig
	HTTPAddr        string
	ShutdownTimeout time.Duration
	Database        DatabaseConfig
	Storage         StorageConfig
	R2              R2Config
}

type StorageConfig struct {
	Driver          string
	LocalDir        string
	LocalPublicPath string
}

type R2Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	PublicBaseURL   string
}

type AuthConfig struct {
	SessionCookieName string
	SessionTTL        time.Duration
	CookieSecure      bool
	Social            SocialAuthConfig
	API               APIAuthConfig
}

type SocialAuthConfig struct {
	Google OAuthClientConfig
	GitHub OAuthClientConfig
}

type OAuthClientConfig struct {
	ClientID     string
	ClientSecret string
}

type APIAuthConfig struct {
	AccessTokenSecret string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	RefreshCookieName string
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		AppName: defaultAppName,
		AppEnv:  defaultAppEnv,
		AppURL:  defaultAppURL,
		Auth: AuthConfig{
			SessionCookieName: defaultSessionCookie,
			SessionTTL:        defaultSessionTTL,
			API: APIAuthConfig{
				AccessTokenTTL:    defaultAPIAccessTTL,
				RefreshTokenTTL:   defaultAPIRefreshTTL,
				RefreshCookieName: defaultAPIRefreshCookie,
			},
		},
		HTTPAddr:        defaultHTTPAddr,
		ShutdownTimeout: defaultShutdownTimeout,
		Database: DatabaseConfig{
			MaxConns:        defaultDBMaxConns,
			MaxConnLifetime: defaultDBConnLifetime,
			MaxConnIdleTime: defaultDBConnIdleTime,
		},
		Storage: StorageConfig{
			Driver:          defaultStorageDriver,
			LocalDir:        defaultLocalStorageDir,
			LocalPublicPath: defaultLocalPublicPath,
		},
		R2: R2Config{
			Region: defaultR2Region,
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
	if v := strings.TrimSpace(os.Getenv("GOOGLE_TAG_ID")); v != "" {
		cfg.GoogleTagID = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_SESSION_COOKIE_NAME")); v != "" {
		cfg.Auth.SessionCookieName = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_SESSION_TTL")); v != "" {
		d, err := parseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse AUTH_SESSION_TTL: %w", err)
		}
		cfg.Auth.SessionTTL = d
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_COOKIE_SECURE")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse AUTH_COOKIE_SECURE: %w", err)
		}
		cfg.Auth.CookieSecure = b
	}
	cfg.Auth.Social.Google.ClientID = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	cfg.Auth.Social.Google.ClientSecret = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET"))
	cfg.Auth.Social.GitHub.ClientID = strings.TrimSpace(os.Getenv("GITHUB_CLIENT_ID"))
	cfg.Auth.Social.GitHub.ClientSecret = strings.TrimSpace(os.Getenv("GITHUB_CLIENT_SECRET"))
	cfg.Auth.API.AccessTokenSecret = strings.TrimSpace(os.Getenv("API_ACCESS_TOKEN_SECRET"))
	if v := strings.TrimSpace(os.Getenv("API_ACCESS_TOKEN_TTL")); v != "" {
		d, err := parseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse API_ACCESS_TOKEN_TTL: %w", err)
		}
		cfg.Auth.API.AccessTokenTTL = d
	}
	if v := strings.TrimSpace(os.Getenv("API_REFRESH_TOKEN_TTL")); v != "" {
		d, err := parseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse API_REFRESH_TOKEN_TTL: %w", err)
		}
		cfg.Auth.API.RefreshTokenTTL = d
	}
	if v := strings.TrimSpace(os.Getenv("API_REFRESH_COOKIE_NAME")); v != "" {
		cfg.Auth.API.RefreshCookieName = v
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
	if strings.TrimSpace(cfg.Auth.SessionCookieName) == "" {
		cfg.Auth.SessionCookieName = defaultSessionCookie
	}
	if cfg.Auth.SessionTTL <= 0 {
		cfg.Auth.SessionTTL = defaultSessionTTL
	}
	if cfg.Auth.API.AccessTokenTTL <= 0 {
		cfg.Auth.API.AccessTokenTTL = defaultAPIAccessTTL
	}
	if cfg.Auth.API.RefreshTokenTTL <= 0 {
		cfg.Auth.API.RefreshTokenTTL = defaultAPIRefreshTTL
	}
	if strings.TrimSpace(cfg.Auth.API.RefreshCookieName) == "" {
		cfg.Auth.API.RefreshCookieName = defaultAPIRefreshCookie
	}

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

	if v := strings.TrimSpace(os.Getenv("STORAGE_DRIVER")); v != "" {
		cfg.Storage.Driver = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_DIR")); v != "" {
		cfg.Storage.LocalDir = v
	}
	if v := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_PUBLIC_PATH")); v != "" {
		cfg.Storage.LocalPublicPath = v
	}
	if !strings.HasPrefix(cfg.Storage.LocalPublicPath, "/") {
		cfg.Storage.LocalPublicPath = "/" + cfg.Storage.LocalPublicPath
	}

	if v := strings.TrimSpace(os.Getenv("R2_ENDPOINT")); v != "" {
		cfg.R2.Endpoint = v
	}
	if v := strings.TrimSpace(os.Getenv("R2_REGION")); v != "" {
		cfg.R2.Region = v
	}
	cfg.R2.AccessKeyID = strings.TrimSpace(os.Getenv("R2_ACCESS_KEY_ID"))
	cfg.R2.SecretAccessKey = strings.TrimSpace(os.Getenv("R2_SECRET_ACCESS_KEY"))
	cfg.R2.Bucket = strings.TrimSpace(os.Getenv("R2_BUCKET"))
	cfg.R2.PublicBaseURL = strings.TrimSpace(os.Getenv("R2_PUBLIC_BASE_URL"))

	switch cfg.Storage.Driver {
	case "local":
		if strings.TrimSpace(cfg.Storage.LocalDir) == "" {
			return Config{}, errors.New("LOCAL_STORAGE_DIR must not be empty when STORAGE_DRIVER=local")
		}
	case "r2":
		missing := []string{}
		if cfg.R2.Endpoint == "" {
			missing = append(missing, "R2_ENDPOINT")
		}
		if cfg.R2.AccessKeyID == "" {
			missing = append(missing, "R2_ACCESS_KEY_ID")
		}
		if cfg.R2.SecretAccessKey == "" {
			missing = append(missing, "R2_SECRET_ACCESS_KEY")
		}
		if cfg.R2.Bucket == "" {
			missing = append(missing, "R2_BUCKET")
		}
		if cfg.R2.PublicBaseURL == "" {
			missing = append(missing, "R2_PUBLIC_BASE_URL")
		}
		if len(missing) > 0 {
			return Config{}, fmt.Errorf("STORAGE_DRIVER=r2 requires: %s", strings.Join(missing, ", "))
		}
	default:
		return Config{}, fmt.Errorf("STORAGE_DRIVER must be either local or r2, got %q", cfg.Storage.Driver)
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
