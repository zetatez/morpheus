package attestation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type AttestationType string

const (
	AttestationTypeBun AttestationType = "bun"
	AttestationTypeZig AttestationType = "zig"
)

type AttestationResult struct {
	Valid           bool
	AttestationType AttestationType
	Version         string
	Checksum        string
	Error           string
}

type AttestationConfig struct {
	TrustedChecksums   map[string]string
	RequireAttestation bool
}

type Attestor struct {
	config AttestationConfig
}

func NewAttestor(config AttestationConfig) *Attestor {
	return &Attestor{config: config}
}

func (a *Attestor) Attest() (*AttestationResult, error) {
	bunPath, err := exec.LookPath("bun")
	if err == nil && bunPath != "" {
		return a.attestBun(bunPath)
	}

	zigPath, err := exec.LookPath("zig")
	if err == nil && zigPath != "" {
		return a.attestZig(zigPath)
	}

	return &AttestationResult{
		Valid: false,
		Error: "neither bun nor zig found in PATH",
	}, nil
}

func (a *Attestor) attestBun(bunPath string) (*AttestationResult, error) {
	version, err := a.getBunVersion(bunPath)
	if err != nil {
		return &AttestationResult{
			Valid: false,
			Error: fmt.Sprintf("failed to get bun version: %v", err),
		}, nil
	}

	checksum, err := a.getFileChecksum(bunPath)
	if err != nil {
		return &AttestationResult{
			Valid: false,
			Error: fmt.Sprintf("failed to get bun checksum: %v", err),
		}, nil
	}

	expectedChecksum, trusted := a.config.TrustedChecksums[version]
	trustedBun := trusted && expectedChecksum == checksum

	if a.config.RequireAttestation && !trustedBun {
		return &AttestationResult{
			Valid:           false,
			AttestationType: AttestationTypeBun,
			Version:         version,
			Checksum:        checksum,
			Error:           "bun binary is not from a trusted source",
		}, nil
	}

	return &AttestationResult{
		Valid:           trustedBun || !a.config.RequireAttestation,
		AttestationType: AttestationTypeBun,
		Version:         version,
		Checksum:        checksum,
	}, nil
}

func (a *Attestor) attestZig(zigPath string) (*AttestationResult, error) {
	version, err := a.getZigVersion(zigPath)
	if err != nil {
		return &AttestationResult{
			Valid: false,
			Error: fmt.Sprintf("failed to get zig version: %v", err),
		}, nil
	}

	checksum, err := a.getFileChecksum(zigPath)
	if err != nil {
		return &AttestationResult{
			Valid: false,
			Error: fmt.Sprintf("failed to get zig checksum: %v", err),
		}, nil
	}

	expectedChecksum, trusted := a.config.TrustedChecksums[version]
	trustedZig := trusted && expectedChecksum == checksum

	if a.config.RequireAttestation && !trustedZig {
		return &AttestationResult{
			Valid:           false,
			AttestationType: AttestationTypeZig,
			Version:         version,
			Checksum:        checksum,
			Error:           "zig binary is not from a trusted source",
		}, nil
	}

	return &AttestationResult{
		Valid:           trustedZig || !a.config.RequireAttestation,
		AttestationType: AttestationTypeZig,
		Version:         version,
		Checksum:        checksum,
	}, nil
}

func (a *Attestor) getBunVersion(bunPath string) (string, error) {
	cmd := exec.Command(bunPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (a *Attestor) getZigVersion(zigPath string) (string, error) {
	cmd := exec.Command(zigPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (a *Attestor) getFileChecksum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func GetRuntimeInfo() map[string]string {
	info := map[string]string{
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"compiler": runtime.Compiler,
		"version":  runtime.Version(),
	}

	if bunPath, err := exec.LookPath("bun"); err == nil {
		info["bun"] = bunPath
		if version, err := getVersionFromPath(bunPath, "--version"); err == nil {
			info["bun_version"] = version
		}
	}

	if zigPath, err := exec.LookPath("zig"); err == nil {
		info["zig"] = zigPath
		if version, err := getVersionFromPath(zigPath, "version"); err == nil {
			info["zig_version"] = version
		}
	}

	return info
}

func getVersionFromPath(path, versionFlag string) (string, error) {
	cmd := exec.Command(path, versionFlag)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

type AttestationToken struct {
	Type      AttestationType `json:"type"`
	Version   string          `json:"version"`
	Checksum  string          `json:"checksum"`
	RuntimeOS string          `json:"runtime_os"`
	Arch      string          `json:"arch"`
	Timestamp int64           `json:"timestamp"`
}

func GenerateAttestationToken(result *AttestationResult) AttestationToken {
	return AttestationToken{
		Type:      result.AttestationType,
		Version:   result.Version,
		Checksum:  result.Checksum,
		RuntimeOS: runtime.GOOS,
		Arch:      runtime.GOARCH,
		Timestamp: 0,
	}
}

func VerifyAttestationToken(token AttestationToken, config AttestationConfig) bool {
	if !config.RequireAttestation {
		return true
	}

	expectedChecksum, trusted := config.TrustedChecksums[token.Version]
	if !trusted {
		return false
	}

	return expectedChecksum == token.Checksum
}
