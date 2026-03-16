package audible

import (
	"strings"
	"testing"
)

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

func TestParseLicenseDenialReasons(t *testing.T) {
	reasons := parseLicenseDenialReasons([]any{
		map[string]any{
			"message":         "Customer is not part of any plans",
			"rejectionReason": "RequesterEligibility",
			"validationType":  "Membership",
		},
		map[string]any{
			"message":         "Asin is not eligible",
			"rejectionReason": "ContentEligibility",
			"validationType":  "AYCL",
		},
	})

	if len(reasons) != 2 {
		t.Fatalf("got %d reasons, want 2", len(reasons))
	}

	if reasons[0].ValidationType != "Membership" {
		t.Fatalf("got validation type %q, want Membership", reasons[0].ValidationType)
	}
}

func TestLicenseDeniedError_Error(t *testing.T) {
	err := (&LicenseDeniedError{
		ASIN:       "B002V19RO6",
		StatusCode: "Denied",
		Message:    "License not granted",
		Reasons: []LicenseDenialReason{
			{
				Message:         "Customer is not part of any plans",
				RejectionReason: "RequesterEligibility",
				ValidationType:  "Membership",
			},
		},
	}).Error()

	checks := []string{
		"license denied for ASIN B002V19RO6",
		"status=Denied",
		"License not granted",
		"Customer is not part of any plans",
		"RequesterEligibility/Membership",
	}

	for _, needle := range checks {
		if !strings.Contains(err, needle) {
			t.Fatalf("error %q does not contain %q", err, needle)
		}
	}
}
