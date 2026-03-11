// SPDX-License-Identifier:Apache-2.0

package tlsconfig

import (
	"crypto/tls"
	"testing"
)

func Test_curveNamesToIDs(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []tls.CurveID
		wantErr bool
	}{
		{
			name:  "empty returns nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "IANA names",
			input: []string{"X25519", "secp256r1", "secp384r1"},
			want:  []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
		},
		{
			name:  "PQC curve",
			input: []string{"X25519MLKEM768"},
			want:  []tls.CurveID{tls.X25519MLKEM768},
		},
		{
			name:  "all supported curves",
			input: []string{"X25519", "secp256r1", "secp384r1", "secp521r1", "X25519MLKEM768"},
			want:  []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384, tls.CurveP521, tls.X25519MLKEM768},
		},
		{
			name:    "unknown curve returns error",
			input:   []string{"X25519", "FakeCurve"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := curveNamesToIDs(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CurveNamesToIDs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Fatalf("CurveNamesToIDs() returned %d items, want %d", len(got), len(tt.want))
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("CurveNamesToIDs()[%d] = %v, want %v", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func Test_parseCurvePreferences(t *testing.T) {
	got, err := parseCurvePreferences("")
	if err != nil || got != nil {
		t.Fatalf("ParseCurvePreferences(\"\") = %v, %v; want nil, nil", got, err)
	}

	got, err = parseCurvePreferences("X25519, secp256r1, X25519MLKEM768")
	if err != nil {
		t.Fatalf("ParseCurvePreferences() error = %v", err)
	}
	want := []tls.CurveID{tls.X25519, tls.CurveP256, tls.X25519MLKEM768}
	if len(got) != len(want) {
		t.Fatalf("ParseCurvePreferences() returned %d items, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("ParseCurvePreferences()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func Test_parseTLSVersion(t *testing.T) {
	v, err := parseTLSVersion("")
	if err != nil || v != tls.VersionTLS13 {
		t.Fatalf("ParseTLSVersion(\"\") = %v, %v; want %v, nil", v, err, tls.VersionTLS13)
	}

	v, err = parseTLSVersion("VersionTLS13")
	if err != nil || v != tls.VersionTLS13 {
		t.Fatalf("ParseTLSVersion(\"VersionTLS13\") = %v, %v; want %v, nil", v, err, tls.VersionTLS13)
	}

	_, err = parseTLSVersion("VersionTLS99")
	if err == nil {
		t.Fatal("ParseTLSVersion(\"VersionTLS99\") expected error, got nil")
	}
}

func TestTLSOptFor(t *testing.T) {
	t.Run("applies cipher and curve settings for TLS 1.2", func(t *testing.T) {
		opt, err := TLSOptFor("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "X25519,X25519MLKEM768", "VersionTLS12")
		if err != nil {
			t.Fatalf("TLSOptFor() error = %v", err)
		}
		cfg := &tls.Config{}
		opt(cfg)

		if len(cfg.CipherSuites) != 1 {
			t.Errorf("CipherSuites = %v, want 1 entry", cfg.CipherSuites)
		}
		if len(cfg.CurvePreferences) != 2 {
			t.Errorf("CurvePreferences = %v, want 2 entries", cfg.CurvePreferences)
		}
		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %v, want %v", cfg.MinVersion, tls.VersionTLS12)
		}
	})

	t.Run("rejects CipherSuites with TLS 1.3", func(t *testing.T) {
		_, err := TLSOptFor("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "X25519", "VersionTLS13")
		if err == nil {
			t.Fatal("expected error when configuring ciphers with TLS 1.3")
		}
	})

	t.Run("TLS 1.3 without ciphers is fine", func(t *testing.T) {
		opt, err := TLSOptFor("", "X25519", "VersionTLS13")
		if err != nil {
			t.Fatalf("TLSOptFor() error = %v", err)
		}
		cfg := &tls.Config{}
		opt(cfg)
		if cfg.MinVersion != tls.VersionTLS13 {
			t.Errorf("MinVersion = %v, want %v", cfg.MinVersion, tls.VersionTLS13)
		}
	})

	t.Run("empty flags use defaults", func(t *testing.T) {
		opt, err := TLSOptFor("", "", "")
		if err != nil {
			t.Fatalf("TLSOptFor() error = %v", err)
		}
		cfg := &tls.Config{}
		opt(cfg)

		if cfg.CipherSuites != nil {
			t.Errorf("CipherSuites should be nil, got %v", cfg.CipherSuites)
		}
		if cfg.CurvePreferences != nil {
			t.Errorf("CurvePreferences should be nil, got %v", cfg.CurvePreferences)
		}
		if cfg.MinVersion != tls.VersionTLS13 {
			t.Errorf("MinVersion = %v, want %v", cfg.MinVersion, tls.VersionTLS13)
		}
	})

	t.Run("invalid cipher returns error", func(t *testing.T) {
		_, err := TLSOptFor("FAKE_CIPHER", "", "")
		if err == nil {
			t.Fatal("expected error for invalid cipher")
		}
	})

	t.Run("invalid curve returns error", func(t *testing.T) {
		_, err := TLSOptFor("", "FakeCurve", "")
		if err == nil {
			t.Fatal("expected error for invalid curve")
		}
	})

	t.Run("invalid version returns error", func(t *testing.T) {
		_, err := TLSOptFor("", "", "VersionTLS99")
		if err == nil {
			t.Fatal("expected error for invalid version")
		}
	})
}
