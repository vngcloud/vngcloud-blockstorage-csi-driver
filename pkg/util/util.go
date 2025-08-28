package util

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	GiB = 1024 * 1024 * 1024
)

func ParseEndpoint(endpoint string) (string, string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", "", fmt.Errorf("could not parse endpoint: %w", err)
	}

	addr := filepath.Join(u.Host, filepath.FromSlash(u.Path))

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "tcp":
	case "unix":
		addr = filepath.Join("/", addr)
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return "", "", fmt.Errorf("could not remove unix domain socket %q: %w", addr, err)
		}
	default:
		return "", "", fmt.Errorf("unsupported protocol: %s", scheme)
	}

	return scheme, addr, nil
}

// RoundUpBytes rounds up the volume size in bytes upto multiplications of GiB
// in the unit of Bytes
func RoundUpBytes(volumeSizeBytes int64) int64 {
	return roundUpSize(volumeSizeBytes, GiB) * GiB
}

func RoundUpSize(volumeSizeBytes int64, allocationUnitBytes int64) int64 {
	roundedUp := volumeSizeBytes / allocationUnitBytes
	if volumeSizeBytes%allocationUnitBytes > 0 {
		roundedUp++
	}
	return roundedUp
}

// TODO: check division by zero and int overflow
func roundUpSize(volumeSizeBytes int64, allocationUnitBytes int64) int64 {
	return (volumeSizeBytes + allocationUnitBytes - 1) / allocationUnitBytes
}

func RoundUpGiB(volumeSizeBytes int64) int64 {
	return roundUpSize(volumeSizeBytes, GiB)
}

// GiBToBytes converts GiB to Bytes
func GiBToBytes(volumeSizeGiB int64) int64 {
	return volumeSizeGiB * GiB
}

func ConvertPortalZoneToVMZone(zone string) string {
	switch zone {
	case "HCM03-1A":
		return "AZ01"
	case "HCM03-1B":
		return "HCM-1B"
	case "HCM03-1C":
		return "HCM-1C"
	case "BKK-01":
		return "BKK01"
	case "HAN01-1A":
		return "az01"
	default:
		return zone
	}
}

func ConvertVMZoneToPortalZone(zone string) string {
	switch zone {
	case "AZ01":
		return "HCM03-1A"
	case "HCM-1B":
		return "HCM03-1B"
	case "HCM-1C":
		return "HCM03-1C"
	case "BKK01":
		return "BKK-01"
	case "az01":
		return "HAN01-1A"
	default:
		return zone
	}
}
