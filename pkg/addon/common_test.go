// Copyright Contributors to the Open Cluster Management project

package addon

import "testing"

func TestSetTLSMinVersion(t *testing.T) {
	t.Run("valid version is set", func(t *testing.T) {
		cv := &CommonValues{}

		if err := cv.SetTLSMinVersion("VersionTLS13"); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if cv.TLSMinVersion != "VersionTLS13" {
			t.Fatalf("expected TLSMinVersion to be set, got: %q", cv.TLSMinVersion)
		}
	})

	t.Run("invalid version is rejected and left unset", func(t *testing.T) {
		cv := &CommonValues{}

		if err := cv.SetTLSMinVersion("not-a-real-version"); err == nil {
			t.Fatal("expected an error for an invalid TLS version")
		}

		if cv.TLSMinVersion != "" {
			t.Fatalf("expected TLSMinVersion to be left unset, got: %q", cv.TLSMinVersion)
		}
	})
}

func TestSetTLSCipherSuites(t *testing.T) {
	t.Run("valid cipher suites are set", func(t *testing.T) {
		cv := &CommonValues{}

		value := "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"
		if err := cv.SetTLSCipherSuites(value); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if cv.TLSCipherSuites != value {
			t.Fatalf("expected TLSCipherSuites to be set, got: %q", cv.TLSCipherSuites)
		}
	})

	t.Run("unsupported cipher suite is rejected and left unset", func(t *testing.T) {
		cv := &CommonValues{}

		if err := cv.SetTLSCipherSuites("NOT_A_REAL_CIPHER_SUITE"); err == nil {
			t.Fatal("expected an error for an unsupported cipher suite")
		}

		if cv.TLSCipherSuites != "" {
			t.Fatalf("expected TLSCipherSuites to be left unset, got: %q", cv.TLSCipherSuites)
		}
	})
}
