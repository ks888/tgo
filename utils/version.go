package utils

import (
	"strconv"
	"strings"
)

const (
	develVersion  = "devel"
	versionPrefix = "go"
)

// GoVersion represents a go version.
type GoVersion struct {
	Raw                                      string
	Devel                                    bool
	MajorVersion, MinorVersion, PatchVersion int
}

// ParseGoVersion parses the go version string such as 'go1.11.1'
func ParseGoVersion(raw string) GoVersion {
	goVersion := GoVersion{Raw: raw}

	if strings.HasPrefix(raw, develVersion) {
		goVersion.Devel = true
		return goVersion
	}

	if !strings.HasPrefix(raw, versionPrefix) {
		return goVersion
	}

	version := strings.Split(strings.TrimPrefix(raw, versionPrefix), ".")
	if len(version) > 0 {
		goVersion.MajorVersion, _ = strconv.Atoi(version[0])
	}

	if len(version) > 1 {
		goVersion.MinorVersion, _ = strconv.Atoi(version[1])
	}

	if len(version) > 2 {
		goVersion.PatchVersion, _ = strconv.Atoi(version[2])
	}
	return goVersion
}

// LaterThan returns true if the version is equal to or later than the given version.
func (v GoVersion) LaterThan(target GoVersion) bool {
	if v.Devel {
		return true
	}

	if v.MajorVersion > target.MajorVersion {
		return true
	} else if v.MajorVersion < target.MajorVersion {
		return false
	}

	if v.MinorVersion > target.MinorVersion {
		return true
	} else if v.MinorVersion < target.MinorVersion {
		return false
	}

	return v.PatchVersion >= target.PatchVersion
}
