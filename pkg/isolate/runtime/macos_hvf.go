//go:build darwin

package runtime

func init() {
	Register(Descriptor{
		Name:       "macos-hvf",
		OS:         "darwin",
		Hypervisor: "hypervisor.framework",
		Priority:   10,
		Notes:      "Native Hypervisor.framework backend",
	}, func() Runtime {
		return newStubRuntime(Descriptor{
			Name:       "macos-hvf",
			OS:         "darwin",
			Hypervisor: "hypervisor.framework",
			Priority:   10,
			Notes:      "Hypervisor.framework stub",
		})
	})
}
