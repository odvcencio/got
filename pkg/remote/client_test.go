package remote

import "testing"

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantBase   string
		wantOwner  string
		wantRepo   string
		shouldFail bool
	}{
		{
			name:      "canonical got path",
			in:        "https://example.com/got/alice/proj",
			wantBase:  "https://example.com/got/alice/proj",
			wantOwner: "alice",
			wantRepo:  "proj",
		},
		{
			name:      "plain owner repo path",
			in:        "https://example.com/alice/proj",
			wantBase:  "https://example.com/got/alice/proj",
			wantOwner: "alice",
			wantRepo:  "proj",
		},
		{
			name:      "api prefix with got path",
			in:        "https://example.com/api/v1/got/alice/proj",
			wantBase:  "https://example.com/api/v1/got/alice/proj",
			wantOwner: "alice",
			wantRepo:  "proj",
		},
		{
			name:       "invalid",
			in:         "alice/proj",
			shouldFail: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := ParseEndpoint(tc.in)
			if tc.shouldFail {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEndpoint: %v", err)
			}
			if ep.BaseURL != tc.wantBase {
				t.Fatalf("BaseURL = %q, want %q", ep.BaseURL, tc.wantBase)
			}
			if ep.Owner != tc.wantOwner {
				t.Fatalf("Owner = %q, want %q", ep.Owner, tc.wantOwner)
			}
			if ep.Repo != tc.wantRepo {
				t.Fatalf("Repo = %q, want %q", ep.Repo, tc.wantRepo)
			}
		})
	}
}
