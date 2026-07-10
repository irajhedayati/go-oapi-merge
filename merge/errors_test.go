package merge

import (
	"errors"
	"testing"
)

func TestMergeError_Error(t *testing.T) {
	cause := errors.New("underlying failure")

	tests := []struct {
		name string
		err  *MergeError
		want string
	}{
		{
			name: "message only",
			err:  &MergeError{Message: "boom"},
			want: "boom",
		},
		{
			name: "with file",
			err:  &MergeError{Message: "boom", File: "api.yaml"},
			want: "boom (in api.yaml)",
		},
		{
			name: "with file and path",
			err:  &MergeError{Message: "boom", File: "api.yaml", Path: "/paths/~1users"},
			want: "boom (in api.yaml at /paths/~1users)",
		},
		{
			name: "cause is not printed inline",
			err:  &MergeError{Message: "boom", File: "api.yaml", Cause: cause},
			want: "boom (in api.yaml)",
		},
		{
			name: "path without file is suppressed",
			err:  &MergeError{Message: "boom", Path: "/paths"},
			want: "boom",
		},
		{
			name: "empty message",
			err:  &MergeError{File: "api.yaml"},
			want: " (in api.yaml)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeError_Unwrap(t *testing.T) {
	cause := errors.New("boom")

	tests := []struct {
		name string
		err  *MergeError
		want error
	}{
		{"nil cause returns nil", &MergeError{Message: "x"}, nil},
		{"non-nil cause is returned", &MergeError{Message: "x", Cause: cause}, cause},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Unwrap(); got != tt.want {
				t.Errorf("Unwrap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeError_ErrorsIs(t *testing.T) {
	cause := errors.New("sentinel")
	wrapped := &MergeError{Message: "boom", Cause: cause}

	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is failed to find wrapped cause through MergeError")
	}
	if errors.Is(wrapped, errors.New("other")) {
		t.Error("errors.Is matched an unrelated error")
	}
}
