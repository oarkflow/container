//go:build darwin

package runtime

func init() {
	Register(Descriptor{
		Name:       "macos-qemu",
		OS:         "darwin",
		Hypervisor: "qemu-hvf",
		Priority:   20,
		Notes:      "QEMU with HVF acceleration",
	}, func() Runtime {
		return newStubRuntime(Descriptor{
			Name:       "macos-qemu",
			OS:         "darwin",
			Hypervisor: "qemu-hvf",
			Priority:   20,
			Notes:      "QEMU HVF stub",
		}, "qemu-system-x86_64")
	})
}
