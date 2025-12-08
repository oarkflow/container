//go:build linux

package runtime

func init() {
	Register(Descriptor{
		Name:       "linux-firecracker",
		OS:         "linux",
		Hypervisor: "firecracker",
		Priority:   10,
		Notes:      "Fast microVM runtime leveraging Firecracker",
	}, func() Runtime {
		return newStubRuntime(Descriptor{
			Name:       "linux-firecracker",
			OS:         "linux",
			Hypervisor: "firecracker",
			Priority:   10,
			Notes:      "Firecracker stub",
		}, "firecracker")
	})
}
