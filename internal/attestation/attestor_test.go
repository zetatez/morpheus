package attestation

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestGetRuntimeInfo(t *testing.T) {
	info := GetRuntimeInfo()

	if info["os"] == "" {
		t.Error("expected non-empty os in runtime info")
	}
	if info["arch"] == "" {
		t.Error("expected non-empty arch in runtime info")
	}
	if info["compiler"] == "" {
		t.Error("expected non-empty compiler in runtime info")
	}
	if info["version"] == "" {
		t.Error("expected non-empty version in runtime info")
	}
}

func TestNewAttestor(t *testing.T) {
	config := AttestationConfig{
		TrustedChecksums: map[string]string{
			"1.0.0": "abc123",
		},
		RequireAttestation: false,
	}

	attestor := NewAttestor(config)
	if attestor == nil {
		t.Fatal("expected non-nil attestor")
	}
	if attestor.config.RequireAttestation != false {
		t.Error("expected RequireAttestation to be false")
	}
}

func TestAttestor_NoTrustedBinaries(t *testing.T) {
	config := AttestationConfig{
		TrustedChecksums:   map[string]string{},
		RequireAttestation: true,
	}

	attestor := NewAttestor(config)
	result, err := attestor.Attest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected Valid to be false when no trusted binaries found")
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestGenerateAttestationToken(t *testing.T) {
	result := &AttestationResult{
		Valid:           true,
		AttestationType: AttestationTypeBun,
		Version:         "1.0.0",
		Checksum:        "abc123",
	}

	token := GenerateAttestationToken(result)

	if token.Type != AttestationTypeBun {
		t.Errorf("expected type Bun, got %s", token.Type)
	}
	if token.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", token.Version)
	}
	if token.Checksum != "abc123" {
		t.Errorf("expected checksum abc123, got %s", token.Checksum)
	}
	if token.RuntimeOS == "" {
		t.Error("expected non-empty RuntimeOS")
	}
	if token.Arch == "" {
		t.Error("expected non-empty Arch")
	}
}

func TestVerifyAttestationToken_NotRequired(t *testing.T) {
	config := AttestationConfig{
		TrustedChecksums:   map[string]string{},
		RequireAttestation: false,
	}

	token := AttestationToken{
		Type:     AttestationTypeBun,
		Version:  "1.0.0",
		Checksum: "abc123",
	}

	if !VerifyAttestationToken(token, config) {
		t.Error("expected token to be valid when RequireAttestation is false")
	}
}

func TestVerifyAttestationToken_TrustedChecksum(t *testing.T) {
	config := AttestationConfig{
		TrustedChecksums: map[string]string{
			"1.0.0": "abc123",
		},
		RequireAttestation: true,
	}

	token := AttestationToken{
		Type:     AttestationTypeBun,
		Version:  "1.0.0",
		Checksum: "abc123",
	}

	if !VerifyAttestationToken(token, config) {
		t.Error("expected token to be valid with matching trusted checksum")
	}
}

func TestVerifyAttestationToken_UntrustedChecksum(t *testing.T) {
	config := AttestationConfig{
		TrustedChecksums: map[string]string{
			"1.0.0": "abc123",
		},
		RequireAttestation: true,
	}

	token := AttestationToken{
		Type:     AttestationTypeBun,
		Version:  "1.0.0",
		Checksum: "wrong_checksum",
	}

	if VerifyAttestationToken(token, config) {
		t.Error("expected token to be invalid with non-matching checksum")
	}
}

func TestVerifyAttestationToken_UntrustedVersion(t *testing.T) {
	config := AttestationConfig{
		TrustedChecksums: map[string]string{
			"1.0.0": "abc123",
		},
		RequireAttestation: true,
	}

	token := AttestationToken{
		Type:     AttestationTypeBun,
		Version:  "2.0.0",
		Checksum: "abc123",
	}

	if VerifyAttestationToken(token, config) {
		t.Error("expected token to be invalid with untrusted version")
	}
}

func TestGetFileChecksum(t *testing.T) {
	attestor := NewAttestor(AttestationConfig{})

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("hello world"), 0644)
	if err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	checksum, err := attestor.getFileChecksum(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if checksum == "" {
		t.Error("expected non-empty checksum")
	}

	want := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if checksum != want {
		t.Errorf("expected checksum %s, got %s", want, checksum)
	}
}

