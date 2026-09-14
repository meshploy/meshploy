package service

import (
	"errors"
	"strings"
	"testing"
)

// A build's requests and limits must be quantities the cluster accepts, with
// no limit below its request, before they are saved.
func TestValidateBuilderResources(t *testing.T) {
	ok := [][4]string{
		{"", "", "", ""},
		{"500m", "2", "1Gi", "4Gi"},
		{"", "", "8Gi", ""}, // the default memory limit rises to the request
		{"", "1000m", "", "1Gi"},
	}
	for _, c := range ok {
		if err := validateBuilderResources(c[0], c[1], c[2], c[3]); err != nil {
			t.Errorf("%q: %v", c, err)
		}
	}
	bad := [][4]string{
		{"1 core", "", "", ""},
		{"", "", "1 GB", ""},
		{"", "lots", "", ""},
		{"2", "1", "", ""},
		{"", "", "2Gi", "1Gi"},
		{"", "500m", "", ""}, // below the default 1000m request
	}
	for _, c := range bad {
		err := validateBuilderResources(c[0], c[1], c[2], c[3])
		if !errors.Is(err, ErrInvalidResources) {
			t.Errorf("%q: got %v, want ErrInvalidResources", c, err)
		}
	}
}

func TestBuildOutOfMemoryMessageNamesTheLimit(t *testing.T) {
	for req, want := range map[string]string{"": "4Gi", "8Gi": "8Gi"} {
		if msg := buildOutOfMemoryMessage(req, ""); !strings.Contains(msg, want+" limit") {
			t.Errorf("request %q: %s", req, msg)
		}
	}
	if msg := buildOutOfMemoryMessage("", "6Gi"); !strings.Contains(msg, "6Gi limit") {
		t.Errorf("explicit limit: %s", msg)
	}
}
