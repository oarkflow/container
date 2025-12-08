//go:build windows

package runtime

func init() {
	Register(Descriptor{
		Name:       "windows-qemu",
		OS:         "windows",
		Hypervisor: "qemu",
		Priority:   30,
		Notes:      "QEMU fallback on Windows",
	}, func() Runtime {
		return newStubRuntime(Descriptor{
			Name:       "windows-qemu",
			OS:         "windows",
			Hypervisor: "qemu",
			Priority:   30,
			Notes:      "QEMU stub",
		}, "qemu-system-x86_64.exe")
	})
}