func TestGetFileChecksum_FileNotFound(t *testing.T) {
	attestor := NewAttestor(AttestationConfig{})

	_, err := attestor.getFileChecksum("/nonexistent/path/to/file")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestAttestorTypeConstants(t *testing.T) {
	if AttestationTypeBun != "bun" {
		t.Errorf("expected AttestationTypeBun to be 'bun', got '%s'", AttestationTypeBun)
	}
	if AttestationTypeZig != "zig" {
		t.Errorf("expected AttestationTypeZig to be 'zig', got '%s'", AttestationTypeZig)
	}
}

func TestGetVersionFromPath_NotFound(t *testing.T) {
	_, err := getVersionFromPath("/nonexistent/binary", "--version")
	if err == nil {
		t.Error("expected error for nonexistent binary")
	}
}

func TestGetVersionFromPath_Success(t *testing.T) {
	tmpDir := t.TempDir()
	script := filepath.Join(tmpDir, "version_test.sh")
	err := os.WriteFile(script, []byte("#!/bin/sh\necho '1.2.3'"), 0755)
	if err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	version, err := getVersionFromPath(script, "--version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.2.3" {
		t.Errorf("expected version '1.2.3', got '%s'", version)
	}
}

func TestGetBunVersion_NotInstalled(t *testing.T) {
	attestor := NewAttestor(AttestationConfig{})

	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)

	os.Setenv("PATH", "/nonexistent")

	_, err := attestor.getBunVersion("bun")
	if err == nil {
		t.Error("expected error when bun not in PATH")
	}
}

func TestGetZigVersion_NotInstalled(t *testing.T) {
	attestor := NewAttestor(AttestationConfig{})

	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)

	os.Setenv("PATH", "/nonexistent")

	_, err := attestor.getZigVersion("zig")
	if err == nil {
		t.Error("expected error when zig not in PATH")
	}
}

func TestGetBunVersion_Simulated(t *testing.T) {
	attestor := NewAttestor(AttestationConfig{})

	tmpDir := t.TempDir()
	fakeBun := filepath.Join(tmpDir, "bun")
	err := os.WriteFile(fakeBun, []byte("#!/bin/sh\necho '1.0.0'"), 0755)
	if err != nil {
		t.Fatalf("failed to write fake bun: %v", err)
	}

	version, err := attestor.getBunVersion(fakeBun)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", version)
	}
}

func TestAttestBun_Untrusted(t *testing.T) {
	config := AttestationConfig{
		TrustedChecksums:   map[string]string{},
		RequireAttestation: true,
	}
	attestor := NewAttestor(config)

	tmpDir := t.TempDir()
	fakeBun := filepath.Join(tmpDir, "bun")
	err := os.WriteFile(fakeBun, []byte("#!/bin/sh\necho '1.0.0'"), 0755)
	if err != nil {
		t.Fatalf("failed to write fake bun: %v", err)
	}

	result, err := attestor.attestBun(fakeBun)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Valid {
		t.Error("expected Valid to be false for untrusted bun")
	}
	if result.AttestationType != AttestationTypeBun {
		t.Errorf("expected type Bun, got %s", result.AttestationType)
	}
	if result.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", result.Version)
	}
}

func TestAttestBun_Trusted(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBun := filepath.Join(tmpDir, "bun")
	err := os.WriteFile(fakeBun, []byte("#!/bin/sh\necho '1.0.0'"), 0755)
	if err != nil {
		t.Fatalf("failed to write fake bun: %v", err)
	}

	data, err := os.ReadFile(fakeBun)
	if err != nil {
		t.Fatalf("failed to read fake bun: %v", err)
	}

	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])

	config := AttestationConfig{
		TrustedChecksums: map[string]string{
			"1.0.0": checksum,
		},
		RequireAttestation: true,
	}
	attestor := NewAttestor(config)

	result, err := attestor.attestBun(fakeBun)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Valid {
		t.Error("expected Valid to be true for trusted bun")
	}
}

func TestAttestBun_NoAttestationRequired(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBun := filepath.Join(tmpDir, "bun")
	err := os.WriteFile(fakeBun, []byte("#!/bin/sh\necho '1.0.0'"), 0755)
	if err != nil {
		t.Fatalf("failed to write fake bun: %v", err)
	}

	config := AttestationConfig{
		TrustedChecksums:   map[string]string{},
		RequireAttestation: false,
	}
	attestor := NewAttestor(config)

	result, err := attestor.attestBun(fakeBun)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Valid {
		t.Error("expected Valid to be true when attestation not required")
	}
}

func TestAttest_ZigFallback(t *testing.T) {
	config := AttestationConfig{
		TrustedChecksums:   map[string]string{},
		RequireAttestation: true,
	}
	attestor := NewAttestor(config)

	tmpDir := t.TempDir()
	fakeZig := filepath.Join(tmpDir, "zig")
	err := os.WriteFile(fakeZig, []byte("#!/bin/sh\necho '0.13.0'"), 0755)
	if err != nil {
		t.Fatalf("failed to write fake zig: %v", err)
	}

	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	os.Setenv("PATH", tmpDir)

	result, err := attestor.Attest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.AttestationType != AttestationTypeZig {
		t.Errorf("expected type Zig, got %s", result.AttestationType)
	}
}
