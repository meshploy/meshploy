package templates

import (
	"strings"
	"testing"
	"time"
)

// The rejection sampler must terminate for every alphabet length, including the
// powers of two that divide 256. base64url has 64 characters: the limit is then
// 256, which wrapped to 0 as a byte and rejected every draw forever, so
// deploying any template with a secret64 variable spun a core until restart.
//
// Each case runs in its own goroutine so a regression fails this test in a
// second rather than hanging the whole suite until the -timeout kills it.
func TestRandFromAlphabetTerminates(t *testing.T) {
	sizes := []int{1, 2, 4, 8, 16, 32, 62, 64, 100, 128, 255, 256}
	for _, size := range sizes {
		alphabet := buildAlphabet(size)
		t.Run(alphabet[:1]+"_len"+itoa(size), func(t *testing.T) {
			done := make(chan string, 1)
			go func() { done <- randFromAlphabet(alphabet, 64) }()
			select {
			case got := <-done:
				if len(got) != 64 {
					t.Fatalf("length = %d, want 64", len(got))
				}
				for _, r := range got {
					if !strings.ContainsRune(alphabet, r) {
						t.Fatalf("character %q is not in the alphabet", r)
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("did not terminate — the rejection sampler is rejecting every draw")
			}
		})
	}
}

// Every generator named in a manifest must produce a value, at the documented
// length. secret64 is the one that hung.
func TestGenerateValueProducesEveryGenerator(t *testing.T) {
	cases := []struct {
		name    string
		wantLen int
	}{
		{genPassword, 24},
		{genSecret64, 64},
		{genHex32, 32},
		{genUUID, 36},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan string, 1)
			go func() {
				v, err := generateValue(tc.name)
				if err != nil {
					t.Errorf("generateValue(%q): %v", tc.name, err)
				}
				done <- v
			}()
			select {
			case got := <-done:
				if len(got) != tc.wantLen {
					t.Errorf("len = %d, want %d", len(got), tc.wantLen)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("generator %q did not terminate", tc.name)
			}
		})
	}
}

func buildAlphabet(size int) string {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return string(b)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
