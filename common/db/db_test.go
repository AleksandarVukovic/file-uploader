package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateURL(t *testing.T) {
	const valid = "postgres://user:pass@db:5432/files?sslmode=verify-full&sslrootcert=/certs/ca.pem"

	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "accepts a complete postgres url", url: valid},
		{name: "accepts the postgresql scheme", url: "postgresql://user:pass@db:5432/files?sslmode=verify-full"},
		{name: "rejects an empty url", url: "", wantErr: "DATABASE_URL is required"},
		{name: "rejects an unparsable url", url: "postgres://user:pass@db:notaport/files", wantErr: "not a valid URL"},
		{name: "rejects another scheme", url: "mysql://user:pass@db:3306/files?sslmode=verify-full", wantErr: "scheme must be postgres or postgresql"},
		{name: "rejects a missing user", url: "postgres://:pass@db:5432/files?sslmode=verify-full", wantErr: "user is missing"},
		{name: "rejects a missing password", url: "postgres://user@db:5432/files?sslmode=verify-full", wantErr: "password is missing"},
		{name: "rejects a missing host", url: "postgres://user:pass@:5432/files?sslmode=verify-full", wantErr: "host is missing"},
		{name: "rejects a missing database name", url: "postgres://user:pass@db:5432/?sslmode=verify-full", wantErr: "database name is missing"},
		{name: "rejects a missing sslmode", url: "postgres://user:pass@db:5432/files", wantErr: "sslmode must be verify-full"},
		{name: "rejects a weaker sslmode", url: "postgres://user:pass@db:5432/files?sslmode=require", wantErr: "sslmode must be verify-full"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url)

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
