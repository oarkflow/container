//go:build windows

package runtime

func init() {
	Register(Descriptor{
		Name:       "windows-hyperv",
		OS:         "windows",
		Hypervisor: "hyper-v",
		Priority:   10,
		Notes:      "Native Hyper-V runtime",
	}, func() Runtime {
		return newStubRuntime(Descriptor{
			Name:       "windows-hyperv",
			OS:         "windows",
			Hypervisor: "hyper-v",
			Priority:   10,
			Notes:      "Hyper-V stub",
		}, "vmcompute.exe")
	})
}
