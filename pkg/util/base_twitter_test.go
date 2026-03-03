package util

import "testing"

func TestGetTwitterStatusID(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "x.com status url",
			url:  "https://x.com/OpenAI/status/1234567890123456789",
			want: "1234567890123456789",
		},
		{
			name: "twitter.com status url with query",
			url:  "https://twitter.com/someone/status/9876543210?s=20",
			want: "9876543210",
		},
		{
			name: "url without scheme",
			url:  "x.com/i/status/1122334455",
			want: "1122334455",
		},
		{
			name: "invalid non status url",
			url:  "https://x.com/home",
			want: "",
		},
		{
			name: "invalid host",
			url:  "https://example.com/user/status/112233",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GetTwitterStatusID(tc.url)
			if got != tc.want {
				t.Fatalf("GetTwitterStatusID(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}
