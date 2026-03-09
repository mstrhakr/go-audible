package audible

import "testing"

func TestIsRetryableCDNError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "invalid audio format is retryable",
			err: &cdnTextError{
				peekBytes: 42,
				message:   "File Assembly error: Invalid Audio Format.",
			},
			want: true,
		},
		{
			name: "other cdn text error is not retryable",
			err: &cdnTextError{
				peekBytes: 18,
				message:   "Access denied",
			},
			want: false,
		},
		{
			name: "non-cdn error is not retryable",
			err:  ErrNotAuthenticated,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableCDNError(tc.err); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
