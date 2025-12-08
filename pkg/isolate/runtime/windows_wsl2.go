//go:build windows

package runtime

func init() {
	Register(Descriptor{
		Name:       "windows-wsl2",
		OS:         "windows",
		Hypervisor: "wsl2",
		Priority:   20,
		Notes:      "WSL2 lightweight backend",
	}, func() Runtime {
		return newStubRuntime(Descriptor{
			Name:       "windows-wsl2",
			OS:         "windows",
			Hypervisor: "wsl2",
			Priority:   20,
			Notes:      "WSL2 stub",
		}, "wsl.exe")
	})
}
