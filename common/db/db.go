package db

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func ValidateURL(raw string) error {
	if raw == "" {
		return errors.New("DATABASE_URL is required")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("not a valid URL")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("scheme must be postgres or postgresql, got %q", u.Scheme)
	}
	if u.User.Username() == "" {
		return errors.New("user is missing")
	}
	if _, ok := u.User.Password(); !ok {
		return errors.New("password is missing")
	}
	if u.Hostname() == "" {
		return errors.New("host is missing")
	}
	if strings.Trim(u.Path, "/") == "" {
		return errors.New("database name is missing")
	}
	if mode := u.Query().Get("sslmode"); mode != "verify-full" {
		return fmt.Errorf("sslmode must be verify-full, got %q", mode)
	}
	return nil
}
