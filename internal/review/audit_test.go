package review

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		leaked string // must NOT survive
		keep   string // must survive (readability of the audit trail)
	}{
		{
			name:   "bearer auth header",
			in:     `curl -H "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.secret" https://api.example.com`,
			leaked: "eyJhbGciOiJIUzI1NiJ9.secret",
			keep:   "https://api.example.com",
		},
		{
			name:   "x-api-key header",
			in:     `curl -H "x-api-key: abcd1234efgh5678" https://api.example.com`,
			leaked: "abcd1234efgh5678",
			keep:   "curl",
		},
		{
			name:   "password flag",
			in:     `mysql --user=root --password=hunter2ismypass -e 'select 1'`,
			leaked: "hunter2ismypass",
			keep:   "mysql",
		},
		{
			name:   "token flag with space",
			in:     `gh auth login --token ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAA`,
			leaked: "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			keep:   "gh auth login",
		},
		{
			name:   "env assignment",
			in:     `AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY aws s3 ls`,
			leaked: "wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY",
			keep:   "aws s3 ls",
		},
		{
			name:   "github token prefix anywhere",
			in:     `echo ghp_1234567890abcdefghijklmnop > /tmp/t`,
			leaked: "1234567890abcdefghijklmnop",
			keep:   "echo",
		},
		{
			name:   "slack token prefix",
			in:     `curl -d token=xoxb-123456789012-abcdefghij https://slack.com/api/x`,
			leaked: "xoxb-123456789012-abcdefghij",
			keep:   "slack.com",
		},
		{
			name:   "aws access key id",
			in:     `export KEY=AKIAIOSFODNN7EXAMPLE`,
			leaked: "AKIAIOSFODNN7EXAMPLE",
			keep:   "export",
		},
		{
			name:   "private key block",
			in:     "ssh-add <<'K'\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAxxxxSECRETMATERIALxxxx\n-----END RSA PRIVATE KEY-----\nK",
			leaked: "MIIEowIBAAKCAQEAxxxxSECRETMATERIALxxxx",
			keep:   "ssh-add",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.in)
			if strings.Contains(got, tc.leaked) {
				t.Errorf("secret survived redaction:\n in:  %s\n out: %s\n leaked: %s", tc.in, got, tc.leaked)
			}
			if !strings.Contains(got, tc.keep) {
				t.Errorf("redaction destroyed useful context %q:\n out: %s", tc.keep, got)
			}
			if !strings.Contains(got, redactedPlaceholder) {
				t.Errorf("expected a redaction marker in: %s", got)
			}
		})
	}
}

func TestRedactSecrets_LeavesOrdinaryCommandsAlone(t *testing.T) {
	for _, cmd := range []string{
		`go test ./...`,
		`rm -rf ./dist`,
		`git commit -m "fix the token parser"`, // "token" as prose, not an assignment
		`curl -sL https://registry.npmjs.org/left-pad`,
	} {
		if got := redactSecrets(cmd); got != cmd {
			t.Errorf("redaction altered an ordinary command:\n in:  %s\n out: %s", cmd, got)
		}
	}
}
