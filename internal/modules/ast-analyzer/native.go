package astanalyzer

import (
	"fmt"
	"runtime"
)

// NativeParser returns the parser already shipped with JXScout for this host.
func NativeParser() ([]byte, error) {
	key := runtime.GOOS + "/" + runtime.GOARCH
	if runtime.GOOS == "linux" && runtime.GOARCH != "arm" {
		libc, err := getLibcVariant()
		if err != nil {
			return nil, err
		}
		key += "/" + libc
	}
	binaries := map[string][]byte{
		"darwin/arm64": parserDarwinArm64, "darwin/amd64": parserDarwinX64,
		"linux/arm": parserLinuxArmGnueabihf, "linux/arm64/gnu": parserLinuxArm64Gnu, "linux/arm64/musl": parserLinuxArm64Musl,
		"linux/amd64/gnu": parserLinuxX64Gnu, "linux/amd64/musl": parserLinuxX64Musl,
		"windows/amd64": parserWin32X64Msvc, "windows/arm64": parserWin32Arm64Msvc,
	}
	if data, ok := binaries[key]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("unsupported parser platform: %s", key)
}
